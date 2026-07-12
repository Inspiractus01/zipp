package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type page int

const (
	pageMenu page = iota
	pageJobs
	pageJobDetail
	pageAdd
	pageEdit
	pageRun
	pageConfirmDelete
	pageRestore
	pageConfirmRestore
	pageNest
)

// runResultMsg is used by setup/update cmds (non-streaming).
type runResultMsg struct {
	lines []string
	err   error
}

// runStartedMsg signals that a streaming job has started.
type runStartedMsg struct {
	ch    <-chan string
	errCh <-chan error
}

// jobLineMsg is one line of streamed output.
type jobLineMsg string

// jobDoneMsg signals the streaming job finished.
type jobDoneMsg struct{ err error }

// tickMsg drives the fly animation.
type tickMsg struct{}

type nestHealthMsg struct{ ok bool }

type snapshotsLoadedMsg struct {
	job   *Job
	snaps []SnapshotInfo
}

var nestClient = &http.Client{Timeout: 8 * time.Second}

func nestHealthCmd(address string) tea.Cmd {
	return func() tea.Msg {
		resp, err := nestClient.Get("http://" + address + "/health")
		if err != nil {
			return nestHealthMsg{ok: false}
		}
		resp.Body.Close()
		return nestHealthMsg{ok: resp.StatusCode == 200}
	}
}

func nestHealthCmdDelayed(address string) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(30 * time.Second)
		resp, err := nestClient.Get("http://" + address + "/health")
		if err != nil {
			return nestHealthMsg{ok: false}
		}
		resp.Body.Close()
		return nestHealthMsg{ok: resp.StatusCode == 200}
	}
}

type updateCheckMsg updateResult

type model struct {
	page        page
	cursor      int
	config      *Config
	err         error
	windowWidth int

	updateInfo    updateResult
	schedulerInfo schedulerStatus

	// add job form
	formStep   int
	formInputs []textinput.Model
	formErr    string

	// run view
	runOutput []string
	runDone   bool

	// streaming state
	runCh     <-chan string
	runErrCh  <-chan error
	animFrame int

	// running guards against starting a second run/restore while one is
	// still in flight, and runCancel lets esc cancel the in-flight one.
	running   bool
	runCancel context.CancelFunc

	// delete confirm
	deleteTarget *Job

	// edit
	editTarget *Job

	// job detail
	detailJob    *Job
	detailCursor int

	// restore
	restoreJob     *Job
	restoreSnaps   []SnapshotInfo
	restoreCursor  int
	restoreLoading bool
	restoreMsg     string

	// nest
	nestInput      textinput.Model
	nestErr        string
	nestConnected  bool
	nestChecking   bool
	nestTSStatus   tailscaleStatus
	nestPageCursor int
	nestInputMode  bool
}

var menuItemsBase = []string{"Jobs", "Run all", "Scheduler", "Nest", "Quit"}

func (m model) getMenuItems() []string {
	if m.updateInfo.hasUpdate {
		items := make([]string, 0, len(menuItemsBase)+1)
		for _, item := range menuItemsBase {
			if item == "Quit" {
				items = append(items, "Run update")
			}
			items = append(items, item)
		}
		return items
	}
	return menuItemsBase
}

