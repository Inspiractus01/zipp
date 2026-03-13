package main

import (
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

	// run view
	runOutput []string
	runDone   bool

	// streaming state
	runCh    <-chan string
	runErrCh <-chan error
	animFrame int

	// delete confirm
	deleteTarget *Job

	// edit
	editTarget *Job

	// restore
	restoreJob    *Job
	restoreSnaps  []string
	restoreCursor int

	// nest
	nestInput      textinput.Model
	nestErr        string
	nestConnected  bool
	nestChecking   bool
	nestTSStatus   tailscaleStatus
	nestPageCursor int
	nestInputMode  bool
}

var menuItemsBase = []string{"Jobs", "Run all", "Setup", "Nest", "Quit"}

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
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.windowWidth = msg.Width
		return m, nil

	case tea.KeyMsg:
		if m.page == pageNest {
			return m.updateNest(msg)
		}
		switch msg.String() {
		case "ctrl+c", "q":
			if m.page == pageMenu {
				return m, tea.Quit
			}
		case "esc":
			if m.page == pageAdd || m.page == pageEdit || m.page == pageJobs || m.page == pageRun {
				m.page = pageMenu
				m.cursor = 0
				m.formStep = 0
				m.formInputs = nil
				m.runOutput = nil
				m.runDone = false
			} else if m.page == pageConfirmDelete {
				m.page = pageJobs
				m.deleteTarget = nil
			} else if m.page == pageRestore {
				m.page = pageJobs
				m.restoreJob = nil
				m.restoreSnaps = nil
				m.restoreCursor = 0
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
		m.runOutput = msg.lines
		if msg.err != nil {
			m.runOutput = append(m.runOutput, styleError.Render("error: "+msg.err.Error()))
		} else {
			m.runOutput = append(m.runOutput, styleSuccess.Render("\n✓ all done"))
		}
		m.config.save()
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

func nextLineCmd(ch <-chan string, errCh <-chan error) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return jobDoneMsg{err: <-errCh}
		}
		return jobLineMsg(line)
	}
}

