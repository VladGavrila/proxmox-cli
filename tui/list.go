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
	listNormal            listMode = iota
	listConfirmDelete              // waiting for Enter/Esc to confirm resource delete
	listCloneInput                 // text inputs for clone VMID + name
	listConfirmTemplate            // confirm convert-to-template
	listResizeDisk                 // text inputs for disk resize (disk ID + size delta)
	listSelectMoveDisk             // cursor picker: choose which disk to move
	listSelectMoveStorage          // cursor picker: choose target storage for move
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

	// Disk resize input state
	resizeDiskInput textinput.Model
	resizeSizeInput textinput.Model
	resizeDiskField int // 0 = disk ID, 1 = size delta

	// Disk move state
	availableDisks  map[string]string
	diskMoveKeys    []string
	diskMoveIdx     int
	pendingMoveDisk string
	moveStorages    []storageChoice
	moveStorageIdx  int

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

	rdiskInput := textinput.New()
	rdiskInput.Placeholder = "e.g. scsi0, rootfs"
	rdiskInput.CharLimit = 20
	rdiskInput.Width = 20

	rsizeInput := textinput.New()
	rsizeInput.Placeholder = "e.g. 10G"
	rsizeInput.CharLimit = 10
	rsizeInput.Width = 10

	return listModel{
		client:          c,
		instName:        instName,
		loading:         true,
		spinner:         s,
		fetchID:         time.Now().UnixNano(),
		cloneIDInput:    cidInput,
		cloneNameInput:  cnameInput,
		resizeDiskInput: rdiskInput,
		resizeSizeInput: rsizeInput,
		width:           w,
		height:          h,
	}
}