func newModel(cfg *Config) model {
	return model{
		page:   pageMenu,
		config: cfg,
	}
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		func() tea.Msg { return updateCheckMsg(checkForUpdate()) },
		checkSchedulerCmd(),
		tickCmd(),
	}
	if m.config.Nest != nil {
		cmds = append(cmds, nestHealthCmd(m.config.Nest.Address))
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// ctrl+c always force-quits, regardless of page — everything below is
	// page-scoped (e.g. plain "q" only quits from the menu, so it doesn't
	// steal the letter "q" while typing into a text input elsewhere).
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "ctrl+c" {
		return m, tea.Quit
	}

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.windowWidth = msg.Width
		return m, nil

	case tea.KeyMsg:
		if m.page == pageNest {
			return m.updateNest(msg)
		}
		switch msg.String() {
		case "q":
			if m.page == pageMenu {
				return m, tea.Quit
			}
		case "esc":
			if m.page == pageRun {
				// cancel the in-flight run so it stops before mutating any
				// more shared job state, and free the guard so a new run
				// can be started.
				if m.runCancel != nil {
					m.runCancel()
				}
				m.running = false
				m.runCancel = nil
				m.page = pageMenu
				m.cursor = 0
				m.runOutput = nil
				m.runDone = false
				return m, nil
			}
			if m.page == pageAdd || m.page == pageEdit || m.page == pageJobs {
				m.page = pageMenu
				m.cursor = 0
				m.formStep = 0
				m.formInputs = nil
				m.formErr = ""
				m.restoreLoading = false
				m.restoreMsg = ""
				m.restoreSnaps = nil
				m.restoreJob = nil
			} else if m.page == pageJobDetail {
				m.page = pageJobs
				m.detailJob = nil
				m.detailCursor = 0
			} else if m.page == pageConfirmDelete {
				m.page = pageJobs
				m.deleteTarget = nil
			} else if m.page == pageRestore {
				m.page = pageJobs
				m.restoreJob = nil
				m.restoreSnaps = nil
				m.restoreCursor = 0
				m.restoreLoading = false
			} else if m.page == pageConfirmRestore {
				m.page = pageRestore
			}
			return m, nil
		}

		switch m.page {
		case pageMenu:
			return m.updateMenu(msg)
		case pageJobs:
			return m.updateJobs(msg)
		case pageJobDetail:
			return m.updateJobDetail(msg)
		case pageAdd:
			return m.updateAdd(msg)
		case pageEdit:
			return m.updateEdit(msg)
		case pageRun:
			return m.updateRun(msg)
		case pageConfirmDelete:
			return m.updateConfirmDelete(msg)
		case pageRestore:
			return m.updateRestore(msg)
		case pageConfirmRestore:
			return m.updateConfirmRestore(msg)
		case pageNest:
			return m.updateNest(msg)
		}

	case updateCheckMsg:
		m.updateInfo = updateResult(msg)
		return m, nil

	case updateDoneMsg:
		m.running = false
		m.runCancel = nil
		if msg.err != nil {
			m.runDone = true
			m.runOutput = append(m.runOutput, styleError.Render("update failed: "+msg.err.Error()))
			return m, nil
		}
		return m, tea.Quit

	case nestHealthMsg:
		m.nestChecking = false
		m.nestConnected = msg.ok
		if m.config.Nest != nil {
			return m, nestHealthCmdDelayed(m.config.Nest.Address)
		}
		return m, nil

	case nestTSCheckMsg:
		m.nestTSStatus = tailscaleStatus(msg)
		return m, nil

	case nestTSDoneMsg:
		m.nestErr = ""
		if msg.err != nil {
			m.nestErr = msg.err.Error()
		}
		// delay recheck so tailscale has time to update its state
		return m, checkNestTSCmdDelayed()

	case schedulerCheckMsg:
		m.schedulerInfo = schedulerStatus(msg)
		return m, nil

	case runStartedMsg:
		m.runCh = msg.ch
		m.runErrCh = msg.errCh
		return m, tea.Batch(nextLineCmd(msg.ch, msg.errCh), tickCmd())

	case jobLineMsg:
		m.runOutput = append(m.runOutput, string(msg))
		return m, nextLineCmd(m.runCh, m.runErrCh)

	case jobDoneMsg:
		m.runDone = true
		m.runCh = nil
		m.runErrCh = nil
		m.running = false
		m.runCancel = nil
		if msg.err != nil {
			m.runOutput = append(m.runOutput, styleError.Render("error: "+msg.err.Error()))
		} else {
			m.runOutput = append(m.runOutput, styleSuccess.Render("\n✓ all done"))
		}
		m.config.save()
		return m, nil

	case tickMsg:
		m.animFrame++
		return m, tickCmd()

	case runResultMsg:
		m.runDone = true
		m.running = false
		m.runCancel = nil
		m.runOutput = msg.lines
		if msg.err != nil {
			m.runOutput = append(m.runOutput, styleError.Render("error: "+msg.err.Error()))
		} else {
			m.runOutput = append(m.runOutput, styleSuccess.Render("\n✓ all done"))
		}
		m.config.save()
		return m, nil

	case snapshotsLoadedMsg:
		if !m.restoreLoading {
			return m, nil // user cancelled with esc
		}
		m.restoreLoading = false
		if len(msg.snaps) == 0 {
			m.restoreMsg = "no snapshots yet for \"" + msg.job.Name + "\""
			return m, nil
		}
		m.restoreMsg = ""
		m.restoreJob = msg.job
		m.restoreSnaps = msg.snaps
		m.restoreCursor = 0
		m.page = pageRestore
		return m, nil
	}

	return m, nil
}

// — Cmds —

func tickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

func loadSnapshotsCmd(job *Job, nest *NestConfig) tea.Cmd {
	return func() tea.Msg {
		snaps, _ := listSnapshotInfos(job, nest)
		return snapshotsLoadedMsg{job: job, snaps: snaps}
	}
}

func nextLineCmd(ch <-chan string, errCh <-chan error) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return jobDoneMsg{err: <-errCh}
		}
		return jobLineMsg(line)
	}
}

func runAllCmd(ctx context.Context, cfg *Config) tea.Cmd {
	return func() tea.Msg {
		ch := make(chan string, 256)
		errCh := make(chan error, 1)
		go func() {
			defer close(ch)
			var runErr error
			ran := 0
			for _, job := range cfg.Jobs {
				if ctx.Err() != nil {
					ch <- styleDim.Render("cancelled")
					break
				}
				if !job.Enabled {
					continue
				}
				if err := runJob(ctx, job, cfg.Nest, ch); err != nil {
					if ctx.Err() == nil {
						ch <- styleError.Render("✗ " + err.Error())
						runErr = err
					}
				}
				ran++
			}
			if ran == 0 {
				ch <- styleDim.Render("no enabled jobs to run")
			}
			errCh <- runErr
		}()
		return runStartedMsg{ch: ch, errCh: errCh}
	}
}

func runJobCmd(ctx context.Context, cfg *Config, job *Job) tea.Cmd {
	return func() tea.Msg {
		ch := make(chan string, 256)
		errCh := make(chan error, 1)
		go func() {
			defer close(ch)
			var runErr error
			if err := runJob(ctx, job, cfg.Nest, ch); err != nil && ctx.Err() == nil {
				ch <- styleError.Render("✗ " + err.Error())
				runErr = err
			}
			errCh <- runErr
		}()
		return runStartedMsg{ch: ch, errCh: errCh}
	}
}

// — Menu —

func (m model) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.getMenuItems()
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(items)-1 {
			m.cursor++
		}
	case "enter":
		switch items[m.cursor] {
		case "Jobs":
			m.page = pageJobs
			m.cursor = 0
		case "Run all":
			if m.running {
				return m, nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			m.running = true
			m.runCancel = cancel
			m.page = pageRun
			m.runOutput = nil
			m.runDone = false
			return m, runAllCmd(ctx, m.config)
		case "Scheduler":
			if m.running {
				return m, nil
			}
			m.running = true
			m.runCancel = nil
			m.page = pageRun
			m.runOutput = nil
			m.runDone = false
			return m, setupSchedulerCmd()
		case "Run update":
			if m.running {
				return m, nil
			}
			m.running = true
			m.runCancel = nil
			m.page = pageRun
			m.runOutput = nil
			m.runDone = false
			return m, runUpdateCmd()
		case "Nest":
			m.nestErr = ""
			m.nestInputMode = false
			m.nestPageCursor = 0
			m.page = pageNest
			var cmds []tea.Cmd
			cmds = append(cmds, checkNestTSCmd())
			if m.config.Nest != nil {
				m.nestChecking = true
				cmds = append(cmds, nestHealthCmd(m.config.Nest.Address))
			}
			return m, tea.Batch(cmds...)
		case "Quit":
			return m, tea.Quit
		}
	}
	return m, nil
}

