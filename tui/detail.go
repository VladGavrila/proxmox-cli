package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/chupakbra/proxmox-cli/internal/actions"
)

type detailMode int

const (
	detailNormal         detailMode = iota
	detailConfirmDelete             // waiting for Enter/Esc to confirm snapshot delete
	detailConfirmRollback           // waiting for Enter/Esc to confirm snapshot rollback
	detailInputName                 // text input active for new snapshot name
)

// snapshotEntry is a unified representation for both VM and CT snapshots.
type snapshotEntry struct {
	Name        string
	Parent      string
	Description string
	Date        string
}

// detailLoadedMsg is sent when snapshot loading completes.
type detailLoadedMsg struct {
	snapshots []snapshotEntry
	err       error
}

// actionResultMsg is sent after a power or snapshot action completes.
type actionResultMsg struct {
	message     string
	err         error
	needRefresh bool // true for power actions: triggers a resource status re-fetch
}

// reloadSnapshotsMsg is sent by create/delete commands to show feedback and
// trigger an automatic snapshot list reload.
type reloadSnapshotsMsg struct{ message string }

// resourceRefreshedMsg is sent after a resource status re-fetch completes.
type resourceRefreshedMsg struct {
	resource proxmox.ClusterResource
	err      error
}

type detailModel struct {
	client   *proxmox.Client
	resource proxmox.ClusterResource

	snapshots []snapshotEntry
	loading   bool
	loadErr   error

	snapTable  table.Model
	spinner    spinner.Model
	mode       detailMode
	input      textinput.Model
	statusMsg     string
	statusErr     bool
	actionBusy    bool
	lastRefreshed time.Time

	width  int
	height int
}

func newDetailModel(c *proxmox.Client, r proxmox.ClusterResource, w, h int) detailModel {
	ti := textinput.New()
	ti.Placeholder = "snapshot name"
	ti.CharLimit = 40

	s := spinner.New()
	s.Spinner = CLISpinner
	s.Style = StyleSpinner

	return detailModel{
		client:   c,
		resource: r,
		loading:  true,
		input:    ti,
		spinner:  s,
		width:    w,
		height:   h,
	}
}

func (m detailModel) withRebuiltTable() detailModel {
	if len(m.snapshots) == 0 {
		return m
	}

	descWidth := m.width - 20 - 18 - 15 - 12 // remainder after NAME+DATE+PARENT+padding
	if descWidth < 10 {
		descWidth = 10
	}

	cols := []table.Column{
		{Title: "NAME", Width: 20},
		{Title: "DATE", Width: 18},
		{Title: "PARENT", Width: 15},
		{Title: "DESCRIPTION", Width: descWidth},
	}

	rows := make([]table.Row, len(m.snapshots))
	for i, s := range m.snapshots {
		rows[i] = table.Row{s.Name, s.Date, s.Parent, s.Description}
	}

	// Fixed overhead lines in the detail view:
	// padding(1) + title + stats + "" + power + sep + statusMsg + "Snapshots(N)" = 9
	// help lines at bottom: snap actions + nav = 2
	// table header is internal to bubbles/table (counts as 1 extra line)
	// Total non-table lines: ~13
	tableHeight := m.height - 13
	if tableHeight < 3 {
		tableHeight = 3
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(tableHeight),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("255")).
		Background(lipgloss.Color("236")).
		Bold(false)
	t.SetStyles(s)
	m.snapTable = t
	return m
}

func (m detailModel) init() tea.Cmd {
	return tea.Batch(m.loadSnapshotsCmd(), m.spinner.Tick)
}

