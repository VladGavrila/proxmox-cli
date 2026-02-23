package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
)

var (
	StyleTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	StyleSubtitle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	StyleHelp     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	StyleKey      = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	StyleTag      = lipgloss.NewStyle().Foreground(lipgloss.Color("130"))
	StyleError    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	StyleSuccess  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	StyleWarning  = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	StyleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	StyleRunning  = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true)
	StyleStopped  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	StyleSpinner  = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
)

// renderHelp renders a help string with keybinding hints — text inside [ ] —
// highlighted in gold, and the surrounding description text in the dim help colour.
func renderHelp(s string) string {
	var b strings.Builder
	for {
		open := strings.IndexByte(s, '[')
		if open < 0 {
			break
		}
		close := strings.IndexByte(s[open:], ']')
		if close < 0 {
			break
		}
		close += open
		if open > 0 {
			b.WriteString(StyleHelp.Render(s[:open]))
		}
		b.WriteString(StyleKey.Render(s[open : close+1]))
		s = s[close+1:]
	}
	if len(s) > 0 {
		b.WriteString(StyleHelp.Render(s))
	}
	return b.String()
}

// headerLine places left-aligned text and a right-aligned refreshed timestamp on the same line.
// width is the full terminal width; padding (4) is subtracted for the content area.
func headerLine(left string, width int, t time.Time) string {
	right := "Refreshed: " + formatRefreshTime(t)
	contentWidth := width - 4 // account for outer Padding(1,2)
	leftLen := lipgloss.Width(left)
	rightLen := len(right)
	gap := contentWidth - leftLen - rightLen
	if gap < 2 {
		gap = 2
	}
	return left + strings.Repeat(" ", gap) + StyleDim.Render(right)
}

// CLISpinner matches the braille spinner used in the CLI output.
var CLISpinner = spinner.Spinner{
	Frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	FPS:    time.Second / 10,
}