// — Jobs —

func (m model) updateJobs(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.config.Jobs)-1 {
			m.cursor++
		}
	case "enter":
		if len(m.config.Jobs) > 0 {
			m.detailJob = m.config.Jobs[m.cursor]
			m.detailCursor = 0
			m.page = pageJobDetail
		}
	case "e":
		if len(m.config.Jobs) > 0 {
			m.editTarget = m.config.Jobs[m.cursor]
			m.formStep = 0
			m.formInputs = newFormInputsFrom(m.editTarget)
			m.page = pageEdit
		}
	case "d":
		if len(m.config.Jobs) > 0 {
			m.deleteTarget = m.config.Jobs[m.cursor]
			m.page = pageConfirmDelete
		}
	case "t":
		if len(m.config.Jobs) > 0 {
			job := m.config.Jobs[m.cursor]
			job.Enabled = !job.Enabled
			m.config.save()
		}
	case "n":
		if len(m.config.Jobs) > 0 {
			job := m.config.Jobs[m.cursor]
			job.cycleMode()
			m.config.save()
		}
	case "r":
		if len(m.config.Jobs) > 0 {
			job := m.config.Jobs[m.cursor]
			m.restoreLoading = true
			m.restoreMsg = ""
			return m, loadSnapshotsCmd(job, m.config.Nest)
		}
	case "c":
		m.page = pageAdd
		m.formStep = 0
		m.formInputs = newFormInputs()
		m.formErr = ""
	}
	return m, nil
}

// — Job detail —

func (m model) jobDetailItems() []string {
	j := m.detailJob
	toggle := "Disable"
	if !j.Enabled {
		toggle = "Enable"
	}
	return []string{
		"Run now",
		"Restore a snapshot",
		"Edit",
		"Backup mode: " + j.mode(),
		toggle,
		"Delete",
		"Back",
	}
}

func (m model) updateJobDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.detailJob == nil {
		m.page = pageJobs
		return m, nil
	}
	items := m.jobDetailItems()
	switch msg.String() {
	case "up", "k":
		if m.detailCursor > 0 {
			m.detailCursor--
		}
	case "down", "j":
		if m.detailCursor < len(items)-1 {
			m.detailCursor++
		}
	case "enter":
		item := items[m.detailCursor]
		switch {
		case item == "Run now":
			if m.running {
				return m, nil
			}
			job := m.detailJob
			ctx, cancel := context.WithCancel(context.Background())
			m.running = true
			m.runCancel = cancel
			m.page = pageRun
			m.runOutput = nil
			m.runDone = false
			return m, runJobCmd(ctx, m.config, job)
		case item == "Restore a snapshot":
			m.restoreLoading = true
			m.restoreMsg = ""
			m.page = pageJobs
			return m, loadSnapshotsCmd(m.detailJob, m.config.Nest)
		case item == "Edit":
			m.editTarget = m.detailJob
			m.formStep = 0
			m.formInputs = newFormInputsFrom(m.editTarget)
			m.page = pageEdit
		case strings.HasPrefix(item, "Backup mode"):
			m.detailJob.cycleMode()
			m.config.save()
		case item == "Disable" || item == "Enable":
			m.detailJob.Enabled = !m.detailJob.Enabled
			m.config.save()
		case item == "Delete":
			m.deleteTarget = m.detailJob
			m.detailJob = nil
			m.page = pageConfirmDelete
		case item == "Back":
			m.detailJob = nil
			m.detailCursor = 0
			m.page = pageJobs
		}
	}
	return m, nil
}

func (m model) viewJobDetail() string {
	j := m.detailJob
	if j == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(renderHeader(j.Name))
	b.WriteString("\n")

	lStyle := styleLabel.Copy().Width(labelWidth(m.windowWidth))
	info := func(label, val string) {
		b.WriteString("  " + lStyle.Render(label) + styleNormal.Render(val) + "\n")
	}
	info("source", j.Source)
	dest := j.Destination
	if dest == "" {
		dest = "—"
	}
	info("destination", dest)
	if j.IntervalHours == 0 {
		info("interval", "manual only")
	} else {
		info("interval", fmt.Sprintf("every %dh", j.IntervalHours))
	}
	if j.MaxSnapshots == 0 {
		info("snapshots kept", "all")
	} else {
		info("snapshots kept", fmt.Sprintf("%d", j.MaxSnapshots))
	}
	format := "folders (rsync)"
	if j.Compress {
		format = "compressed (.tar.gz)"
	}
	info("format", format)
	info("next run", j.nextRun())
	b.WriteString("\n")

	for i, item := range m.jobDetailItems() {
		if i == m.detailCursor {
			b.WriteString("  " + styleSelected.Render("▸ "+item) + "\n")
		} else {
			b.WriteString("  " + styleDim.Render("  "+item) + "\n")
		}
	}

	b.WriteString(styleHint.Render("\n  ↑↓ navigate · enter select · esc back"))
	return b.String()
}

// — Add job form —