func (m detailModel) loadSnapshotsCmd() tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		var entries []snapshotEntry

		if r.Type == "qemu" {
			snaps, err := actions.VMSnapshots(ctx, c, int(r.VMID), r.Node)
			if err != nil {
				return detailLoadedMsg{err: err}
			}
			for _, s := range snaps {
				if s.Name == "current" {
					continue
				}
				entries = append(entries, snapshotEntry{
					Name:        s.Name,
					Parent:      s.Parent,
					Description: s.Description,
					Date:        formatSnapTime(s.Snaptime),
				})
			}
		} else {
			snaps, err := actions.ContainerSnapshots(ctx, c, int(r.VMID), r.Node)
			if err != nil {
				return detailLoadedMsg{err: err}
			}
			for _, s := range snaps {
				if s.Name == "current" {
					continue
				}
				entries = append(entries, snapshotEntry{
					Name:        s.Name,
					Parent:      s.Parent,
					Description: s.Description,
					Date:        formatSnapTime(s.SnapshotCreationTime),
				})
			}
		}
		return detailLoadedMsg{snapshots: entries}
	}
}

// refreshResourceCmd re-fetches the current resource's status from the cluster.
func (m detailModel) refreshResourceCmd() tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		cl, err := c.Cluster(ctx)
		if err != nil {
			return resourceRefreshedMsg{err: err}
		}
		all, err := cl.Resources(ctx, "vm")
		if err != nil {
			return resourceRefreshedMsg{err: err}
		}
		for _, res := range all {
			if res.VMID == r.VMID && res.Node == r.Node {
				return resourceRefreshedMsg{resource: *res}
			}
		}
		return resourceRefreshedMsg{err: fmt.Errorf("resource not found after refresh")}
	}
}

func formatSnapTime(t int64) string {
	if t == 0 {
		return "-"
	}
	return time.Unix(t, 0).Format("2006-01-02 15:04")
}

