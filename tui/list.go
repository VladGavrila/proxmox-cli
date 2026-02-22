package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
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

type listMode int

const (
	listNormal        listMode = iota
	listConfirmDelete          // waiting for Enter/Esc to confirm resource delete
	listCloneInput             // text inputs for clone VMID + name
)

// resourcesFetchedMsg is sent when the async fetch of VMs and containers completes.
type resourcesFetchedMsg struct {
	resources proxmox.ClusterResources
	err       error
	fetchID   int64 // matches listModel.fetchID; stale responses are discarded
}

type listModel struct {
	client        *proxmox.Client
	instName      string
	resources     proxmox.ClusterResources
	loading       bool
	err           error
	table         table.Model
	spinner       spinner.Model
	lastRefreshed time.Time
	fetchID       int64
	mode          listMode
	statusMsg     string
	statusErr     bool
	actionBusy    bool

	// Clone input state
	cloneIDInput   textinput.Model
	cloneNameInput textinput.Model
	cloneField     int // 0 = VMID, 1 = name

	width  int
	height int
}

func newListModel(c *proxmox.Client, instName string, w, h int) listModel {
	s := spinner.New()
	s.Spinner = CLISpinner
	s.Style = StyleSpinner

	cidInput := textinput.New()
	cidInput.Placeholder = "new VMID"
	cidInput.CharLimit = 10
	cidInput.Width = 12

	cnameInput := textinput.New()
	cnameInput.Placeholder = "clone name"
	cnameInput.CharLimit = 63

	return listModel{
		client:         c,
		instName:       instName,
		loading:        true,
		spinner:        s,
		fetchID:        time.Now().UnixNano(),
		cloneIDInput:   cidInput,
		cloneNameInput: cnameInput,
		width:          w,
		height:         h,
	}
}

// fixedColWidth is the total width of all columns except NAME.
// VMID(6) + TYPE(4) + TMPL(5) + NODE(12) + STATUS(10) + CPU(7) + MEM(10) + DISK(10) = 64
// Plus cell padding: 9 columns × 2 chars (1 left + 1 right per cell) = 18.
const fixedColWidth = 64 + 18

func (m listModel) nameColWidth() int {
	w := m.width - fixedColWidth - 4 // 4 for outer padding
	if w < 20 {
		w = 20
	}
	return w
}

func (m listModel) withRebuiltTable() listModel {
	nameWidth := m.nameColWidth()

	cols := []table.Column{
		{Title: "VMID", Width: 6},
		{Title: "TYPE", Width: 4},
		{Title: "TMPL", Width: 5},
		{Title: "NAME", Width: nameWidth},
		{Title: "NODE", Width: 12},
		{Title: "STATUS", Width: 10},
		{Title: "CPU", Width: 7},
		{Title: "MEM", Width: 10},
		{Title: "DISK", Width: 10},
	}

	rows := make([]table.Row, len(m.resources))
	for i, r := range m.resources {
		typeStr := "VM"
		if r.Type == "lxc" {
			typeStr = "CT"
		}
		tmpl := ""
		if r.Template == 1 {
			tmpl = "✓"
		}
		rows[i] = table.Row{
			fmt.Sprintf("%d", r.VMID),
			typeStr,
			tmpl,
			r.Name,
			r.Node,
			r.Status,
			formatPercent(r.CPU),
			formatBytes(r.Mem),
			formatBytes(r.MaxDisk),
		}
	}

	tableHeight := m.height - 11 // padding(2) + title(1) + blank(1) + status(1) + actions(1) + help(2) + table border(2) + margin(1)
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
	m.table = t
	return m
}

func (m listModel) init() tea.Cmd {
	return tea.Batch(fetchAllResources(m.client, m.fetchID), m.spinner.Tick)
}