func newFormInputs() []textinput.Model {
	fields := []struct {
		placeholder string
		charLimit   int
	}{
		{"e.g. Documents backup", 64},
		{"e.g. ~/Documents", 256},
		{"e.g. /mnt/backup/docs", 256},
		{"24 = daily, 168 = weekly, 0 = manual only", 4},
		{"old backups to keep, 0 = keep all (default 10)", 4},
		{"y = compress (.tar.gz, saves space, slower)  n = folders (default)", 1},
	}

	inputs := make([]textinput.Model, len(fields))
	for i, f := range fields {
		ti := textinput.New()
		ti.Placeholder = f.placeholder
		ti.CharLimit = f.charLimit
		if i == 0 {
			ti.Focus()
		}
		inputs[i] = ti
	}
	return inputs
}

func newFormInputsFrom(j *Job) []textinput.Model {
	inputs := newFormInputs()
	inputs[0].SetValue(j.Name)
	inputs[1].SetValue(j.Source)
	inputs[2].SetValue(j.Destination)
	inputs[3].SetValue(fmt.Sprintf("%d", j.IntervalHours))
	inputs[4].SetValue(fmt.Sprintf("%d", j.MaxSnapshots))
	if j.Compress {
		inputs[5].SetValue("y")
	} else {
		inputs[5].SetValue("n")
	}
	return inputs
}

var formLabels = []string{"Name", "Source", "Destination", "Interval (h)", "Max snapshots", "Compress"}

func (m model) updateAdd(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.formStep < len(m.formInputs)-1 {
			m.formInputs[m.formStep].Blur()
			m.formStep++
			m.formInputs[m.formStep].Focus()
			return m, textinput.Blink
		}
		job := m.buildJob()
		if job == nil {
			// stay on the form and say what's missing instead of silently
			// discarding everything the user just typed
			missingStep, msg := m.formMissingField()
			m.formErr = msg
			m.formInputs[m.formStep].Blur()
			m.formStep = missingStep
			m.formInputs[m.formStep].Focus()
			return m, textinput.Blink
		}
		m.formErr = ""
		m.config.addJob(job)
		m.config.save()
		m.page = pageJobs
		m.cursor = len(m.config.Jobs) - 1
		m.formInputs = nil
		m.formStep = 0
		return m, nil

	case "shift+tab":
		if m.formStep > 0 {
			m.formInputs[m.formStep].Blur()
			m.formStep--
			m.formInputs[m.formStep].Focus()
			return m, textinput.Blink
		}
	}

	var cmd tea.Cmd
	m.formInputs[m.formStep], cmd = m.formInputs[m.formStep].Update(msg)
	return m, cmd
}

// formMissingField reports which required field(s) are blank and which form
// step to focus so the user lands on the problem field, not just a bounce
// back to the jobs list.
func (m model) formMissingField() (step int, message string) {
	nameBlank := strings.TrimSpace(m.formInputs[0].Value()) == ""
	srcBlank := strings.TrimSpace(m.formInputs[1].Value()) == ""
	switch {
	case nameBlank && srcBlank:
		return 0, "Name and Source are required"
	case nameBlank:
		return 0, "Name is required"
	case srcBlank:
		return 1, "Source is required"
	}
	return 0, "Name and Source are required"
}

func (m model) buildJob() *Job {
	name := strings.TrimSpace(m.formInputs[0].Value())
	src := strings.TrimSpace(m.formInputs[1].Value())
	if name == "" || src == "" {
		return nil
	}

	dest := strings.TrimSpace(m.formInputs[2].Value())

	interval := 0
	fmt.Sscanf(m.formInputs[3].Value(), "%d", &interval)

	maxSnap := 10
	if m.formInputs[4].Value() != "" {
		fmt.Sscanf(m.formInputs[4].Value(), "%d", &maxSnap)
	}

	compress := strings.ToLower(strings.TrimSpace(m.formInputs[5].Value())) == "y"

	return &Job{
		ID:            fmt.Sprintf("%x", time.Now().UnixNano()),
		Name:          name,
		Source:        src,
		Destination:   dest,
		IntervalHours: interval,
		MaxSnapshots:  maxSnap,
		Compress:      compress,
		Enabled:       true,
	}
}

// — Edit job —

func (m model) updateEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.formStep < len(m.formInputs)-1 {
			m.formInputs[m.formStep].Blur()
			m.formStep++
			m.formInputs[m.formStep].Focus()
			return m, textinput.Blink
		}
		// save edits back to existing job
		if m.editTarget != nil {
			m.editTarget.Name = strings.TrimSpace(m.formInputs[0].Value())
			m.editTarget.Source = strings.TrimSpace(m.formInputs[1].Value())
			m.editTarget.Destination = strings.TrimSpace(m.formInputs[2].Value())
			fmt.Sscanf(m.formInputs[3].Value(), "%d", &m.editTarget.IntervalHours)
			fmt.Sscanf(m.formInputs[4].Value(), "%d", &m.editTarget.MaxSnapshots)
			m.editTarget.Compress = strings.ToLower(strings.TrimSpace(m.formInputs[5].Value())) == "y"
			m.config.save()
		}
		m.editTarget = nil
		m.formInputs = nil
		m.formStep = 0
		m.page = pageJobs
		return m, nil

	case "shift+tab":
		if m.formStep > 0 {
			m.formInputs[m.formStep].Blur()
			m.formStep--
			m.formInputs[m.formStep].Focus()
			return m, textinput.Blink
		}
	}

	var cmd tea.Cmd
	m.formInputs[m.formStep], cmd = m.formInputs[m.formStep].Update(msg)
	return m, cmd
}

// — Run view —

func (m model) updateRun(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.runDone {
		switch msg.String() {
		case "enter", "esc":
			m.page = pageMenu
			m.runOutput = nil
			m.runDone = false
		}
	}
	return m, nil
}

// — Delete confirm —

func (m model) updateConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		if m.deleteTarget != nil {
			m.config.removeJob(m.deleteTarget.ID)
			m.config.save()
			if m.cursor >= len(m.config.Jobs) && m.cursor > 0 {
				m.cursor--
			}
		}
		m.deleteTarget = nil
		m.page = pageJobs
	case "n", "esc":
		m.deleteTarget = nil
		m.page = pageJobs
	}
	return m, nil
}