func (m detailModel) update(msg tea.Msg) (detailModel, tea.Cmd) {
	switch msg := msg.(type) {
	case detailLoadedMsg:
		m.loading = false
		m.actionBusy = false
		if msg.err != nil {
			m.loadErr = msg.err
		} else {
			m.loadErr = nil
			m.snapshots = msg.snapshots
			m.lastRefreshed = time.Now()
			m = m.withRebuiltTable()
		}
		return m, nil

	case reloadSnapshotsMsg:
		m.statusMsg = msg.message
		m.statusErr = false
		m.actionBusy = false
		m.loading = true
		return m, tea.Batch(m.loadSnapshotsCmd(), m.spinner.Tick)

	case actionResultMsg:
		m.actionBusy = false
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
			return m, nil
		}
		m.statusMsg = msg.message
		m.statusErr = false
		if msg.needRefresh {
			return m, m.refreshResourceCmd()
		}
		return m, nil

	case resourceRefreshedMsg:
		if msg.err == nil {
			m.resource = msg.resource
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.loading || m.actionBusy {
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		// Input mode: all keystrokes go to the text input.
		if m.mode == detailInputName {
			switch msg.String() {
			case "esc":
				m.mode = detailNormal
				m.input.Reset()
				m.input.Blur()
				return m, nil
			case "enter":
				name := strings.TrimSpace(m.input.Value())
				m.input.Reset()
				m.input.Blur()
				m.mode = detailNormal
				if name != "" {
					m.actionBusy = true
					m.statusMsg = fmt.Sprintf("Creating snapshot %q...", name)
					m.statusErr = false
					return m, tea.Batch(m.createSnapshotCmd(name), m.spinner.Tick)
				}
				return m, nil
			default:
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				return m, cmd
			}
		}

		// Confirm delete mode.
		if m.mode == detailConfirmDelete {
			switch msg.String() {
			case "enter":
				name := m.selectedSnapshotName()
				m.mode = detailNormal
				if name == "" {
					return m, nil
				}
				m.actionBusy = true
				m.statusMsg = fmt.Sprintf("Deleting snapshot %q...", name)
				m.statusErr = false
				return m, tea.Batch(m.deleteSnapshotCmd(name), m.spinner.Tick)
			case "esc":
				m.mode = detailNormal
				return m, nil
			}
			return m, nil
		}

		// Confirm rollback mode.
		if m.mode == detailConfirmRollback {
			switch msg.String() {
			case "enter":
				name := m.selectedSnapshotName()
				m.mode = detailNormal
				if name == "" {
					return m, nil
				}
				m.actionBusy = true
				m.statusMsg = fmt.Sprintf("Rolling back to %q...", name)
				m.statusErr = false
				return m, tea.Batch(m.rollbackSnapshotCmd(name), m.spinner.Tick)
			case "esc":
				m.mode = detailNormal
				return m, nil
			}
			return m, nil
		}

		// Normal mode: ignore keys during an in-flight action.
		if m.actionBusy {
			return m, nil
		}

		switch msg.String() {
		case "s":
			m.actionBusy = true
			m.statusMsg = "Starting..."
			m.statusErr = false
			return m, tea.Batch(m.powerCmd("start"), m.spinner.Tick)
		case "S":
			m.actionBusy = true
			m.statusMsg = "Stopping..."
			m.statusErr = false
			return m, tea.Batch(m.powerCmd("stop"), m.spinner.Tick)
		case "u":
			m.actionBusy = true
			m.statusMsg = "Shutting down..."
			m.statusErr = false
			return m, tea.Batch(m.powerCmd("shutdown"), m.spinner.Tick)
		case "r":
			m.actionBusy = true
			m.statusMsg = "Rebooting..."
			m.statusErr = false
			return m, tea.Batch(m.powerCmd("reboot"), m.spinner.Tick)
		case "ctrl+r", "f5":
			m.loading = true
			m.loadErr = nil
			return m, tea.Batch(m.loadSnapshotsCmd(), m.refreshResourceCmd(), m.spinner.Tick)
		case "n":
			m.mode = detailInputName
			m.input.Focus()
			return m, textinput.Blink
		case "d":
			if len(m.snapshots) == 0 {
				return m, nil
			}
			m.mode = detailConfirmDelete
			return m, nil
		case "R":
			if len(m.snapshots) == 0 {
				return m, nil
			}
			m.mode = detailConfirmRollback
			return m, nil
		case "esc":
			// Handled by appModel (navigates back to list).
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.snapTable, cmd = m.snapTable.Update(msg)
	return m, cmd
}

func (m detailModel) selectedSnapshotName() string {
	if len(m.snapshots) == 0 {
		return ""
	}
	cursor := m.snapTable.Cursor()
	if cursor < 0 || cursor >= len(m.snapshots) {
		return ""
	}
	return m.snapshots[cursor].Name
}

func (m detailModel) powerCmd(action string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		vmid := int(r.VMID)

		if r.Type == "qemu" {
			switch action {
			case "start":
				task, err = actions.StartVM(ctx, c, vmid, r.Node)
			case "stop":
				task, err = actions.StopVM(ctx, c, vmid, r.Node)
			case "shutdown":
				task, err = actions.ShutdownVM(ctx, c, vmid, r.Node)
			case "reboot":
				task, err = actions.RebootVM(ctx, c, vmid, r.Node)
			}
		} else {
			switch action {
			case "start":
				task, err = actions.StartContainer(ctx, c, vmid, r.Node)
			case "stop":
				task, err = actions.StopContainer(ctx, c, vmid, r.Node)
			case "shutdown":
				task, err = actions.ShutdownContainer(ctx, c, vmid, r.Node)
			case "reboot":
				task, err = actions.RebootContainer(ctx, c, vmid, r.Node)
			}
		}

		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 300); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return actionResultMsg{message: action + " completed", needRefresh: true}
	}
}

func (m detailModel) createSnapshotCmd(name string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		vmid := int(r.VMID)

		if r.Type == "qemu" {
			task, err = actions.CreateVMSnapshot(ctx, c, vmid, r.Node, name)
		} else {
			task, err = actions.CreateContainerSnapshot(ctx, c, vmid, r.Node, name)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 300); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return reloadSnapshotsMsg{message: fmt.Sprintf("Snapshot %q created", name)}
	}
}

func (m detailModel) deleteSnapshotCmd(name string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		vmid := int(r.VMID)

		if r.Type == "qemu" {
			task, err = actions.DeleteVMSnapshot(ctx, c, vmid, r.Node, name)
		} else {
			task, err = actions.DeleteContainerSnapshot(ctx, c, vmid, r.Node, name)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 300); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return reloadSnapshotsMsg{message: fmt.Sprintf("Snapshot %q deleted", name)}
	}
}

