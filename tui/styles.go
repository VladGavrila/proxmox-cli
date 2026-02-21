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
	StyleError    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	StyleSuccess  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	StyleWarning  = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	StyleDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	StyleRunning  = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true)
	StyleStopped  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	StyleSpinner  = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
)

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
