package tui

import (
	"context"
	"fmt"
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

type backupsScreenMode int

const (
	backupsScreenNormal         backupsScreenMode = iota
	backupsScreenConfirmDel                       // confirm backup deletion
	backupsScreenRestoreInput                     // VMID + name input (restoreField tracks active)
	backupsScreenRestoreStorage                   // storage picker for restore target
)

// backupsFetchedMsg is sent when the async fetch of all cluster backups completes.
type backupsFetchedMsg struct {
	backups []backupEntry
	err     error
	fetchID int64
}

// backupsScreenActionMsg is sent after a backup action (e.g. delete/restore) completes.
type backupsScreenActionMsg struct {
	message string
	err     error
	reload  bool
}

// backupsNextIDMsg is sent when the next available VMID is fetched.
type backupsNextIDMsg struct {
	id  int
	err error
}

// backupsStoragesMsg is sent when restore-target storages are loaded.
type backupsStoragesMsg struct {
	storages []storageChoice
	err      error
}

type backupsScreenModel struct {
	client   *proxmox.Client
	instName string
	backups  []backupEntry
	loading  bool
	err      error
	table    table.Model
	spinner  spinner.Model
	mode     backupsScreenMode
	fetchID  int64

	// Restore state
	restoreIDInput     textinput.Model
	restoreNameInput   textinput.Model
	restoreField       int // 0 = VMID, 1 = name
	pendingVolid       string
	pendingNode        string
	pendingType        string // "qemu" or "lxc"
	pendingRestoreID   int
	pendingRestoreName string
	availableStorages  []storageChoice
	storageIdx         int
	actionBusy         bool

	statusMsg     string
	statusErr     bool
	lastRefreshed time.Time

	// Filter state
	filter          tableFilter
	filteredIndices []int // maps table row index → m.backups index

	width  int
	height int
}

func newBackupsScreenModel(c *proxmox.Client, instName string, w, h int) backupsScreenModel {
	s := spinner.New()
	s.Spinner = CLISpinner
	s.Style = StyleSpinner

	ridInput := textinput.New()
	ridInput.Placeholder = "VMID"
	ridInput.CharLimit = 10
	ridInput.Width = 12

	rnameInput := textinput.New()
	rnameInput.Placeholder = "name (empty = from backup)"
	rnameInput.CharLimit = 63

	return backupsScreenModel{
		client:           c,
		instName:         instName,
		loading:          true,
		spinner:          s,
		fetchID:          time.Now().UnixNano(),
		restoreIDInput:   ridInput,
		restoreNameInput: rnameInput,
		width:            w,
		height:           h,
	}
}

// fixedBackupsColWidth: VMID(6)+TYPE(4)+SIZE(10)+DATE(18)+NOTES(20) = 58 + separators ~12 = 70
const fixedBackupsColWidth = 58 + 12

func (m backupsScreenModel) volidColWidth() int {
	w := m.width - fixedBackupsColWidth - 4
	if w < 20 {
		w = 20
	}
	return w
}

func (m backupsScreenModel) withRebuiltTable() backupsScreenModel {
	volidWidth := m.volidColWidth()
	cols := []table.Column{
		{Title: "VOLID", Width: volidWidth},
		{Title: "VMID", Width: 6},
		{Title: "TYPE", Width: 4},
		{Title: "SIZE", Width: 10},
		{Title: "DATE", Width: 18},
		{Title: "NOTES", Width: 20},
	}

	var rows []table.Row
	m.filteredIndices = nil
	for i, b := range m.backups {
		typeStr := "VM"
		if b.Type == "lxc" {
			typeStr = "CT"
		}
		if !m.filter.matches(b.Volid, b.VMID, typeStr, b.Notes) {
			continue
		}
		rows = append(rows, table.Row{b.Volid, b.VMID, typeStr, b.Size, b.Date, b.Notes})
		m.filteredIndices = append(m.filteredIndices, i)
	}

	// Reserve space for: padding(2) + header(1) + blank(1) + status(1)
	// + overlay worst-case: blank(1) + title(1) + VMID(1) + Name(1) + help(1)
	// + [Esc] back(1) = 11, plus table header border(2) = 13 + filter(1) = 14
	tableHeight := m.height - 14
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

func (m backupsScreenModel) init() tea.Cmd {
	return tea.Batch(fetchAllClusterBackups(m.client, m.fetchID), m.spinner.Tick)
}

func (m backupsScreenModel) update(msg tea.Msg) (backupsScreenModel, tea.Cmd) {
	switch msg := msg.(type) {
	case backupsFetchedMsg:
		if msg.fetchID != m.fetchID {
			return m, nil // stale response; discard
		}
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.backups = msg.backups
		m.lastRefreshed = time.Now()
		m = m.withRebuiltTable()
		return m, nil

	case backupsScreenActionMsg:
		m.actionBusy = false
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
		} else {
			m.statusMsg = msg.message
			m.statusErr = false
		}
		if msg.reload {
			m.loading = true
			m.fetchID = time.Now().UnixNano()
			return m, tea.Batch(fetchAllClusterBackups(m.client, m.fetchID), m.spinner.Tick)
		}
		return m, nil

	case backupsNextIDMsg:
		m.actionBusy = false
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
			m.mode = backupsScreenNormal
			return m, nil
		}
		m.restoreIDInput.SetValue(fmt.Sprintf("%d", msg.id))
		m.restoreNameInput.SetValue("")
		m.restoreField = 0
		m.restoreIDInput.Focus()
		m.restoreNameInput.Blur()
		m.mode = backupsScreenRestoreInput
		return m, textinput.Blink

	case backupsStoragesMsg:
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
			m.actionBusy = false
			m.mode = backupsScreenNormal
			return m, nil
		}
		m.availableStorages = msg.storages
		if len(msg.storages) == 0 {
			m.statusMsg = "No restore storages found"
			m.statusErr = true
			m.actionBusy = false
			m.mode = backupsScreenNormal
			return m, nil
		}
		// If only 1 storage, use it directly.
		if len(msg.storages) == 1 {
			m.mode = backupsScreenNormal
			m.actionBusy = true
			m.statusMsg = "Restoring backup..."
			m.statusErr = false
			return m, tea.Batch(m.restoreBackupCmd(m.pendingVolid, m.pendingNode, m.pendingRestoreID, m.pendingRestoreName, msg.storages[0].Name), m.spinner.Tick)
		}
		m.storageIdx = 0
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.loading || m.actionBusy {
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		// Confirm-delete mode.
		if m.mode == backupsScreenConfirmDel {
			switch msg.String() {
			case "enter":
				m.mode = backupsScreenNormal
				b := m.selectedBackup()
				if b == nil {
					return m, nil
				}
				m.actionBusy = true
				m.statusMsg = "Deleting backup..."
				m.statusErr = false
				return m, tea.Batch(m.deleteBackupCmd(b.Volid, b.Storage, b.Node), m.spinner.Tick)
			case "esc":
				m.mode = backupsScreenNormal
				return m, nil
			}
			return m, nil
		}

		// Restore input mode: VMID (field 0) then name (field 1).
		if m.mode == backupsScreenRestoreInput {
			switch msg.String() {
			case "esc":
				m.mode = backupsScreenNormal
				m.restoreIDInput.Reset()
				m.restoreIDInput.Blur()
				m.restoreNameInput.Reset()
				m.restoreNameInput.Blur()
				m.restoreField = 0
				m.pendingVolid = ""
				m.loading = true
				m.fetchID = time.Now().UnixNano()
				return m, tea.Batch(fetchAllClusterBackups(m.client, m.fetchID), m.spinner.Tick)
			case "enter":
				if m.restoreField == 0 {
					// Validate VMID and advance to name field.
					idStr := strings.TrimSpace(m.restoreIDInput.Value())
					if idStr == "" {
						return m, nil
					}
					vmid, err := strconv.Atoi(idStr)
					if err != nil || vmid < 100 {
						m.statusMsg = "Invalid VMID (must be >= 100)"
						m.statusErr = true
						return m, nil
					}
					m.pendingRestoreID = vmid
					m.restoreField = 1
					m.restoreIDInput.Blur()
					m.restoreNameInput.Focus()
					return m, textinput.Blink
				}
				// Field 1: submit name and proceed to storage selection.
				m.pendingRestoreName = strings.TrimSpace(m.restoreNameInput.Value())
				m.restoreIDInput.Blur()
				m.restoreNameInput.Blur()
				m.mode = backupsScreenRestoreStorage
				m.actionBusy = true
				m.statusMsg = "Loading storages..."
				m.statusErr = false
				return m, tea.Batch(m.loadRestoreStoragesCmd(), m.spinner.Tick)
			default:
				var cmd tea.Cmd
				if m.restoreField == 0 {
					m.restoreIDInput, cmd = m.restoreIDInput.Update(msg)
				} else {
					m.restoreNameInput, cmd = m.restoreNameInput.Update(msg)
				}
				return m, cmd
			}
		}

		// Storage selection for restore target.
		if m.mode == backupsScreenRestoreStorage {
			switch msg.String() {
			case "up", "k":
				if m.storageIdx > 0 {
					m.storageIdx--
				}
				return m, nil
			case "down", "j":
				if m.storageIdx < len(m.availableStorages)-1 {
					m.storageIdx++
				}
				return m, nil
			case "enter":
				storageName := m.availableStorages[m.storageIdx].Name
				m.mode = backupsScreenNormal
				m.actionBusy = true
				m.statusMsg = "Restoring backup..."
				m.statusErr = false
				return m, tea.Batch(m.restoreBackupCmd(m.pendingVolid, m.pendingNode, m.pendingRestoreID, m.pendingRestoreName, storageName), m.spinner.Tick)
			case "esc":
				m.mode = backupsScreenNormal
				m.actionBusy = false
				m.pendingVolid = ""
				m.restoreIDInput.Reset()
				m.restoreIDInput.Blur()
				m.restoreNameInput.Reset()
				m.restoreNameInput.Blur()
				m.restoreField = 0
				m.loading = true
				m.fetchID = time.Now().UnixNano()
				return m, tea.Batch(fetchAllClusterBackups(m.client, m.fetchID), m.spinner.Tick)
			}
			return m, nil
		}

		// Filter input mode.
		if m.filter.active {
			var rebuild bool
			m.filter, rebuild = m.filter.handleKey(msg)
			if rebuild {
				m = m.withRebuiltTable()
			}
			return m, nil
		}

		// Normal mode: ignore keys during an in-flight action.
		if m.actionBusy {
			return m, nil
		}

		switch msg.String() {
		case "/":
			m.filter.active = true
			return m, nil
		case "ctrl+u":
			if m.filter.hasActiveFilter() {
				m.filter.text = ""
				m = m.withRebuiltTable()
			}
			return m, nil
		case "alt+d", "∂":
			if len(m.backups) == 0 {
				return m, nil
			}
			m.mode = backupsScreenConfirmDel
			return m, nil
		case "alt+r", "®":
			b := m.selectedBackup()
			if b == nil {
				return m, nil
			}
			m.pendingVolid = b.Volid
			m.pendingNode = b.Node
			m.pendingType = b.Type
			return m, m.loadNextIDCmd()
		case "ctrl+r":
			m.loading = true
			m.err = nil
			m.statusMsg = ""
			m.statusErr = false
			m.fetchID = time.Now().UnixNano()
			return m, tea.Batch(fetchAllClusterBackups(m.client, m.fetchID), m.spinner.Tick)
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m backupsScreenModel) view() string {
	if m.width == 0 {
		return ""
	}

	title := StyleTitle.Render(fmt.Sprintf("Backups — %s", m.instName))

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

	var count string
	if m.filter.hasActiveFilter() {
		count = StyleDim.Render(fmt.Sprintf(" (%d/%d)", len(m.filteredIndices), len(m.backups)))
	} else {
		count = StyleDim.Render(fmt.Sprintf(" (%d)", len(m.backups)))
	}

	var lines []string
	lines = append(lines, headerLine(title+count, m.width, m.lastRefreshed))
	lines = append(lines, "")
	lines = append(lines, m.table.View())

	// Filter line.
	if fl := m.filter.renderLine(); fl != "" {
		lines = append(lines, fl)
	} else {
		lines = append(lines, "")
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

	// Overlay modes.
	lines = append(lines, m.viewOverlay()...)

	lines = append(lines, renderHelp("[Esc] back   [Q] quit"))

	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (m backupsScreenModel) viewOverlay() []string {
	var lines []string

	switch m.mode {
	case backupsScreenConfirmDel:
		volid := ""
		if b := m.selectedBackup(); b != nil {
			volid = b.Volid
		}
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(
			fmt.Sprintf("Delete backup %q? [Enter] confirm   [Esc] cancel", volid),
		))

	case backupsScreenRestoreInput:
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render("Restore backup"))
		idLabel := StyleDim.Render("  VMID: ")
		nameLabel := StyleDim.Render("  Name: ")
		if m.restoreField == 0 {
			idLabel = StyleWarning.Render("> VMID: ")
		} else {
			nameLabel = StyleWarning.Render("> Name: ")
		}
		lines = append(lines, idLabel+m.restoreIDInput.View())
		lines = append(lines, nameLabel+m.restoreNameInput.View())
		lines = append(lines, renderHelp("[Enter] next/confirm  [Esc] cancel"))

	case backupsScreenRestoreStorage:
		if len(m.availableStorages) > 0 {
			lines = append(lines, "")
			lines = append(lines, StyleWarning.Render("Select target storage for restored disks:"))
			for i, s := range m.availableStorages {
				cursor := "  "
				if i == m.storageIdx {
					cursor = "> "
				}
				lines = append(lines, StyleWarning.Render(
					fmt.Sprintf("%s%s (%s free, %s)", cursor, s.Name, s.Avail, s.Type),
				))
			}
			lines = append(lines, renderHelp("[Enter] select  [Esc] cancel"))
		}

	default:
		lines = append(lines, "")
		if len(m.backups) > 0 {
			lines = append(lines, renderHelp("[Alt+d] delete  [Alt+r] restore  [/] filter  |  [Tab] Resources  |  [ctrl+r] refresh"))
		} else {
			lines = append(lines, renderHelp("[Tab] Resources  |  [ctrl+r] refresh"))
		}
	}

	return lines
}

// clearFilter resets the filter and rebuilds the table to show all rows.
func (m *backupsScreenModel) clearFilter() {
	if m.filter.hasActiveFilter() || m.filter.active {
		m.filter.clear()
		*m = m.withRebuiltTable()
	}
}

func (m backupsScreenModel) selectedBackup() *backupEntry {
	if len(m.filteredIndices) == 0 {
		return nil
	}
	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(m.filteredIndices) {
		return nil
	}
	return &m.backups[m.filteredIndices[cursor]]
}

func fetchAllClusterBackups(c *proxmox.Client, fetchID int64) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		list, err := actions.ListBackups(ctx, c, "", "", 0)
		if err != nil {
			return backupsFetchedMsg{err: err, fetchID: fetchID}
		}
		var entries []backupEntry
		for _, b := range list {
			entries = append(entries, backupEntry{
				Volid:   b.Volid,
				Type:    b.Type,
				Size:    formatBytes(b.Size),
				Date:    formatSnapTime(b.Ctime),
				Notes:   b.Notes,
				Storage: b.Storage,
				Node:    b.Node,
				VMID:    fmt.Sprintf("%d", b.VMID),
			})
		}
		return backupsFetchedMsg{backups: entries, fetchID: fetchID}
	}
}

