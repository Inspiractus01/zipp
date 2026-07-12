package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	colorPurple = lipgloss.AdaptiveColor{Light: "#3311a8", Dark: "#451ce8"}
	colorViolet = lipgloss.AdaptiveColor{Light: "#5b3fd6", Dark: "#7b5ef8"}
	// colorLavender is used for selected/emphasized text; darkened on light
	// backgrounds since the pastel dark-mode value is unreadable on white.
	colorLavender = lipgloss.AdaptiveColor{Light: "#6d28d9", Dark: "#a78bfa"}
	colorFuchsia  = lipgloss.AdaptiveColor{Light: "#9333ea", Dark: "#c084fc"}
	// colorBorder is a subdued neutral used for panel borders and version/
	// hint-key text — deliberately quieter than colorDim.
	colorBorder = lipgloss.AdaptiveColor{Light: "#94a3b8", Dark: "#4b5563"}
	// colorDim is the everyday "secondary text" color (labels, hints, dim
	// status lines) — a little lighter/softer-reading than colorBorder.
	colorDim = lipgloss.AdaptiveColor{Light: "#64748b", Dark: "#6b7280"}
	// colorWhite is primary foreground text — near-white on dark backgrounds,
	// near-black on light ones.
	colorWhite  = lipgloss.AdaptiveColor{Light: "#1e293b", Dark: "#e2e8f0"}
	colorGreen  = lipgloss.AdaptiveColor{Light: "#16a34a", Dark: "#86efac"}
	colorRed    = lipgloss.AdaptiveColor{Light: "#dc2626", Dark: "#f87171"}
	colorYellow = lipgloss.AdaptiveColor{Light: "#b45309", Dark: "#fbbf24"}
	colorOrange = lipgloss.AdaptiveColor{Light: "#c2410c", Dark: "#fb923c"}

	styleSelected = lipgloss.NewStyle().
			Foreground(colorLavender).
			Bold(true)

	styleNormal = lipgloss.NewStyle().
			Foreground(colorWhite)

	styleDim = lipgloss.NewStyle().
			Foreground(colorDim)

	styleSuccess = lipgloss.NewStyle().
			Foreground(colorGreen)

	styleWarning = lipgloss.NewStyle().
			Foreground(colorYellow)

	styleError = lipgloss.NewStyle().
			Foreground(colorRed)

	styleLabel = lipgloss.NewStyle().
			Foreground(colorDim).
			Width(16)

	styleHint = lipgloss.NewStyle().
			Foreground(colorBorder).
			PaddingTop(1)

	styleUpdate = lipgloss.NewStyle().
			Foreground(colorFuchsia).
			Bold(true)

	styleLogo = lipgloss.NewStyle().
			Foreground(colorViolet)

	styleLogoAccent = lipgloss.NewStyle().
			Foreground(colorPurple).
			Bold(true)

	styleVersion = lipgloss.NewStyle().
			Foreground(colorBorder)

	// styleNavBar is the small "ZIPP · PAGE" bar shown above every page's
	// bordered panel.
	styleNavBar = lipgloss.NewStyle().
			Foreground(colorViolet).
			Bold(true)

	// stylePanel is the single outer panel each page's content is rendered
	// into.
	stylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)
)

var flyWingFrames = []string{
	`  )()(`,
	`  /\/\`,
	`  ~~~~`,
	`  \/\/`,
}

func renderFlyLines(frame int) string {
	idx := frame % len(flyWingFrames)
	return styleLogo.Render(flyWingFrames[idx]) + "\n" +
		styleLogo.Render(" ( ") + styleLogoAccent.Render("●●") + styleLogo.Render(" )") + "\n" +
		styleLogo.Render(`  \──/`) + "\n" +
		styleLogo.Render(`  /||\`)
}

// keyHint renders a colored key + dim label for the hint bar.
func keyHint(key string, label string, c lipgloss.TerminalColor) string {
	return lipgloss.NewStyle().Foreground(c).Bold(true).Render(key) +
		styleDim.Render(" "+label)
}

func renderHeader(subtitle string) string {
	return buildHeader(subtitle, 0)
}

func renderAnimatedHeader(subtitle string, frame int) string {
	return buildHeader(subtitle, frame)
}

func buildHeader(subtitle string, frame int) string {
	logo := renderFlyLines(frame)

	name := lipgloss.NewStyle().
		Foreground(colorLavender).
		Bold(true).
		Render("zipp")

	ver := styleVersion.Render("v" + version)

	var sub string
	if subtitle != "" {
		sub = "\n" + styleDim.Render("  "+subtitle)
	}

	right := lipgloss.JoinVertical(lipgloss.Left,
		name+" "+ver+sub,
	)

	return lipgloss.JoinHorizontal(lipgloss.Center,
		logo+"  ",
		"\n\n\n"+right,
	) + "\n"
}

// pageHeader renders the one-line "ZIPP · PAGE" nav bar shown above every
// page's bordered panel, tying all screens together visually.
func pageHeader(title string) string {
	return styleNavBar.Render("ZIPP · " + strings.ToUpper(title))
}

// renderPanel wraps a page's rendered body in the single outer bordered
// panel, sized responsively from the terminal width.
func renderPanel(windowWidth int, body string) string {
	return stylePanel.Copy().Width(contentWidth(windowWidth)).Render(body)
}

// clampWidth returns w clamped to [min, max].
func clampWidth(w, min, max int) int {
	if w < min {
		return min
	}
	if w > max {
		return max
	}
	return w
}

// contentWidth is the responsive width used for the outer panel's content
// area (i.e. before lipgloss adds border + padding on top of it).
func contentWidth(windowWidth int) int {
	return clampWidth(windowWidth-6, 60, 100)
}

// jobRowWidth is the responsive width used to align the job list's
// indicator/name column against the "next run" column.
func jobRowWidth(windowWidth int) int {
	return clampWidth(windowWidth-26, 40, 84)
}

// dividerWidth is the responsive width used for "───" rule lines.
func dividerWidth(windowWidth int) int {
	return clampWidth(windowWidth-10, 40, 90)
}

// labelWidth is the responsive width used for form label columns.
func labelWidth(windowWidth int) int {
	return clampWidth(windowWidth/12, 16, 22)
}