// — Views —

// View renders a small "ZIPP · PAGE" nav bar above a single bordered panel
// holding the current page's content — a consistent frame every page shares.
func (m model) View() string {
	title, body := m.currentPageView()
	return pageHeader(title) + "\n\n" + renderPanel(m.windowWidth, body)
}

func (m model) currentPageView() (title, body string) {
	switch m.page {
	case pageMenu:
		return "menu", m.viewMenu()
	case pageJobs:
		return "jobs", m.viewJobs()
	case pageJobDetail:
		return "job detail", m.viewJobDetail()
	case pageAdd:
		return "add job", m.viewAdd()
	case pageEdit:
		return "edit job", m.viewEdit()
	case pageRun:
		if m.runDone {
			return "done", m.viewRunDone()
		}
		return "running", m.viewRun()
	case pageConfirmDelete:
		return "delete job", m.viewConfirmDelete()
	case pageRestore:
		return "restore", m.viewRestore()
	case pageConfirmRestore:
		return "confirm restore", m.viewConfirmRestore()
	case pageNest:
		return "nest", m.viewNest()
	}
	return "", ""
}

func (m model) viewMenu() string {
	var b strings.Builder
	b.WriteString(renderHeader(""))
	b.WriteString("\n")

	for i, item := range m.getMenuItems() {
		if i == m.cursor {
			b.WriteString("  " + styleSelected.Render("▸ "+item))
		} else if item == "Run update" {
			b.WriteString("  " + styleUpdate.Render("  "+item))
		} else {
			b.WriteString("  " + styleDim.Render("  "+item))
		}
		if item == "Jobs" && len(m.config.Jobs) > 0 {
			b.WriteString(styleDim.Render(fmt.Sprintf("   %d total", len(m.config.Jobs))))
		}
		if item == "Scheduler" {
			if m.schedulerInfo.active {
				b.WriteString(styleSuccess.Render("   ✓ hourly") + styleDim.Render(" via "+m.schedulerInfo.method))
			} else {
				b.WriteString(styleError.Render("   ⚠ not running") + styleDim.Render(" — enter to set up"))
			}
		}
		if item == "Nest" {
			if m.nestChecking {
				dots := []string{"·  ", "·· ", "···"}
				b.WriteString(styleDim.Render("   checking " + dots[m.animFrame%len(dots)]))
			} else if m.nestConnected {
				b.WriteString(styleSuccess.Render("   ✓ connected"))
			} else if m.config.Nest != nil {
				b.WriteString(styleWarning.Render("   ○ " + m.config.Nest.Address))
			} else {
				b.WriteString(styleDim.Render("   remote backup server — not set up"))
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")

	if m.updateInfo.hasUpdate {
		alert := lipgloss.NewStyle().Foreground(colorRed).Bold(true)
		b.WriteString("  " + alert.Render("● new version available: v"+m.updateInfo.latest) + "\n")
		b.WriteString("  " + styleDim.Render("  select \"Run update\" to install") + "\n")
	}

	b.WriteString(styleHint.Render("\n  ↑↓ navigate · enter select · q quit"))
	return b.String()
}

func (m model) viewJobs() string {
	var b strings.Builder
	b.WriteString(renderHeader(""))
	b.WriteString("\n")

	if len(m.config.Jobs) == 0 {
		b.WriteString(styleDim.Render("  no jobs yet — press ") +
			lipgloss.NewStyle().Foreground(colorOrange).Bold(true).Render("c") +
			styleDim.Render(" to create your first backup job") + "\n")
	} else {
		for i, job := range m.config.Jobs {
			selected := i == m.cursor

			var indicator string
			if !job.Enabled {
				indicator = styleError.Render("✗")
			} else if job.isDue() {
				indicator = styleWarning.Render("⚡")
			} else {
				indicator = styleSuccess.Render("✓")
			}

			nameStr := job.Name
			if selected {
				nameStr = styleSelected.Render(nameStr)
			} else {
				nameStr = styleNormal.Render(nameStr)
			}
			if job.Compress {
				nameStr += styleDim.Render(" [zip]")
			}
			switch job.mode() {
			case "both":
				nameStr += styleSuccess.Render(" [nest+local]")
			case "nest":
				nameStr += styleSuccess.Render(" [nest]")
			default:
				nameStr += styleDim.Render(" [local]")
			}
			next := styleDim.Render(job.nextRun())

			prefix := "  "
			if selected {
				prefix = styleSelected.Render("▸ ")
			}

			line := fmt.Sprintf("%s%s %s", prefix, indicator+"  "+nameStr, next)
			b.WriteString(lipgloss.NewStyle().Width(jobRowWidth(m.windowWidth)).Render(line) + "\n")

			if job.Destination == "" {
				switch job.mode() {
				case "local":
					b.WriteString("     " + styleError.Render("⚠ destination not set") + "\n")
				case "both":
					b.WriteString("     " + styleWarning.Render("⚠ backup works only on nest") + "\n")
				}
			}
			if (job.mode() == "nest" || job.mode() == "both") && m.config.Nest == nil {
				b.WriteString("     " + styleError.Render("⚠ nest needs to be configured") + "\n")
			}
		}
	}

	if m.restoreLoading {
		dots := []string{"·  ", "·· ", "···"}
		b.WriteString("\n  " + styleDim.Render("loading snapshots "+dots[m.animFrame%len(dots)]))
	} else {
		if m.restoreMsg != "" {
			b.WriteString("\n  " + styleWarning.Render("⚠ "+m.restoreMsg))
		}
		sep := styleDim.Render("  ·  ")
		if len(m.config.Jobs) > 0 {
			b.WriteString("\n  " + styleDim.Render("✓ ok  ·  ⚡ backup due  ·  ✗ disabled"))
		}
		b.WriteString("\n  " + strings.Join([]string{
			keyHint("↑↓", "navigate", colorDim),
			keyHint("enter", "job actions", colorGreen),
			keyHint("c", "create new", colorOrange),
			keyHint("esc", "back", colorDim),
		}, sep))
	}
	return b.String()
}

func (m model) viewAdd() string {
	var b strings.Builder
	b.WriteString(renderHeader(""))
	b.WriteString("\n")

	lw := labelWidth(m.windowWidth)
	for i, label := range formLabels {
		lStyle := styleLabel.Copy().Width(lw)
		var val string

		if i == m.formStep {
			lStyle = lStyle.Copy().Foreground(colorGreen)
			val = m.formInputs[i].View()
		} else if i < m.formStep {
			val = styleNormal.Render(m.formInputs[i].Value())
		} else {
			val = styleDim.Render(m.formInputs[i].Placeholder)
		}

		b.WriteString("  " + lStyle.Render(label+":") + "  " + val + "\n")
		if i == m.formStep && i == 5 {
			b.WriteString("  " + styleDim.Render("                 saves space but takes longer to backup") + "\n")
		}
	}

	if m.formErr != "" {
		b.WriteString("\n  " + styleError.Render("⚠ "+m.formErr) + "\n")
	}

	b.WriteString(styleHint.Render("\n  enter next · shift+tab back · esc cancel"))
	return b.String()
}

var buzzFrames = []string{"bzz", "bzz ·", "bzz ··", "bzz ···"}

func (m model) viewRun() string {
	if m.runDone {
		return m.viewRunDone()
	}

	var b strings.Builder

	// center the fly within the panel's inner content area (not the full
	// terminal width, since it now renders inside a bordered panel)
	innerWidth := contentWidth(m.windowWidth)
	const flyWidth = 7
	pad := ""
	if innerWidth > flyWidth {
		pad = strings.Repeat(" ", (innerWidth-flyWidth)/2)
	}

	b.WriteString("\n\n\n")
	for _, line := range strings.Split(renderFlyLines(m.animFrame), "\n") {
		b.WriteString(pad + line + "\n")
	}
	b.WriteString("\n")

	status := buzzFrames[m.animFrame%len(buzzFrames)]
	if len(m.runOutput) > 0 {
		last := strings.TrimSpace(m.runOutput[len(m.runOutput)-1])
		if last != "" {
			status = last
		}
	}
	statusLine := lipgloss.NewStyle().
		Width(innerWidth).
		Align(lipgloss.Center).
		Foreground(colorDim).
		Render(status)
	b.WriteString(statusLine + "\n")

	return b.String()
}

func (m model) viewRunDone() string {
	var b strings.Builder
	b.WriteString(renderHeader(""))
	b.WriteString("\n")

	lines := m.runOutput
	if len(lines) > 18 {
		lines = lines[len(lines)-18:]
	}
	for _, line := range lines {
		b.WriteString("  " + line + "\n")
	}

	b.WriteString(styleHint.Render("\n  enter to go back"))
	return b.String()
}

func (m model) viewEdit() string {
	var b strings.Builder
	b.WriteString(renderHeader(""))
	b.WriteString("\n")

	lw := labelWidth(m.windowWidth)
	for i, label := range formLabels {
		lStyle := styleLabel.Copy().Width(lw)
		var val string
		if i == m.formStep {
			lStyle = lStyle.Copy().Foreground(colorLavender)
			val = m.formInputs[i].View()
		} else if i < m.formStep {
			val = styleNormal.Render(m.formInputs[i].Value())
		} else {
			val = styleDim.Render(m.formInputs[i].Value())
		}
		b.WriteString("  " + lStyle.Render(label+":") + "  " + val + "\n")
	}

	b.WriteString(styleHint.Render("\n  enter next · shift+tab back · esc cancel"))
	return b.String()
}

// — Restore —

func (m model) updateRestore(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.restoreCursor > 0 {
			m.restoreCursor--
		}
	case "down", "j":
		if m.restoreCursor < len(m.restoreSnaps)-1 {
			m.restoreCursor++
		}
	case "enter":
		if len(m.restoreSnaps) > 0 {
			m.page = pageConfirmRestore
		}
	}
	return m, nil
}

func (m model) updateConfirmRestore(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		if m.running {
			return m, nil
		}
		if m.restoreJob != nil && m.restoreCursor < len(m.restoreSnaps) {
			snap := m.restoreSnaps[m.restoreCursor]
			job := m.restoreJob
			nest := m.config.Nest
			m.running = true
			m.runCancel = nil
			m.page = pageRun
			m.runOutput = nil
			m.runDone = false
			m.restoreJob = nil
			m.restoreSnaps = nil
			return m, func() tea.Msg {
				ch := make(chan string, 128)
				errCh := make(chan error, 1)
				go func() {
					defer close(ch)
					if snap.Source == "nest" && nest != nil {
						errCh <- runRestoreFromNest(job, snap.Name, nest, ch)
					} else {
						errCh <- runRestore(job, snap.Name, ch)
					}
				}()
				return runStartedMsg{ch: ch, errCh: errCh}
			}
		}
	case "n", "esc":
		m.page = pageRestore
	}
	return m, nil
}

func (m model) viewRestore() string {
	var b strings.Builder

	jobName := ""
	jobDest := ""
	if m.restoreJob != nil {
		jobName = m.restoreJob.Name
		jobDest = expandPath(m.restoreJob.Destination)
	}
	b.WriteString(renderHeader(jobName))
	b.WriteString("\n")

	// path header
	pathStyle := lipgloss.NewStyle().Foreground(colorDim)
	rule := strings.Repeat("─", dividerWidth(m.windowWidth))
	b.WriteString("  " + pathStyle.Render(jobDest+"/") + "\n")
	b.WriteString("  " + styleDim.Render(rule) + "\n\n")

	// find max size for bar scaling
	var maxSize int64 = 1
	for _, s := range m.restoreSnaps {
		if s.Size > maxSize {
			maxSize = s.Size
		}
	}

	const barWidth = 14
	const maxVisible = 12

	// scroll window: keep cursor visible
	scrollOffset := 0
	if m.restoreCursor >= maxVisible {
		scrollOffset = m.restoreCursor - maxVisible + 1
	}
	visible := m.restoreSnaps
	if len(visible) > maxVisible {
		end := scrollOffset + maxVisible
		if end > len(visible) {
			end = len(visible)
		}
		visible = visible[scrollOffset:end]
	}

	for ii, snap := range visible {
		i := ii + scrollOffset
		selected := i == m.restoreCursor

		date, timeStr := snapshotDateTime(snap.Name)

		// bar and size — hidden when size is unknown (nest-only)
		sizeKnown := snap.Size > 0
		filled := 0
		if sizeKnown {
			filled = int(float64(snap.Size) / float64(maxSize) * barWidth)
			if filled < 1 {
				filled = 1
			}
		}
		var bar, sizeLabel string
		if sizeKnown {
			bar = strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
			sizeLabel = formatBytes(snap.Size)
		} else {
			bar = strings.Repeat(" ", barWidth)
			sizeLabel = "—"
		}

		// source badge
		var sourceBadge string
		switch snap.Source {
		case "nest":
			sourceBadge = lipgloss.NewStyle().Foreground(colorGreen).Render(" [nest]")
		case "both":
			sourceBadge = lipgloss.NewStyle().Foreground(colorGreen).Render(" [nest]") +
				lipgloss.NewStyle().Foreground(colorLavender).Render("+local")
		default:
			sourceBadge = lipgloss.NewStyle().Foreground(colorBorder).Render(" [local]")
		}

		// latest badge
		badge := ""
		if i == 0 {
			badge = lipgloss.NewStyle().Foreground(colorGreen).Render(" latest")
		}

		cursor := "  "
		if selected {
			cursor = lipgloss.NewStyle().Foreground(colorViolet).Bold(true).Render("▸ ")
		}

		dateS := lipgloss.NewStyle().Foreground(colorDim).Render(date)
		timeS := lipgloss.NewStyle().Foreground(colorBorder).Render(timeStr)
		barS := lipgloss.NewStyle().Foreground(colorViolet).Render(bar)
		sizeS := lipgloss.NewStyle().Foreground(colorDim).Width(9).Render(sizeLabel)

		if selected {
			dateS = lipgloss.NewStyle().Foreground(colorWhite).Bold(true).Render(date)
			timeS = lipgloss.NewStyle().Foreground(colorLavender).Render(timeStr)
			barS = lipgloss.NewStyle().Foreground(colorLavender).Render(bar)
			sizeS = lipgloss.NewStyle().Foreground(colorWhite).Bold(true).Width(9).Render(sizeLabel)
		}

		b.WriteString("  " + cursor + dateS + "  " + timeS + "  " + barS + "  " + sizeS + sourceBadge + badge + "\n")
	}

	// footer
	total := len(m.restoreSnaps)
	maxSnap := 0
	if m.restoreJob != nil {
		maxSnap = m.restoreJob.MaxSnapshots
	}
	footerParts := fmt.Sprintf("%d snapshots", total)
	if maxSnap > 0 {
		footerParts += fmt.Sprintf("  ·  limit: %d", maxSnap)
	}
	if total > maxVisible {
		footerParts += fmt.Sprintf("  ·  %d–%d of %d", scrollOffset+1, scrollOffset+len(visible), total)
	}
	b.WriteString("\n  " + styleDim.Render(rule) + "\n")
	b.WriteString("  " + styleDim.Render(footerParts) + "\n")
	b.WriteString(styleHint.Render("\n  ↑↓ navigate · enter restore · esc back"))
	return b.String()
}

func (m model) updateNest(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.nestInputMode {
		return m.updateNestInput(msg)
	}
	return m.updateNestMenu(msg)
}

func (m model) nestMenuItems() []string {
	var items []string

	if m.config.Nest != nil {
		items = append(items, "Change server code")
		items = append(items, "Disable nest")
	} else {
		items = append(items, "Enter server code")
	}

	if !m.nestTSStatus.installed {
		items = append(items, "Setup Tailscale")
	} else if m.nestTSStatus.loggedIn {
		if m.nestTSStatus.running {
			items = append(items, "Disable Tailscale")
		} else {
			items = append(items, "Enable Tailscale")
		}
		items = append(items, "Logout from Tailscale")
	} else {
		items = append(items, "Login to Tailscale")
	}

	items = append(items, "Back")
	return items
}

func (m model) updateNestMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.nestMenuItems()
	switch msg.String() {
	case "esc":
		m.page = pageMenu
		m.nestErr = ""
		return m, nil
	case "up", "k":
		if m.nestPageCursor > 0 {
			m.nestPageCursor--
		}
	case "down", "j":
		if m.nestPageCursor < len(items)-1 {
			m.nestPageCursor++
		}
	case "enter":
		switch items[m.nestPageCursor] {
		case "Setup Tailscale":
			return m, installTailscaleCmd()
		case "Login to Tailscale":
			return m, tailscaleLoginCmd()
		case "Enable Tailscale":
			return m, tailscaleUpCmd()
		case "Disable Tailscale":
			m.nestConnected = false
			return m, tailscaleDownCmd()
		case "Logout from Tailscale":
			m.nestConnected = false
			return m, tailscaleLogoutCmd()
		case "Disable nest":
			m.config.Nest = nil
			m.nestConnected = false
			m.config.save()
		case "Enter server code", "Change server code":
			ti := textinput.New()
			ti.Placeholder = "e.g. oak-fox-red-ice-9f3a2b…"
			ti.Width = 56
			if m.config.Nest != nil {
				ti.SetValue(m.config.Nest.Address)
			}
			ti.Focus()
			m.nestInput = ti
			m.nestInputMode = true
			return m, textinput.Blink
		case "Back":
			m.page = pageMenu
			m.nestErr = ""
			return m, nil
		}
	}
	return m, nil
}