func (m backupsScreenModel) deleteBackupCmd(volid, storage, node string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		task, err := actions.DeleteBackup(ctx, c, node, storage, volid)
		if err != nil {
			return backupsScreenActionMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 300); werr != nil {
				return backupsScreenActionMsg{err: werr}
			}
		}
		return backupsScreenActionMsg{message: "Backup deleted", reload: true}
	}
}

func (m backupsScreenModel) loadNextIDCmd() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		id, err := actions.NextID(ctx, c)
		return backupsNextIDMsg{id: id, err: err}
	}
}

func (m backupsScreenModel) loadRestoreStoragesCmd() tea.Cmd {
	c := m.client
	node := m.pendingNode
	resType := m.pendingType
	return func() tea.Msg {
		ctx := context.Background()
		storages, err := actions.ListRestoreStorages(ctx, c, node, resType)
		if err != nil {
			return backupsStoragesMsg{err: err}
		}
		var choices []storageChoice
		for _, s := range storages {
			choices = append(choices, storageChoice{
				Name:  s.Name,
				Avail: formatBytes(s.Avail),
				Type:  s.Type,
			})
		}
		return backupsStoragesMsg{storages: choices}
	}
}

func (m backupsScreenModel) restoreBackupCmd(volid, node string, vmid int, name, restoreStorage string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		assignedID, task, err := actions.RestoreBackup(ctx, c, node, volid, vmid, name, restoreStorage)
		if err != nil {
			return backupsScreenActionMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 600); werr != nil {
				return backupsScreenActionMsg{err: werr}
			}
		}
		return backupsScreenActionMsg{message: fmt.Sprintf("Restored to VMID %d", assignedID), reload: true}
	}
}
