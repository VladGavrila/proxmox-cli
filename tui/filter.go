package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// tableFilter is a value type that provides live filtering for table rows.
// Each screen/tab owns its own instance. Press "/" to activate, type to filter,
// Esc/Enter to deactivate (keeping the filter text applied), Ctrl+U to clear.
type tableFilter struct {
	active bool   // true when the filter input is focused
	text   string // current filter text (persists after deactivation)
}

// handleKey processes key events when the filter is active. Returns the updated
// filter and a rebuild flag indicating whether the table rows need to be rebuilt.
func (f tableFilter) handleKey(msg tea.KeyMsg) (tableFilter, bool) {
	switch msg.String() {
	case "esc", "enter":
		f.active = false
		return f, false
	case "backspace":
		if len(f.text) > 0 {
			f.text = f.text[:len(f.text)-1]
			return f, true
		}
		return f, false
	case "ctrl+u":
		if f.text != "" {
			f.text = ""
			return f, true
		}
		return f, false
	default:
		// Only accept printable runes.
		runes := msg.Runes
		if len(runes) > 0 {
			f.text += string(runes)
			return f, true
		}
		return f, false
	}
}

// matches returns true if any of the given fields contain the filter text as a
// case-insensitive substring. Returns true when no filter is set.
func (f tableFilter) matches(fields ...string) bool {
	if f.text == "" {
		return true
	}
	lower := strings.ToLower(f.text)
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), lower) {
			return true
		}
	}
	return false
}

// hasActiveFilter returns true when filter text is non-empty (filter is applied).
func (f tableFilter) hasActiveFilter() bool {
	return f.text != ""
}

// clear resets the filter to its zero state.
func (f *tableFilter) clear() {
	f.active = false
	f.text = ""
}

// renderLine returns a styled status line for the filter. Returns empty string
// when no filter is set and not active.
func (f tableFilter) renderLine() string {
	if f.active {
		return renderHelp("[/] Filter: ") + StyleWarning.Render(f.text+"_") + renderHelp("  [Ctrl+U] clear  [Esc] close")
	}
	if f.text != "" {
		return renderHelp("[/] Filter: ") + StyleWarning.Render(f.text) + renderHelp("  [Ctrl+U] clear")
	}
	return ""
}