func (m listModel) update(msg tea.Msg) (listModel, tea.Cmd) {
	switch msg := msg.(type) {
	case resourcesFetchedMsg:
		if msg.fetchID != m.fetchID {
			return m, nil // stale response from a previous instance; discard
		}
		m.loading = false
		m.actionBusy = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.resources = msg.resources
		m.lastRefreshed = time.Now()
		m = m.withRebuiltTable()
		return m, nil

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
			m.loading = true
			m.fetchID = time.Now().UnixNano()
			return m, tea.Batch(fetchAllResources(m.client, m.fetchID), m.spinner.Tick)
		}
		return m, nil

	case cloneNextIDMsg:
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
			m.mode = listNormal
			return m, nil
		}
		r := m.selectedResource()
		if r == nil {
			m.mode = listNormal
			return m, nil
		}
		m.cloneIDInput.SetValue(fmt.Sprintf("%d", msg.id))
		m.cloneNameInput.SetValue("")
		m.cloneField = 0
		m.cloneIDInput.Focus()
		m.cloneNameInput.Blur()
		m.mode = listCloneInput
		return m, textinput.Blink

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.loading || m.actionBusy {
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		// Clone input mode.
		if m.mode == listCloneInput {
			switch msg.String() {
			case "esc":
				m.mode = listNormal
				m.cloneIDInput.Reset()
				m.cloneNameInput.Reset()
				m.cloneIDInput.Blur()
				m.cloneNameInput.Blur()
				m.statusMsg = ""
				m.statusErr = false
				m.loading = true
				m.fetchID = time.Now().UnixNano()
				return m, tea.Batch(fetchAllResources(m.client, m.fetchID), m.spinner.Tick)
			case "tab", "down":
				if m.cloneField == 0 {
					m.cloneField = 1
					m.cloneIDInput.Blur()
					m.cloneNameInput.Focus()
					return m, textinput.Blink
				}
				m.cloneField = 0
				m.cloneNameInput.Blur()
				m.cloneIDInput.Focus()
				return m, textinput.Blink
			case "shift+tab", "up":
				if m.cloneField == 1 {
					m.cloneField = 0
					m.cloneNameInput.Blur()
					m.cloneIDInput.Focus()
					return m, textinput.Blink
				}
				m.cloneField = 1
				m.cloneIDInput.Blur()
				m.cloneNameInput.Focus()
				return m, textinput.Blink
			case "enter":
				if m.cloneField == 0 {
					// Validate VMID and advance to name field.
					idStr := strings.TrimSpace(m.cloneIDInput.Value())
					if idStr == "" {
						return m, nil
					}
					vmid, err := strconv.Atoi(idStr)
					if err != nil || vmid < 100 {
						m.statusMsg = "Invalid VMID (must be >= 100)"
						m.statusErr = true
						return m, nil
					}
					_ = vmid
					m.cloneField = 1
					m.cloneIDInput.Blur()
					m.cloneNameInput.Focus()
					return m, textinput.Blink
				}
				// Field 1 (name): submit clone.
				idStr := strings.TrimSpace(m.cloneIDInput.Value())
				name := strings.TrimSpace(m.cloneNameInput.Value())
				m.cloneIDInput.Blur()
				m.cloneNameInput.Blur()
				vmid, _ := strconv.Atoi(idStr)
				if name == "" {
					r := m.selectedResource()
					if r != nil {
						name = r.Name + "-clone"
					}
				}
				m.mode = listNormal
				m.actionBusy = true
				m.statusMsg = "Cloning..."
				m.statusErr = false
				return m, tea.Batch(m.listCloneResourceCmd(vmid, name), m.spinner.Tick)
			default:
				var cmd tea.Cmd
				if m.cloneField == 0 {
					m.cloneIDInput, cmd = m.cloneIDInput.Update(msg)
				} else {
					m.cloneNameInput, cmd = m.cloneNameInput.Update(msg)
				}
				return m, cmd
			}
		}

		// Confirm delete mode.
		if m.mode == listConfirmDelete {
			switch msg.String() {
			case "enter":
				m.mode = listNormal
				r := m.selectedResource()
				if r == nil {
					return m, nil
				}
				typeStr := "VM"
				if r.Type == "lxc" {
					typeStr = "CT"
				}
				m.actionBusy = true
				m.statusMsg = fmt.Sprintf("Deleting %s %d...", typeStr, r.VMID)
				m.statusErr = false
				return m, tea.Batch(m.listDeleteResourceCmd(), m.spinner.Tick)
			case "esc":
				m.mode = listNormal
				return m, nil
			}
			return m, nil
		}

		// Normal mode: ignore keys during an in-flight action.
		if m.actionBusy {
			return m, nil
		}

		switch msg.String() {
		case "enter":
			if len(m.resources) == 0 {
				return m, nil
			}
			cursor := m.table.Cursor()
			if cursor >= 0 && cursor < len(m.resources) {
				r := *m.resources[cursor]
				return m, func() tea.Msg {
					return resourceSelectedMsg{resource: r}
				}
			}
		case "s":
			if m.selectedResource() == nil {
				return m, nil
			}
			m.actionBusy = true
			m.statusMsg = fmt.Sprintf("%d: Starting...", m.selectedResource().VMID)
			m.statusErr = false
			return m, tea.Batch(m.listPowerCmd("start"), m.spinner.Tick)
		case "S":
			if m.selectedResource() == nil {
				return m, nil
			}
			m.actionBusy = true
			m.statusMsg = fmt.Sprintf("%d: Stopping...", m.selectedResource().VMID)
			m.statusErr = false
			return m, tea.Batch(m.listPowerCmd("stop"), m.spinner.Tick)
		case "U":
			if m.selectedResource() == nil {
				return m, nil
			}
			m.actionBusy = true
			m.statusMsg = fmt.Sprintf("%d: Shutting down...", m.selectedResource().VMID)
			m.statusErr = false
			return m, tea.Batch(m.listPowerCmd("shutdown"), m.spinner.Tick)
		case "R":
			if m.selectedResource() == nil {
				return m, nil
			}
			m.actionBusy = true
			m.statusMsg = fmt.Sprintf("%d: Rebooting...", m.selectedResource().VMID)
			m.statusErr = false
			return m, tea.Batch(m.listPowerCmd("reboot"), m.spinner.Tick)
		case "c":
			if m.selectedResource() == nil {
				return m, nil
			}
			return m, m.loadCloneNextIDCmd()
		case "D":
			if m.selectedResource() == nil {
				return m, nil
			}
			m.mode = listConfirmDelete
			return m, nil
		case "ctrl+r":
			m.loading = true
			m.err = nil
			m.statusMsg = ""
			m.statusErr = false
			m.fetchID = time.Now().UnixNano()
			return m, tea.Batch(fetchAllResources(m.client, m.fetchID), m.spinner.Tick)
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m listModel) view() string {
	if m.width == 0 {
		return ""
	}

	title := StyleTitle.Render(fmt.Sprintf("Instances — %s", m.instName))

	if m.loading {
		return lipgloss.NewStyle().Padding(1, 2).Render(
			title + "\n\n" + StyleWarning.Render(m.spinner.View()+" Loading..."),
		)
	}

	if m.err != nil {
		lines := []string{
			title,
			"",
			StyleError.Render("Error: " + m.err.Error()),
			"",
			StyleHelp.Render("[ctrl+r] retry"),
			StyleHelp.Render("[Esc] back   [Q] quit"),
		}
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
	}

	count := StyleDim.Render(fmt.Sprintf(" (%d)", len(m.resources)))
	lines := []string{
		headerLine(title+count, m.width, m.lastRefreshed),
		"",
		m.table.View(),
	}

	// Status/spinner feedback line.
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

	// Action hints or confirmation/input overlay.
	switch m.mode {
	case listConfirmDelete:
		r := m.selectedResource()
		if r != nil {
			typeStr := "VM"
			if r.Type == "lxc" {
				typeStr = "CT"
			}
			lines = append(lines, StyleWarning.Render(
				fmt.Sprintf("Delete %s %d (%s)? This cannot be undone. [Enter] confirm   [Esc] cancel", typeStr, r.VMID, r.Name),
			))
		}
	case listCloneInput:
		r := m.selectedResource()
		if r != nil {
			typeStr := "VM"
			if r.Type == "lxc" {
				typeStr = "CT"
			}
			lines = append(lines, StyleWarning.Render(fmt.Sprintf("Clone %s %d (%s)", typeStr, r.VMID, r.Name)))
			idLabel := StyleDim.Render("  VMID: ")
			nameLabel := StyleDim.Render("  Name: ")
			if m.cloneField == 0 {
				idLabel = StyleWarning.Render("> VMID: ")
			} else {
				nameLabel = StyleWarning.Render("> Name: ")
			}
			lines = append(lines, idLabel+m.cloneIDInput.View())
			lines = append(lines, nameLabel+m.cloneNameInput.View())
			lines = append(lines, StyleHelp.Render("[Tab] switch field  [Enter] confirm  [Esc] cancel"))
		}
	default:
		lines = append(lines, StyleHelp.Render("[s] start  [S] stop  [U] shutdown  [R] reboot  [c] clone  [D] delete"))
	}

	lines = append(lines, StyleHelp.Render("[Tab] users / backups  |  [ctrl+r] refresh"))
	lines = append(lines, StyleHelp.Render("[Esc] back   [Q] quit"))
	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (m listModel) selectedResource() *proxmox.ClusterResource {
	if len(m.resources) == 0 {
		return nil
	}
	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(m.resources) {
		return nil
	}
	return m.resources[cursor]
}

func (m listModel) listPowerCmd(action string) tea.Cmd {
	c := m.client
	r := m.selectedResource()
	if r == nil {
		return nil
	}
	res := *r
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		vmid := int(res.VMID)

		if res.Type == "qemu" {
			switch action {
			case "start":
				task, err = actions.StartVM(ctx, c, vmid, res.Node)
			case "stop":
				task, err = actions.StopVM(ctx, c, vmid, res.Node)
			case "shutdown":
				task, err = actions.ShutdownVM(ctx, c, vmid, res.Node)
			case "reboot":
				task, err = actions.RebootVM(ctx, c, vmid, res.Node)
			}
		} else {
			switch action {
			case "start":
				task, err = actions.StartContainer(ctx, c, vmid, res.Node)
			case "stop":
				task, err = actions.StopContainer(ctx, c, vmid, res.Node)
			case "shutdown":
				task, err = actions.ShutdownContainer(ctx, c, vmid, res.Node)
			case "reboot":
				task, err = actions.RebootContainer(ctx, c, vmid, res.Node)
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
		return actionResultMsg{message: fmt.Sprintf("%d: %s completed", vmid, action), needRefresh: true}
	}
}

func (m listModel) listDeleteResourceCmd() tea.Cmd {
	c := m.client
	r := m.selectedResource()
	if r == nil {
		return nil
	}
	res := *r
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		vmid := int(res.VMID)

		if res.Type == "qemu" {
			task, err = actions.DeleteVM(ctx, c, vmid, res.Node)
		} else {
			task, err = actions.DeleteContainer(ctx, c, vmid, res.Node)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 300); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		typeStr := "VM"
		if res.Type == "lxc" {
			typeStr = "CT"
		}
		return actionResultMsg{message: fmt.Sprintf("%s %d deleted", typeStr, vmid), needRefresh: true}
	}
}

func (m listModel) loadCloneNextIDCmd() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		id, err := actions.NextID(ctx, c)
		return cloneNextIDMsg{id: id, err: err}
	}
}

