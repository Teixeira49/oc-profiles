package tui

// Styles and palette are adapted from gentle-ai v3.7.0
// (internal/tui/styles/styles.go, MIT, Gentleman-Programming/gentle-ai).

import "github.com/charmbracelet/lipgloss"

// Rose Pine color palette.
var (
	colorText     = lipgloss.Color("#e0def4")
	colorSubtext  = lipgloss.Color("#908caa")
	colorLavender = lipgloss.Color("#c4a7e7")
	colorGreen    = lipgloss.Color("#9ccfd8")
	colorRed      = lipgloss.Color("#eb6f92")
	colorMauve    = lipgloss.Color("#ebbcba")
	colorYellow   = lipgloss.Color("#f1ca93")
)

// cursorPrefix is the prefix used for the currently focused item.
const cursorPrefix = "▸ "

var (
	titleStyle      = lipgloss.NewStyle().Foreground(colorLavender).Bold(true)
	headingStyle    = lipgloss.NewStyle().Foreground(colorMauve).Bold(true)
	helpStyle       = lipgloss.NewStyle().Foreground(colorSubtext)
	subtextStyle    = lipgloss.NewStyle().Foreground(colorSubtext)
	selectedStyle   = lipgloss.NewStyle().Foreground(colorLavender).Bold(true)
	unselectedStyle = lipgloss.NewStyle().Foreground(colorText)
	successStyle    = lipgloss.NewStyle().Foreground(colorGreen)
	errorStyle      = lipgloss.NewStyle().Foreground(colorRed)
	warningStyle    = lipgloss.NewStyle().Foreground(colorYellow)
	frameStyle      = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(colorLavender).
			Padding(1, 2)
)

// renderOptions renders a vertical option list with the focused row marked.
func renderOptions(options []string, cursor int) string {
	out := ""
	for idx, option := range options {
		if idx == cursor {
			out += selectedStyle.Render(cursorPrefix+option) + "\n"
		} else {
			out += unselectedStyle.Render("  "+option) + "\n"
		}
	}
	return out
}