// fixedColWidth is the total width of all columns except NAME.
// VMID(6) + TYPE(4) + TMPL(5) + NODE(12) + STATUS(10) + CPU(7) + MEM(10) + DISK(10) + TAGS(15) = 79
// Plus cell padding: 10 columns × 2 chars (1 left + 1 right per cell) = 20.
const fixedColWidth = 79 + 20

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
		{Title: "TAGS", Width: 15},
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
			formatTagsCell(r.Tags),
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

	case diskListLoadedMsg:
		m.actionBusy = false
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
			return m, nil
		}
		if len(msg.disks) == 0 {
			m.statusMsg = "No moveable disks found"
			m.statusErr = true
			return m, nil
		}
		m.availableDisks = msg.disks
		keys := make([]string, 0, len(msg.disks))
		for k := range msg.disks {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		m.diskMoveKeys = keys
		if len(keys) == 1 {
			m.pendingMoveDisk = keys[0]
			m.actionBusy = true
			m.statusMsg = "Loading storages..."
			return m, tea.Batch(m.listLoadMoveStoragesCmd(), m.spinner.Tick)
		}
		m.diskMoveIdx = 0
		m.mode = listSelectMoveDisk
		return m, nil

	case moveStoragesLoadedMsg:
		m.actionBusy = false
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
			return m, nil
		}
		if len(msg.storages) == 0 {
			m.statusMsg = "No target storages found"
			m.statusErr = true
			return m, nil
		}
		m.moveStorages = msg.storages
		if len(msg.storages) == 1 {
			m.mode = listNormal
			m.actionBusy = true
			m.statusMsg = "Moving disk..."
			return m, tea.Batch(m.listMoveDiskCmd(m.pendingMoveDisk, msg.storages[0].Name), m.spinner.Tick)
		}
		m.moveStorageIdx = 0
		m.mode = listSelectMoveStorage
		return m, nil

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

		// Confirm template mode.
		if m.mode == listConfirmTemplate {
			switch msg.String() {
			case "enter":
				r := m.selectedResource()
				m.mode = listNormal
				if r == nil {
					return m, nil
				}
				typeStr := "VM"
				if r.Type == "lxc" {
					typeStr = "CT"
				}
				m.actionBusy = true
				m.statusMsg = fmt.Sprintf("Converting %s %d to template...", typeStr, r.VMID)
				m.statusErr = false
				return m, tea.Batch(m.listConvertToTemplateCmd(), m.spinner.Tick)
			case "esc":
				m.mode = listNormal
				return m, nil
			}
			return m, nil
		}

		// Disk resize input mode.
		if m.mode == listResizeDisk {
			switch msg.String() {
			case "esc":
				m.mode = listNormal
				m.resizeDiskInput.Reset()
				m.resizeSizeInput.Reset()
				m.resizeDiskInput.Blur()
				m.resizeSizeInput.Blur()
				m.statusMsg = ""
				m.statusErr = false
				return m, nil
			case "tab", "down":
				if m.resizeDiskField == 0 {
					m.resizeDiskField = 1
					m.resizeDiskInput.Blur()
					m.resizeSizeInput.Focus()
					return m, textinput.Blink
				}
				m.resizeDiskField = 0
				m.resizeSizeInput.Blur()
				m.resizeDiskInput.Focus()
				return m, textinput.Blink
			case "shift+tab", "up":
				if m.resizeDiskField == 1 {
					m.resizeDiskField = 0
					m.resizeSizeInput.Blur()
					m.resizeDiskInput.Focus()
					return m, textinput.Blink
				}
				m.resizeDiskField = 1
				m.resizeDiskInput.Blur()
				m.resizeSizeInput.Focus()
				return m, textinput.Blink
			case "enter":
				if m.resizeDiskField == 0 {
					if strings.TrimSpace(m.resizeDiskInput.Value()) == "" {
						return m, nil
					}
					m.resizeDiskField = 1
					m.resizeDiskInput.Blur()
					m.resizeSizeInput.Focus()
					return m, textinput.Blink
				}
				disk := strings.TrimSpace(m.resizeDiskInput.Value())
				size := strings.TrimSpace(m.resizeSizeInput.Value())
				m.resizeDiskInput.Blur()
				m.resizeSizeInput.Blur()
				if disk == "" || size == "" {
					m.mode = listNormal
					return m, nil
				}
				if !strings.HasPrefix(size, "+") {
					size = "+" + size
				}
				r := m.selectedResource()
				m.mode = listNormal
				if r == nil {
					return m, nil
				}
				typeStr := "VM"
				if r.Type == "lxc" {
					typeStr = "CT"
				}
				m.actionBusy = true
				m.statusMsg = fmt.Sprintf("Resizing %s %d disk %s by %s...", typeStr, r.VMID, disk, size)
				m.statusErr = false
				return m, tea.Batch(m.listResizeDiskCmd(disk, size), m.spinner.Tick)
			default:
				var cmd tea.Cmd
				if m.resizeDiskField == 0 {
					m.resizeDiskInput, cmd = m.resizeDiskInput.Update(msg)
				} else {
					m.resizeSizeInput, cmd = m.resizeSizeInput.Update(msg)
				}
				return m, cmd
			}
		}

		// Disk move: pick which disk.
		if m.mode == listSelectMoveDisk {
			switch msg.String() {
			case "up", "k":
				if m.diskMoveIdx > 0 {
					m.diskMoveIdx--
				}
			case "down", "j":
				if m.diskMoveIdx < len(m.diskMoveKeys)-1 {
					m.diskMoveIdx++
				}
			case "enter":
				m.pendingMoveDisk = m.diskMoveKeys[m.diskMoveIdx]
				m.mode = listNormal
				m.actionBusy = true
				m.statusMsg = "Loading storages..."
				return m, tea.Batch(m.listLoadMoveStoragesCmd(), m.spinner.Tick)
			case "esc":
				m.mode = listNormal
			}
			return m, nil
		}

		// Disk move: pick target storage.
		if m.mode == listSelectMoveStorage {
			switch msg.String() {
			case "up", "k":
				if m.moveStorageIdx > 0 {
					m.moveStorageIdx--
				}
			case "down", "j":
				if m.moveStorageIdx < len(m.moveStorages)-1 {
					m.moveStorageIdx++
				}
			case "enter":
				storageName := m.moveStorages[m.moveStorageIdx].Name
				m.mode = listNormal
				m.actionBusy = true
				m.statusMsg = fmt.Sprintf("Moving disk %s...", m.pendingMoveDisk)
				return m, tea.Batch(m.listMoveDiskCmd(m.pendingMoveDisk, storageName), m.spinner.Tick)
			case "esc":
				m.mode = listNormal
				m.pendingMoveDisk = ""
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
		case "T":
			r := m.selectedResource()
			if r == nil {
				return m, nil
			}
			if r.Template == 1 {
				m.statusMsg = "Already a template"
				m.statusErr = true
				return m, nil
			}
			m.mode = listConfirmTemplate
			return m, nil
		case "alt+z", "Ω":
			r := m.selectedResource()
			if r == nil {
				return m, nil
			}
			defaultDisk := "scsi0"
			if r.Type == "lxc" {
				defaultDisk = "rootfs"
			}
			m.resizeDiskInput.SetValue(defaultDisk)
			m.resizeSizeInput.Reset()
			m.resizeDiskField = 1
			m.resizeDiskInput.Blur()
			m.resizeSizeInput.Focus()
			m.mode = listResizeDisk
			return m, textinput.Blink
		case "alt+m", "µ":
			if m.selectedResource() == nil {
				return m, nil
			}
			m.actionBusy = true
			m.statusMsg = "Loading disk info..."
			m.statusErr = false
			return m, tea.Batch(m.listLoadDisksCmd(), m.spinner.Tick)
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
			renderHelp("[ctrl+r] retry"),
			renderHelp("[Esc] back   [Q] quit"),
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
			lines = append(lines, renderHelp("[Tab] switch field  [Enter] confirm  [Esc] cancel"))
		}
	case listConfirmTemplate:
		r := m.selectedResource()
		if r != nil {
			typeStr := "VM"
			if r.Type == "lxc" {
				typeStr = "CT"
			}
			lines = append(lines, StyleWarning.Render(
				fmt.Sprintf("Convert %s %d (%s) to a template? This cannot be undone. [Enter] confirm   [Esc] cancel",
					typeStr, r.VMID, r.Name),
			))
		}
	case listResizeDisk:
		r := m.selectedResource()
		if r != nil {
			typeStr := "VM"
			if r.Type == "lxc" {
				typeStr = "CT"
			}
			lines = append(lines, StyleWarning.Render(fmt.Sprintf("Resize disk on %s %d (%s)", typeStr, r.VMID, r.Name)))
			diskLabel := StyleDim.Render("  Disk: ")
			sizeLabel := StyleDim.Render("  Size: ")
			if m.resizeDiskField == 0 {
				diskLabel = StyleWarning.Render("> Disk: ")
			} else {
				sizeLabel = StyleWarning.Render("> Size: ")
			}
			lines = append(lines, diskLabel+m.resizeDiskInput.View())
			lines = append(lines, sizeLabel+m.resizeSizeInput.View())
			lines = append(lines, renderHelp("[Tab] switch field  [Enter] confirm  [Esc] cancel"))
		}
	case listSelectMoveDisk:
		r := m.selectedResource()
		diskLabel := "disk"
		if r != nil && r.Type == "lxc" {
			diskLabel = "volume"
		}
		lines = append(lines, StyleWarning.Render("Select "+diskLabel+" to move:"))
		for i, k := range m.diskMoveKeys {
			cursor := "  "
			if i == m.diskMoveIdx {
				cursor = "> "
			}
			lines = append(lines, StyleWarning.Render(fmt.Sprintf("%s%s  %s", cursor, k, m.availableDisks[k])))
		}
		lines = append(lines, renderHelp("[↑/↓] navigate   [Enter] select   [Esc] cancel"))
	case listSelectMoveStorage:
		lines = append(lines, StyleWarning.Render(fmt.Sprintf("Select target storage for %s:", m.pendingMoveDisk)))
		for i, s := range m.moveStorages {
			cursor := "  "
			if i == m.moveStorageIdx {
				cursor = "> "
			}
			lines = append(lines, StyleWarning.Render(fmt.Sprintf("%s%s (%s free, %s)", cursor, s.Name, s.Avail, s.Type)))
		}
		lines = append(lines, renderHelp("[↑/↓] navigate   [Enter] select   [Esc] cancel"))
	default:
		lines = append(lines, renderHelp("[s] start  [S] stop  [U] shutdown  [R] reboot  [c] clone  [D] delete  [T] template"))
		lines = append(lines, renderHelp("[Alt+z] resize disk  [Alt+m] move disk"))
	}

	lines = append(lines, renderHelp("[Tab] users / backups  |  [ctrl+r] refresh"))
	lines = append(lines, renderHelp("[Esc] back   [Q] quit"))
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