func (m detailModel) rollbackSnapshotCmd(name string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		vmid := int(r.VMID)

		if r.Type == "qemu" {
			task, err = actions.RollbackVMSnapshot(ctx, c, vmid, r.Node, name)
		} else {
			task, err = actions.RollbackContainerSnapshot(ctx, c, vmid, r.Node, name, false)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 300); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return actionResultMsg{message: fmt.Sprintf("Rolled back to %q", name), needRefresh: true}
	}
}

func (m detailModel) view() string {
	if m.width == 0 {
		return ""
	}

	r := m.resource
	typeStr := "VM"
	if r.Type == "lxc" {
		typeStr = "CT"
	}

	var statusStyled string
	if r.Status == "running" {
		statusStyled = StyleRunning.Render(r.Status)
	} else {
		statusStyled = StyleStopped.Render(r.Status)
	}

	title := StyleTitle.Render(fmt.Sprintf("%s %d: %s", typeStr, r.VMID, r.Name)) +
		"  " + statusStyled

	stats := StyleSubtitle.Render(fmt.Sprintf(
		"Node: %s   CPU: %s   Mem: %s / %s   Uptime: %s",
		r.Node,
		formatPercent(r.CPU),
		formatBytes(r.Mem),
		formatBytes(r.MaxMem),
		formatUptime(r.Uptime),
	))

	sep := StyleDim.Render(strings.Repeat("─", m.width-6))

	var lines []string
	lines = append(lines, headerLine(title, m.width, m.lastRefreshed))
	lines = append(lines, stats)
	lines = append(lines, StyleHelp.Render("[s] start  [S] stop  [u] shutdown  [r] reboot"))
	lines = append(lines, sep)

	// Status/spinner feedback line (always present for stable layout).
	switch {
	case m.actionBusy:
		lines = append(lines, StyleWarning.Render(m.spinner.View()+" "+m.statusMsg))
	case m.statusMsg != "" && m.statusErr:
		lines = append(lines, StyleError.Render(m.statusMsg))
	case m.statusMsg != "":
		lines = append(lines, StyleSuccess.Render(m.statusMsg))
	default:
		lines = append(lines, "")
	}

	lines = append(lines, StyleTitle.Render(fmt.Sprintf("Snapshots (%d)", len(m.snapshots))))

	switch {
	case m.loading:
		lines = append(lines, StyleWarning.Render(m.spinner.View()+" Loading snapshots..."))
	case m.loadErr != nil:
		lines = append(lines, StyleError.Render("  Error: "+m.loadErr.Error()))
		lines = append(lines, StyleHelp.Render("  [ctrl+r] retry"))
	case len(m.snapshots) == 0:
		lines = append(lines, StyleDim.Render("  No snapshots"))
	default:
		lines = append(lines, m.snapTable.View())
	}

	// Overlay modes.
	switch m.mode {
	case detailInputName:
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render("New snapshot name: ")+m.input.View())
		lines = append(lines, StyleHelp.Render("[Enter] confirm   [Esc] cancel"))

	case detailConfirmDelete:
		snapName := m.selectedSnapshotName()
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(
			fmt.Sprintf("Delete snapshot %q? [Enter] confirm   [Esc] cancel", snapName),
		))

	case detailConfirmRollback:
		snapName := m.selectedSnapshotName()
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(
			fmt.Sprintf("Rollback to %q? [Enter] confirm   [Esc] cancel", snapName),
		))

	default:
		lines = append(lines, "")
		if len(m.snapshots) > 0 {
			lines = append(lines, StyleHelp.Render("[n] new  [d] delete  [R] rollback  |  [ctrl+r] refresh"))
		} else {
			lines = append(lines, StyleHelp.Render("[n] new snapshot"))
		}
	}

	lines = append(lines, StyleHelp.Render("[Esc] back   [Q] quit"))

	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

// formatUptime converts seconds to a human-readable uptime string.
func formatUptime(seconds uint64) string {
	if seconds == 0 {
		return "-"
	}
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