func runAllCmd(cfg *Config) tea.Cmd {
	return func() tea.Msg {
		ch := make(chan string, 256)
		errCh := make(chan error, 1)
		go func() {
			defer close(ch)
			var runErr error
			ran := 0
			for _, job := range cfg.Jobs {
				if !job.Enabled {
					continue
				}
				if err := runJob(job, cfg.Nest, ch); err != nil {
					ch <- styleError.Render("✗ " + err.Error())
					runErr = err
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

func runJobCmd(cfg *Config, job *Job) tea.Cmd {
	return func() tea.Msg {
		ch := make(chan string, 256)
		errCh := make(chan error, 1)
		go func() {
			defer close(ch)
			var runErr error
			if err := runJob(job, cfg.Nest, ch); err != nil {
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
			m.page = pageRun
			m.runOutput = nil
			m.runDone = false
			return m, runAllCmd(m.config)
		case "Setup":
			m.page = pageRun
			m.runOutput = nil
			m.runDone = false
			return m, setupSchedulerCmd()
		case "Run update":
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
			job := m.config.Jobs[m.cursor]
			m.page = pageRun
			m.runOutput = nil
			m.runDone = false
			return m, runJobCmd(m.config, job)
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
			switch job.mode() {
			case "local":
				job.NestMode = "nest"
			case "nest":
				job.NestMode = "both"
			default:
				job.NestMode = "local"
			}
			job.NestEnabled = false // clear legacy field
			m.config.save()
		}
	case "r":
		if len(m.config.Jobs) > 0 {
			job := m.config.Jobs[m.cursor]
			snaps, err := listSnapshots(job)
			if err != nil || len(snaps) == 0 {
				break
			}
			m.restoreJob = job
			m.restoreSnaps = snaps
			m.restoreCursor = 0
			m.page = pageRestore
		}
	case "c":
		m.page = pageAdd
		m.formStep = 0
		m.formInputs = newFormInputs()
	}
	return m, nil
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
		{"hours between backups, 0 = manual", 4},
		{"number of snapshots to keep", 4},
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
		if job != nil {
			m.config.addJob(job)
			m.config.save()
		}
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

func (m model) View() string {
	switch m.page {
	case pageMenu:
		return m.viewMenu()
	case pageJobs:
		return m.viewJobs()
	case pageAdd:
		return m.viewAdd()
	case pageEdit:
		return m.viewEdit()
	case pageRun:
		return m.viewRun()
	case pageConfirmDelete:
		return m.viewConfirmDelete()
	case pageRestore:
		return m.viewRestore()
	case pageConfirmRestore:
		return m.viewConfirmRestore()
	case pageNest:
		return m.viewNest()
	}
	return ""
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
		if item == "Nest" {
			if m.nestChecking {
				dots := []string{"·  ", "·· ", "···"}
				b.WriteString(styleDim.Render("   checking " + dots[m.animFrame%len(dots)]))
			} else if m.nestConnected {
				b.WriteString(styleSuccess.Render("   ✓ connected"))
			} else if m.config.Nest != nil {
				b.WriteString(styleWarning.Render("   ○ " + m.config.Nest.Address))
			} else {
				b.WriteString(styleDim.Render("   not set up"))
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if !m.schedulerInfo.active {
		b.WriteString("  " + styleError.Render("⚠ scheduler not running") +
			styleDim.Render("  → select Setup to fix") + "\n")
	} else {
		b.WriteString("  " + styleSuccess.Render("✓ scheduler active") +
			styleDim.Render(" via "+m.schedulerInfo.method) + "\n")
	}

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
	b.WriteString(renderHeader("Jobs"))
	b.WriteString("\n")

	if len(m.config.Jobs) == 0 {
		b.WriteString(styleDim.Render("  no jobs yet") + "\n")
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
			b.WriteString(lipgloss.NewStyle().Width(60).Render(line) + "\n")

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

	sep := styleDim.Render("  ·  ")
	b.WriteString("\n  " + strings.Join([]string{
		keyHint("↑↓", "navigate", colorMuted),
		keyHint("enter", "run", colorGreen),
		keyHint("e", "edit", colorYellow),
		keyHint("d", "delete", colorRed),
	}, sep))
	b.WriteString("\n  " + strings.Join([]string{
		keyHint("r", "restore", colorViolet),
		keyHint("t", "on/off", colorFuchsia),
		keyHint("n", "mode", colorGreen),
		keyHint("c", "create new", colorOrange),
	}, sep))
	b.WriteString("\n  " + keyHint("esc", "back", colorMuted))
	return b.String()
}

func (m model) viewAdd() string {
	var b strings.Builder
	b.WriteString(renderHeader("add job"))
	b.WriteString("\n")

	for i, label := range formLabels {
		lStyle := styleLabel
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

	b.WriteString(styleHint.Render("\n  enter next · shift+tab back · esc cancel"))
	return b.String()
}

var buzzFrames = []string{"bzz", "bzz ·", "bzz ··", "bzz ···"}

func (m model) viewRun() string {
	if m.runDone {
		return m.viewRunDone()
	}

	var b strings.Builder

	// center the fly horizontally (visual width ≈ 7 chars)
	const flyWidth = 7
	pad := ""
	if m.windowWidth > flyWidth {
		pad = strings.Repeat(" ", (m.windowWidth-flyWidth)/2)
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
	statusLine := styleDim.Render(status)
	if m.windowWidth > 0 {
		statusLine = lipgloss.NewStyle().
			Width(m.windowWidth).
			Align(lipgloss.Center).
			Foreground(colorMuted).
			Render(status)
	}
	b.WriteString(statusLine + "\n")

	return b.String()
}

func (m model) viewRunDone() string {
	var b strings.Builder
	b.WriteString(renderHeader("Done"))
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
	b.WriteString(renderHeader("edit job"))
	b.WriteString("\n")

	for i, label := range formLabels {
		lStyle := styleLabel
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
		if m.restoreJob != nil && m.restoreCursor < len(m.restoreSnaps) {
			snapshot := m.restoreSnaps[m.restoreCursor]
			job := m.restoreJob
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
					errCh <- runRestore(job, snapshot, ch)
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
	name := ""
	if m.restoreJob != nil {
		name = m.restoreJob.Name
	}
	b.WriteString(renderHeader("Restore — " + name))
	b.WriteString("\n")
	b.WriteString(styleDim.Render("  select a snapshot to restore:\n\n"))

	for i, snap := range m.restoreSnaps {
		label := snap
		if i == 0 {
			label += styleDim.Render("  ← newest")
		}
		if i == m.restoreCursor {
			b.WriteString("  " + styleSelected.Render("▸ "+label) + "\n")
		} else {
			b.WriteString("  " + styleDim.Render("  "+snap) + "\n")
		}
	}

	b.WriteString(styleHint.Render("\n  ↑↓ navigate · enter select · esc back"))
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
			ti.Placeholder = "short code (e.g. 6456-fd44)"
			ti.Width = 40
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
		address, err := decodeNestCode(raw)
		if err != nil {
			m.nestErr = err.Error()
			return m, nil
		}
		m.config.Nest = &NestConfig{Address: address}
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
	b.WriteString(renderHeader("Nest"))
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
	b.WriteString(renderHeader("Nest — address"))
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
	b.WriteString(renderHeader("Restore"))
	b.WriteString("\n")

	if m.restoreJob != nil && m.restoreCursor < len(m.restoreSnaps) {
		snap := m.restoreSnaps[m.restoreCursor]
		dst := m.restoreJob.Source
		b.WriteString("  " + styleDim.Render("snapshot:  ") + styleNormal.Render(snap) + "\n")
		b.WriteString("  " + styleDim.Render("restore to: ") + styleNormal.Render(dst) + "\n\n")
		b.WriteString("  " + styleWarning.Render("⚠ this will overwrite existing files") + "\n\n")
		b.WriteString("  " + styleError.Render("[y]") + styleDim.Render(" yes    ") + styleDim.Render("[n / esc]") + styleDim.Render(" cancel") + "\n")
	}
	return b.String()
}

func (m model) viewConfirmDelete() string {
	var b strings.Builder
	b.WriteString(renderHeader("Delete job"))
	b.WriteString("\n")

	if m.deleteTarget != nil {
		b.WriteString("  " + styleDim.Render("do you really want to delete ") + styleError.Render(m.deleteTarget.Name) + styleDim.Render("?") + "\n\n")
		b.WriteString("  " + styleError.Render("[y]") + styleDim.Render(" yes    ") + styleDim.Render("[n / esc]") + styleDim.Render(" no") + "\n")
	}
	return b.String()
}