func (m model) updateNestInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.nestInputMode = false
		return m, nil
	case "enter":
		raw := strings.TrimSpace(m.nestInput.Value())
		if raw == "" {
			m.config.Nest = nil
			m.nestConnected = false
			m.config.save()
			m.nestInputMode = false
			return m, nil
		}
		address, token, err := decodeNestCode(raw)
		if err != nil {
			m.nestErr = err.Error()
			return m, nil
		}
		m.config.Nest = &NestConfig{Address: address, Token: token}
		if err := m.config.save(); err != nil {
			m.nestErr = "could not save: " + err.Error()
			return m, nil
		}
		m.nestChecking = true
		m.nestErr = ""
		m.nestInputMode = false
		return m, nestHealthCmd(address)
	}
	var cmd tea.Cmd
	m.nestInput, cmd = m.nestInput.Update(msg)
	return m, cmd
}

func (m model) viewNest() string {
	if m.nestInputMode {
		return m.viewNestInput()
	}
	return m.viewNestMenu()
}

func (m model) viewNestMenu() string {
	var b strings.Builder
	b.WriteString(renderHeader(""))
	b.WriteString("\n")

	tsLine := "  Tailscale   "
	if !m.nestTSStatus.installed {
		tsLine += styleError.Render("○ not installed")
	} else if m.nestTSStatus.running {
		tsLine += styleSuccess.Render("● connected  ") + styleDim.Render(m.nestTSStatus.ip)
	} else if m.nestTSStatus.loggedIn {
		tsLine += styleWarning.Render("○ logged in, not connected")
	} else {
		tsLine += styleWarning.Render("○ logged out")
	}
	b.WriteString(tsLine + "\n")

	nestLine := "  Nest        "
	if m.nestChecking {
		nestLine += styleDim.Render("checking...")
	} else if m.nestConnected && m.config.Nest != nil {
		nestLine += styleSuccess.Render("● connected  ") + styleDim.Render(m.config.Nest.Address)
	} else if m.config.Nest != nil {
		nestLine += styleWarning.Render("○ " + m.config.Nest.Address)
	} else {
		nestLine += styleDim.Render("○ not set up")
	}
	b.WriteString(nestLine + "\n\n")

	items := m.nestMenuItems()
	for i, item := range items {
		if i == m.nestPageCursor {
			b.WriteString("  " + styleSelected.Render("▸ "+item) + "\n")
		} else {
			b.WriteString("  " + styleDim.Render("  "+item) + "\n")
		}
	}

	if m.nestErr != "" {
		b.WriteString("\n  " + styleError.Render("✗ "+m.nestErr) + "\n")
	}

	b.WriteString(styleHint.Render("\n  ↑↓ navigate · enter select · esc back"))
	return b.String()
}