func (m listModel) listResizeDiskCmd(disk, size string) tea.Cmd {
	c := m.client
	r := m.selectedResource()
	if r == nil {
		return nil
	}
	vmid := int(r.VMID)
	rtype := r.Type
	node := r.Node
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		if rtype == "qemu" {
			task, err = actions.ResizeVMDisk(ctx, c, vmid, node, disk, size)
		} else {
			task, err = actions.ResizeContainerDisk(ctx, c, vmid, node, disk, size)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 300); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return actionResultMsg{message: fmt.Sprintf("Disk %s resized by %s", disk, size)}
	}
}

func (m listModel) listConvertToTemplateCmd() tea.Cmd {
	c := m.client
	r := m.selectedResource()
	if r == nil {
		return nil
	}
	vmid := int(r.VMID)
	rtype := r.Type
	node := r.Node
	return func() tea.Msg {
		ctx := context.Background()
		typeStr := "VM"
		if rtype == "lxc" {
			typeStr = "CT"
			err := actions.ConvertContainerToTemplate(ctx, c, vmid, node)
			if err != nil {
				return actionResultMsg{err: err}
			}
			return actionResultMsg{message: fmt.Sprintf("%s %d converted to template", typeStr, vmid), needRefresh: true}
		}
		task, err := actions.ConvertVMToTemplate(ctx, c, vmid, node)
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 120); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return actionResultMsg{message: fmt.Sprintf("%s %d converted to template", typeStr, vmid), needRefresh: true}
	}
}