func (m listModel) listCloneResourceCmd(newid int, name string) tea.Cmd {
	c := m.client
	r := m.selectedResource()
	if r == nil {
		return nil
	}
	res := *r
	return func() tea.Msg {
		ctx := context.Background()
		vmid := int(res.VMID)
		var assignedID int
		var task *proxmox.Task
		var err error
		if res.Type == "qemu" {
			assignedID, task, err = actions.CloneVM(ctx, c, vmid, newid, res.Node, name)
		} else {
			assignedID, task, err = actions.CloneContainer(ctx, c, vmid, newid, res.Node, name)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 600); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return actionResultMsg{message: fmt.Sprintf("Cloned to VMID %d", assignedID), needRefresh: true}
	}
}

// fetchAllResources fetches all VMs and containers in one API call and sorts by VMID.
// fetchID is echoed back in the message so stale responses can be discarded.
func fetchAllResources(c *proxmox.Client, fetchID int64) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		cl, err := c.Cluster(ctx)
		if err != nil {
			return resourcesFetchedMsg{err: err, fetchID: fetchID}
		}
		all, err := cl.Resources(ctx, "vm")
		if err != nil {
			return resourcesFetchedMsg{err: err, fetchID: fetchID}
		}
		sort.Slice(all, func(i, j int) bool { return all[i].VMID < all[j].VMID })
		return resourcesFetchedMsg{resources: all, fetchID: fetchID}
	}
}

// formatRefreshTime formats a time as an HH:MM:SS timestamp.
func formatRefreshTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Format("15:04:05")
}

// formatPercent formats a CPU fraction (0.0–1.0) as a percentage string.
func formatPercent(cpu float64) string {
	return fmt.Sprintf("%.1f%%", cpu*100)
}

// formatBytes converts bytes to a human-readable string (GiB/MiB/KiB/B).
func formatBytes(b uint64) string {
	const (
		gib = 1024 * 1024 * 1024
		mib = 1024 * 1024
		kib = 1024
	)
	switch {
	case b >= gib:
		return fmt.Sprintf("%.1f GiB", float64(b)/gib)
	case b >= mib:
		return fmt.Sprintf("%.1f MiB", float64(b)/mib)
	case b >= kib:
		return fmt.Sprintf("%.1f KiB", float64(b)/kib)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
