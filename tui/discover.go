package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/chupakbra/proxmox-cli/internal/discovery"
)

// discoveryDoneMsg is sent when network discovery completes.
type discoveryDoneMsg struct {
	result *discovery.Result
	err    error
}

// discoverInstancesCmd returns a tea.Cmd that scans the given subnets
// (or local subnets if empty) for Proxmox instances.
func discoverInstancesCmd(subnets []string) tea.Cmd {
	return func() tea.Msg {
		result, err := discovery.Scan(subnets)
		return discoveryDoneMsg{result: result, err: err}
	}
}
