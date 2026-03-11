package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	colorPurple   = lipgloss.Color("#451ce8")
	colorViolet   = lipgloss.Color("#7b5ef8")
	colorLavender = lipgloss.Color("#a78bfa")
	colorFuchsia  = lipgloss.Color("#c084fc")
	colorGray     = lipgloss.Color("#4b5563")
	colorMuted    = lipgloss.Color("#6b7280")
	colorWhite    = lipgloss.Color("#e2e8f0")
	colorGreen    = lipgloss.Color("#86efac")
	colorRed      = lipgloss.Color("#f87171")
	colorYellow   = lipgloss.Color("#fbbf24")

	styleSelected = lipgloss.NewStyle().
			Foreground(colorLavender).
			Bold(true)

	styleNormal = lipgloss.NewStyle().
			Foreground(colorWhite)

	styleDim = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleSuccess = lipgloss.NewStyle().
			Foreground(colorGreen)

	styleWarning = lipgloss.NewStyle().
			Foreground(colorYellow)

	styleError = lipgloss.NewStyle().
			Foreground(colorRed)

	styleLabel = lipgloss.NewStyle().
			Foreground(colorMuted).
			Width(16)

	styleHint = lipgloss.NewStyle().
			Foreground(colorGray).
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
			Foreground(colorGray)
)

var flyWingFrames = []string{
	`._q0p_.`, // wings normal
	`._/0\_.`, // wings up
	`.~~0~~.`, // buzzing
	`._\0/_.`, // wings down
}

var flySosakFrames = []string{
	"  `·´", // extended  — · aligned with head center
	"   · ", // retracted — · aligned with head center
	"  `·´", // extended
	"   · ", // retracted
}

var styleSosak = lipgloss.NewStyle().Foreground(colorRed)

func renderFlyLines(frame int) string {
	idx := frame % len(flyWingFrames)
	return styleSosak.Render(flySosakFrames[idx]) + "\n" +
		styleLogo.Render(flyWingFrames[idx]) + "\n" +
		styleLogo.Render(`'=(`) + styleLogoAccent.Render(`_`) + styleLogo.Render(`)='`) + "\n" +
		styleLogo.Render(` / `) + styleLogoAccent.Render(`V`) + styleLogo.Render(` \`) + "\n" +
		styleLogo.Render(`(_/`) + styleLogoAccent.Render(`^`) + styleLogo.Render(`\_)`)
}

// renderFlyOnly returns just the fly with no name/version beside it.
func renderFlyOnly(frame int) string {
	return renderFlyLines(frame)
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

// FlyArt is the detailed fly ASCII art used in the README / help text.
const FlyArt = `
    \    /\    /
     \  /  \  /
     (●      ●)
      \______/
        ||||
       /||||\
`

// FlyArtLines returns FlyArt as trimmed non-empty lines.
func FlyArtLines() []string {
	var out []string
	for _, l := range strings.Split(FlyArt, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
