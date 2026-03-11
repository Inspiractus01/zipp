package main

import (
	"fmt"
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

type updateCheckMsg updateResult

type model struct {
	page   page
	cursor int
	config *Config
	err    error

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
}

var menuItemsBase = []string{"Jobs", "Run all", "Add job", "Setup", "Quit"}

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
	return tea.Batch(
		func() tea.Msg { return updateCheckMsg(checkForUpdate()) },
		checkSchedulerCmd(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
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
		}

	case updateCheckMsg:
		m.updateInfo = updateResult(msg)
		return m, nil

	case schedulerCheckMsg:
		m.schedulerInfo = schedulerStatus(msg)
		return m, nil

	// Streaming job started — kick off ticker + first line read.
	case runStartedMsg:
		m.runCh = msg.ch
		m.runErrCh = msg.errCh
		return m, tea.Batch(nextLineCmd(msg.ch, msg.errCh), tickCmd())

	// One line arrived from the job.
	case jobLineMsg:
		m.runOutput = append(m.runOutput, string(msg))
		return m, nextLineCmd(m.runCh, m.runErrCh)

	// Streaming job finished.
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

	// Animation tick.
	case tickMsg:
		m.animFrame++
		if !m.runDone {
			return m, tickCmd()
		}
		return m, nil

	// Non-streaming result (setup, update cmds).
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
				if err := runJob(job, ch); err != nil {
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
			if err := runJob(job, ch); err != nil {
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
		case "Add job":
			m.page = pageAdd
			m.cursor = 0
			m.formStep = 0
			m.formInputs = newFormInputs()
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
	return inputs
}

var formLabels = []string{"Name", "Source", "Destination", "Interval (h)", "Max snapshots"}

func (m model) updateAdd(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if m.formStep < len(m.formInputs)-1 {
			m.formInputs[m.formStep].Blur()
			m.formStep++
			m.formInputs[m.formStep].Focus()
			return m, textinput.Blink
		}
		// save
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
	dest := strings.TrimSpace(m.formInputs[2].Value())
	if name == "" || src == "" || dest == "" {
		return nil
	}

	interval := 0
	fmt.Sscanf(m.formInputs[3].Value(), "%d", &interval)

	maxSnap := 10
	if m.formInputs[4].Value() != "" {
		fmt.Sscanf(m.formInputs[4].Value(), "%d", &maxSnap)
	}

	return &Job{
		ID:            fmt.Sprintf("%x", time.Now().UnixNano()),
		Name:          name,
		Source:        src,
		Destination:   dest,
		IntervalHours: interval,
		MaxSnapshots:  maxSnap,
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
		b.WriteString("  " + styleUpdate.Render("↑ update available: v"+m.updateInfo.latest) + "\n")
		b.WriteString("  " + styleDim.Render("  curl -sL https://raw.githubusercontent.com/Inspiractus01/zipp/main/install.sh | bash") + "\n")
	}

	b.WriteString(styleHint.Render("\n  ↑↓ navigate · enter select · q quit"))
	return b.String()
}

func (m model) viewJobs() string {
	var b strings.Builder
	b.WriteString(renderHeader("Jobs"))
	b.WriteString("\n")

	if len(m.config.Jobs) == 0 {
		b.WriteString(styleDim.Render("  no jobs yet — press esc and add one\n"))
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

			name := job.Name
			if selected {
				name = styleSelected.Render(name)
			} else {
				name = styleNormal.Render(name)
			}

			next := styleDim.Render(job.nextRun())

			prefix := "  "
			if selected {
				prefix = styleSelected.Render("▸ ")
			}

			line := fmt.Sprintf("%s%s %s", prefix, indicator+"  "+name, next)
			b.WriteString(lipgloss.NewStyle().Width(60).Render(line) + "\n")
		}
	}

	b.WriteString(styleHint.Render("\n  ↑↓ navigate · enter run · e edit · d delete · t toggle · esc back"))
	return b.String()
}

func (m model) viewAdd() string {
	var b strings.Builder
	b.WriteString(renderHeader("Add job"))
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
	}

	b.WriteString(styleHint.Render("\n  enter next · shift+tab back · esc cancel"))
	return b.String()
}

var buzzFrames = []string{"bzz", "bzz ·", "bzz ··", "bzz ···"}

func (m model) viewRun() string {
	var b strings.Builder

	if !m.runDone {
		b.WriteString(renderAnimatedHeader("Running", m.animFrame))
	} else {
		b.WriteString(renderHeader("Done"))
	}
	b.WriteString("\n")

	// Show at most last 15 lines so it doesn't overflow
	lines := m.runOutput
	if len(lines) > 15 {
		lines = lines[len(lines)-15:]
	}
	for _, line := range lines {
		b.WriteString("  " + line + "\n")
	}

	if !m.runDone {
		b.WriteString("\n  " + styleDim.Render(buzzFrames[m.animFrame%len(buzzFrames)]))
	} else {
		b.WriteString(styleHint.Render("\n  enter to go back"))
	}

	return b.String()
}

func (m model) viewEdit() string {
	var b strings.Builder
	b.WriteString(renderHeader("Edit job"))
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