func (m model) viewNestInput() string {
	var b strings.Builder
	b.WriteString(renderHeader("address"))
	b.WriteString("\n")
	b.WriteString(styleDim.Render("  enter the short code from zipp-nest Connection info:\n\n"))
	b.WriteString("  " + m.nestInput.View() + "\n")

	if m.nestErr != "" {
		b.WriteString("\n  " + styleError.Render("✗ "+m.nestErr) + "\n")
	}

	b.WriteString(styleHint.Render("\n  enter save · esc back"))
	return b.String()
}

func (m model) viewConfirmRestore() string {
	var b strings.Builder
	b.WriteString(renderHeader(""))
	b.WriteString("\n")

	if m.restoreJob != nil && m.restoreCursor < len(m.restoreSnaps) {
		snap := m.restoreSnaps[m.restoreCursor]
		dst := m.restoreJob.Source
		sourceLabel := "local"
		var sourceColor lipgloss.TerminalColor = colorBorder
		if snap.Source == "nest" {
			sourceLabel = "nest"
			sourceColor = colorGreen
		} else if snap.Source == "both" {
			sourceLabel = "nest + local"
			sourceColor = colorGreen
		}
		b.WriteString("  " + styleDim.Render("snapshot:  ") + styleNormal.Render(snap.Name) + "\n")
		b.WriteString("  " + styleDim.Render("source:    ") + lipgloss.NewStyle().Foreground(sourceColor).Render(sourceLabel) + "\n")
		b.WriteString("  " + styleDim.Render("restore to: ") + styleNormal.Render(dst) + "\n\n")
		b.WriteString("  " + styleWarning.Render("⚠ this will overwrite existing files") + "\n\n")
		b.WriteString("  " + styleError.Render("[y]") + styleDim.Render(" yes    ") + styleDim.Render("[n / esc]") + styleDim.Render(" cancel") + "\n")
	}
	return b.String()
}

func (m model) viewConfirmDelete() string {
	var b strings.Builder
	b.WriteString(renderHeader(""))
	b.WriteString("\n")

	if m.deleteTarget != nil {
		b.WriteString("  " + styleDim.Render("do you really want to delete ") + styleError.Render(m.deleteTarget.Name) + styleDim.Render("?") + "\n\n")
		b.WriteString("  " + styleError.Render("[y]") + styleDim.Render(" yes    ") + styleDim.Render("[n / esc]") + styleDim.Render(" no") + "\n")
	}
	return b.String()
}