func (m listModel) listLoadDisksCmd() tea.Cmd {
	c := m.client
	r := m.selectedResource()
	if r == nil {
		return nil
	}
	vmid := int(r.VMID)
	rtype := r.Type
	node := r.Node
	return func() tea.Msg {
		ctx := context.Background()
		disks := make(map[string]string)
		if rtype == "qemu" {
			vm, err := actions.FindVM(ctx, c, vmid, node)
			if err != nil {
				return diskListLoadedMsg{err: err}
			}
			for k, v := range vm.VirtualMachineConfig.MergeDisks() {
				if v != "" && !strings.Contains(v, "media=cdrom") {
					disks[k] = v
				}
			}
		} else {
			ct, err := actions.FindContainer(ctx, c, vmid, node)
			if err != nil {
				return diskListLoadedMsg{err: err}
			}
			if ct.ContainerConfig.RootFS != "" {
				disks["rootfs"] = ct.ContainerConfig.RootFS
			}
			for k, v := range ct.ContainerConfig.MergeMps() {
				disks[k] = v
			}
		}
		return diskListLoadedMsg{disks: disks}
	}
}

func (m listModel) listLoadMoveStoragesCmd() tea.Cmd {
	c := m.client
	r := m.selectedResource()
	if r == nil {
		return nil
	}
	node := r.Node
	rtype := r.Type
	currentStorage := ""
	if spec, ok := m.availableDisks[m.pendingMoveDisk]; ok {
		if idx := strings.Index(spec, ":"); idx >= 0 {
			currentStorage = spec[:idx]
		}
	}
	return func() tea.Msg {
		ctx := context.Background()
		storages, err := actions.ListRestoreStorages(ctx, c, node, rtype)
		if err != nil {
			return moveStoragesLoadedMsg{err: err}
		}
		var choices []storageChoice
		for _, s := range storages {
			if s.Name == currentStorage {
				continue
			}
			choices = append(choices, storageChoice{
				Name:  s.Name,
				Avail: formatBytes(s.Avail),
				Type:  s.Type,
			})
		}
		return moveStoragesLoadedMsg{storages: choices}
	}
}

func (m listModel) listMoveDiskCmd(disk, storage string) tea.Cmd {
	c := m.client
	r := m.selectedResource()
	if r == nil {
		return nil
	}
	vmid := int(r.VMID)
	rtype := r.Type
	node := r.Node
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		if rtype == "qemu" {
			task, err = actions.MoveVMDisk(ctx, c, vmid, node, disk, storage, true, 0)
		} else {
			task, err = actions.MoveContainerVolume(ctx, c, vmid, node, disk, storage, true, 0)
		}
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 600); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return actionResultMsg{message: fmt.Sprintf("Disk %s moved to %s", disk, storage)}
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

// parseTags splits a Proxmox semicolon-separated tags string into a slice.
func parseTags(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, t := range strings.Split(s, ";") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// formatTagsCell renders tags for the list table: up to 2 joined with commas,
// with "+N" appended when there are more.
func formatTagsCell(s string) string {
	tags := parseTags(s)
	if len(tags) == 0 {
		return ""
	}
	if len(tags) <= 2 {
		return strings.Join(tags, ",")
	}
	return strings.Join(tags[:2], ",") + fmt.Sprintf("+%d", len(tags)-2)
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
