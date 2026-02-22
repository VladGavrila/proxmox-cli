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

type detailMode int

const (
	detailNormal              detailMode = iota
	detailConfirmDelete                  // waiting for Enter/Esc to confirm snapshot delete
	detailConfirmRollback                // waiting for Enter/Esc to confirm snapshot rollback
	detailInputName                      // text input active for new snapshot name
	detailConfirmDeleteBackup            // confirm backup deletion
	detailSelectBackupStorage            // storage picker for creating a backup
	detailRestoreInputID                 // text inputs for restore VMID + name
	detailRestoreSelectStorage           // storage picker for restore target
	detailConfirmDeleteResource          // confirm VM/CT deletion
	detailCloneInput                     // text inputs for clone VMID + name
)

// snapshotEntry is a unified representation for both VM and CT snapshots.
type snapshotEntry struct {
	Name        string
	Parent      string
	Description string
	Date        string
}

// backupEntry is a unified representation for backup items in the TUI.
type backupEntry struct {
	Volid   string
	Type    string
	Size    string
	Date    string
	Notes   string
	Storage string
	Node    string
	VMID    string
}

// storageChoice represents a backup-capable storage for selection overlays.
type storageChoice struct {
	Name  string
	Avail string
	Type  string
}

// detailLoadedMsg is sent when snapshot loading completes.
type detailLoadedMsg struct {
	snapshots []snapshotEntry
	err       error
}

// backupsLoadedMsg is sent when backup loading completes.
type backupsLoadedMsg struct {
	backups []backupEntry
	err     error
}

// backupStoragesLoadedMsg is sent when backup storage discovery completes.
type backupStoragesLoadedMsg struct {
	storages []storageChoice
	err      error
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

// reloadBackupsMsg is sent by backup create/delete commands to show feedback
// and trigger an automatic backup list reload.
type reloadBackupsMsg struct{ message string }

// nextIDLoadedMsg is sent when the next available VMID is fetched.
type nextIDLoadedMsg struct {
	id  int
	err error
}

// cloneNextIDMsg is sent when the next available VMID is fetched for cloning.
type cloneNextIDMsg struct {
	id  int
	err error
}

// resourceRefreshedMsg is sent after a resource status re-fetch completes.
type resourceRefreshedMsg struct {
	resource proxmox.ClusterResource
	err      error
}

// resourceDeletedMsg is sent after a successful VM/CT deletion.
// The router intercepts this to navigate back to the list screen.
type resourceDeletedMsg struct {
	message string
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

	// Tab state: 0 = Snapshots, 1 = Backups
	activeTab int

	// Backup state
	backups           []backupEntry
	backupTable       table.Model
	backupLoading     bool
	backupLoadErr     error
	availableStorages []storageChoice
	storageIdx        int
	pendingVolid      string // volid selected for restore (stored during storage selection)

	// Restore input state
	restoreIDInput    textinput.Model
	restoreNameInput  textinput.Model
	restoreField      int // 0 = VMID, 1 = name
	pendingRestoreID  int
	pendingRestoreName string

	// Clone input state
	cloneIDInput   textinput.Model
	cloneNameInput textinput.Model
	cloneField     int // 0 = VMID, 1 = name

	width  int
	height int
}

func newDetailModel(c *proxmox.Client, r proxmox.ClusterResource, w, h int) detailModel {
	ti := textinput.New()
	ti.Placeholder = "snapshot name"
	ti.CharLimit = 40

	ridInput := textinput.New()
	ridInput.Placeholder = "VMID"
	ridInput.CharLimit = 10
	ridInput.Width = 12

	rnameInput := textinput.New()
	rnameInput.Placeholder = "name (empty = from backup)"
	rnameInput.CharLimit = 63

	cidInput := textinput.New()
	cidInput.Placeholder = "new VMID"
	cidInput.CharLimit = 10
	cidInput.Width = 12

	cnameInput := textinput.New()
	cnameInput.Placeholder = "clone name"
	cnameInput.CharLimit = 63

	s := spinner.New()
	s.Spinner = CLISpinner
	s.Style = StyleSpinner

	return detailModel{
		client:           c,
		resource:         r,
		loading:          true,
		backupLoading:    true,
		activeTab:        0,
		input:            ti,
		restoreIDInput:   ridInput,
		restoreNameInput: rnameInput,
		cloneIDInput:     cidInput,
		cloneNameInput:   cnameInput,
		spinner:          s,
		width:            w,
		height:           h,
	}
}

func (m detailModel) withRebuiltTable() detailModel {
	m = m.withRebuiltSnapTable()
	m = m.withRebuiltBackupTable()
	return m
}

func (m detailModel) withRebuiltSnapTable() detailModel {
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
	// padding(1) + title + stats + power + sep + statusMsg + tab bar + "Snapshots(N)" = 9
	// help lines at bottom: snap actions + nav = 2
	// table header is internal to bubbles/table (counts as 1 extra line)
	// +1 for tab bar line
	// Total non-table lines: ~14
	tableHeight := m.height - 18
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

func (m detailModel) withRebuiltBackupTable() detailModel {
	if len(m.backups) == 0 {
		return m
	}

	notesWidth := m.width - 18 - 10 - 12 // remainder after DATE+SIZE+padding
	if notesWidth < 10 {
		notesWidth = 10
	}

	cols := []table.Column{
		{Title: "DATE", Width: 18},
		{Title: "SIZE", Width: 10},
		{Title: "NOTES", Width: notesWidth},
	}

	rows := make([]table.Row, len(m.backups))
	for i, b := range m.backups {
		rows[i] = table.Row{b.Date, b.Size, b.Notes}
	}

	tableHeight := m.height - 18
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
	m.backupTable = t
	return m
}

func (m detailModel) init() tea.Cmd {
	return tea.Batch(m.loadSnapshotsCmd(), m.loadBackupsCmd(), m.spinner.Tick)
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

func (m detailModel) loadBackupsCmd() tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		list, err := actions.ListBackups(ctx, c, r.Node, "", int(r.VMID))
		if err != nil {
			return backupsLoadedMsg{err: err}
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
		return backupsLoadedMsg{backups: entries}
	}
}

func (m detailModel) loadBackupStoragesCmd() tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		storages, err := actions.ListBackupStorages(ctx, c, r.Node)
		if err != nil {
			return backupStoragesLoadedMsg{err: err}
		}
		var choices []storageChoice
		for _, s := range storages {
			choices = append(choices, storageChoice{
				Name:  s.Name,
				Avail: formatBytes(s.Avail),
				Type:  s.Type,
			})
		}
		return backupStoragesLoadedMsg{storages: choices}
	}
}

func (m detailModel) loadRestoreStoragesCmd() tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		storages, err := actions.ListRestoreStorages(ctx, c, r.Node, r.Type)
		if err != nil {
			return backupStoragesLoadedMsg{err: err}
		}
		var choices []storageChoice
		for _, s := range storages {
			choices = append(choices, storageChoice{
				Name:  s.Name,
				Avail: formatBytes(s.Avail),
				Type:  s.Type,
			})
		}
		return backupStoragesLoadedMsg{storages: choices}
	}
}

func (m detailModel) loadNextIDCmd() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		id, err := actions.NextID(ctx, c)
		return nextIDLoadedMsg{id: id, err: err}
	}
}

func (m detailModel) loadCloneNextIDCmd() tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		id, err := actions.NextID(ctx, c)
		return cloneNextIDMsg{id: id, err: err}
	}
}

func (m detailModel) cloneResourceCmd(newid int, name string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		vmid := int(r.VMID)
		var assignedID int
		var task *proxmox.Task
		var err error
		if r.Type == "qemu" {
			assignedID, task, err = actions.CloneVM(ctx, c, vmid, newid, r.Node, name)
		} else {
			assignedID, task, err = actions.CloneContainer(ctx, c, vmid, newid, r.Node, name)
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

func (m detailModel) createBackupCmd(storageName string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		task, err := actions.CreateBackup(ctx, c, int(r.VMID), r.Node, storageName, "snapshot", "zstd")
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 600); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return reloadBackupsMsg{message: "Backup created"}
	}
}

func (m detailModel) deleteBackupCmd(volid, storage string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		task, err := actions.DeleteBackup(ctx, c, r.Node, storage, volid)
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 300); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return reloadBackupsMsg{message: "Backup deleted"}
	}
}

func (m detailModel) restoreBackupCmd(volid string, vmid int, name, restoreStorage string) tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		assignedID, task, err := actions.RestoreBackup(ctx, c, r.Node, volid, vmid, name, restoreStorage)
		if err != nil {
			return actionResultMsg{err: err}
		}
		if task != nil {
			if werr := task.WaitFor(ctx, 600); werr != nil {
				return actionResultMsg{err: werr}
			}
		}
		return actionResultMsg{message: fmt.Sprintf("Restored to VMID %d", assignedID)}
	}
}

func (m detailModel) deleteResourceCmd() tea.Cmd {
	c := m.client
	r := m.resource
	return func() tea.Msg {
		ctx := context.Background()
		var task *proxmox.Task
		var err error
		vmid := int(r.VMID)

		if r.Type == "qemu" {
			task, err = actions.DeleteVM(ctx, c, vmid, r.Node)
		} else {
			task, err = actions.DeleteContainer(ctx, c, vmid, r.Node)
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
		if r.Type == "lxc" {
			typeStr = "CT"
		}
		return resourceDeletedMsg{message: fmt.Sprintf("%s %d deleted", typeStr, vmid)}
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
			m = m.withRebuiltSnapTable()
		}
		return m, nil

	case backupsLoadedMsg:
		m.backupLoading = false
		if msg.err != nil {
			m.backupLoadErr = msg.err
		} else {
			m.backupLoadErr = nil
			m.backups = msg.backups
			m = m.withRebuiltBackupTable()
		}
		return m, nil

	case backupStoragesLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
			m.actionBusy = false
			m.mode = detailNormal
			return m, nil
		}
		m.availableStorages = msg.storages
		if len(msg.storages) == 0 {
			m.statusMsg = "No backup storages found"
			m.statusErr = true
			m.actionBusy = false
			m.mode = detailNormal
			return m, nil
		}
		if m.mode == detailSelectBackupStorage {
			// Creating a backup — if only 1 storage, use it directly
			if len(msg.storages) == 1 {
				m.mode = detailNormal
				m.actionBusy = true
				m.statusMsg = "Creating backup..."
				m.statusErr = false
				return m, tea.Batch(m.createBackupCmd(msg.storages[0].Name), m.spinner.Tick)
			}
			m.storageIdx = 0
			return m, nil
		}
		if m.mode == detailRestoreSelectStorage {
			// Restoring — if only 1 storage, use it directly
			if len(msg.storages) == 1 {
				m.mode = detailNormal
				m.actionBusy = true
				m.statusMsg = "Restoring backup..."
				m.statusErr = false
				return m, tea.Batch(m.restoreBackupCmd(m.pendingVolid, m.pendingRestoreID, m.pendingRestoreName, msg.storages[0].Name), m.spinner.Tick)
			}
			m.storageIdx = 0
			return m, nil
		}
		return m, nil

	case nextIDLoadedMsg:
		m.actionBusy = false
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
			m.mode = detailNormal
			return m, nil
		}
		// Pre-populate VMID input and show restore input form
		m.restoreIDInput.SetValue(fmt.Sprintf("%d", msg.id))
		m.restoreNameInput.SetValue("")
		m.restoreField = 0
		m.restoreIDInput.Focus()
		m.restoreNameInput.Blur()
		m.mode = detailRestoreInputID
		return m, textinput.Blink

	case cloneNextIDMsg:
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
			m.mode = detailNormal
			return m, nil
		}
		m.cloneIDInput.SetValue(fmt.Sprintf("%d", msg.id))
		m.cloneNameInput.SetValue("")
		m.cloneField = 0
		m.cloneIDInput.Focus()
		m.cloneNameInput.Blur()
		m.mode = detailCloneInput
		return m, textinput.Blink

	case reloadSnapshotsMsg:
		m.statusMsg = msg.message
		m.statusErr = false
		m.actionBusy = false
		m.loading = true
		return m, tea.Batch(m.loadSnapshotsCmd(), m.spinner.Tick)

	case reloadBackupsMsg:
		m.statusMsg = msg.message
		m.statusErr = false
		m.actionBusy = false
		m.backupLoading = true
		return m, tea.Batch(m.loadBackupsCmd(), m.spinner.Tick)

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
		if m.loading || m.backupLoading || m.actionBusy {
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

		// Confirm snapshot delete mode.
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

		// Confirm backup delete mode.
		if m.mode == detailConfirmDeleteBackup {
			switch msg.String() {
			case "enter":
				volid, storage := m.selectedBackupInfo()
				m.mode = detailNormal
				if volid == "" {
					return m, nil
				}
				m.actionBusy = true
				m.statusMsg = "Deleting backup..."
				m.statusErr = false
				return m, tea.Batch(m.deleteBackupCmd(volid, storage), m.spinner.Tick)
			case "esc":
				m.mode = detailNormal
				return m, nil
			}
			return m, nil
		}

		// Confirm VM/CT delete mode.
		if m.mode == detailConfirmDeleteResource {
			switch msg.String() {
			case "enter":
				m.mode = detailNormal
				m.actionBusy = true
				typeStr := "VM"
				if m.resource.Type == "lxc" {
					typeStr = "CT"
				}
				m.statusMsg = fmt.Sprintf("Deleting %s %d...", typeStr, m.resource.VMID)
				m.statusErr = false
				return m, tea.Batch(m.deleteResourceCmd(), m.spinner.Tick)
			case "esc":
				m.mode = detailNormal
				return m, nil
			}
			return m, nil
		}

		// Clone input mode: VMID and name fields.
		if m.mode == detailCloneInput {
			switch msg.String() {
			case "esc":
				m.mode = detailNormal
				m.cloneIDInput.Reset()
				m.cloneNameInput.Reset()
				m.cloneIDInput.Blur()
				m.cloneNameInput.Blur()
				m.statusMsg = ""
				m.statusErr = false
				m.loading = true
				m.loadErr = nil
				m.backupLoading = true
				m.backupLoadErr = nil
				return m, tea.Batch(m.loadSnapshotsCmd(), m.loadBackupsCmd(), m.refreshResourceCmd(), m.spinner.Tick)
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
					name = m.resource.Name + "-clone"
				}
				m.mode = detailNormal
				m.actionBusy = true
				m.statusMsg = "Cloning..."
				m.statusErr = false
				return m, tea.Batch(m.cloneResourceCmd(vmid, name), m.spinner.Tick)
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

		// Restore input mode: VMID and name fields.
		if m.mode == detailRestoreInputID {
			switch msg.String() {
			case "esc":
				m.mode = detailNormal
				m.restoreIDInput.Blur()
				m.restoreNameInput.Blur()
				return m, nil
			case "tab", "down":
				if m.restoreField == 0 {
					m.restoreField = 1
					m.restoreIDInput.Blur()
					m.restoreNameInput.Focus()
					return m, textinput.Blink
				}
				// On name field, wrap to VMID
				m.restoreField = 0
				m.restoreNameInput.Blur()
				m.restoreIDInput.Focus()
				return m, textinput.Blink
			case "shift+tab", "up":
				if m.restoreField == 1 {
					m.restoreField = 0
					m.restoreNameInput.Blur()
					m.restoreIDInput.Focus()
					return m, textinput.Blink
				}
				m.restoreField = 1
				m.restoreIDInput.Blur()
				m.restoreNameInput.Focus()
				return m, textinput.Blink
			case "enter":
				idStr := strings.TrimSpace(m.restoreIDInput.Value())
				name := strings.TrimSpace(m.restoreNameInput.Value())
				m.restoreIDInput.Blur()
				m.restoreNameInput.Blur()
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
				m.pendingRestoreName = name
				m.mode = detailRestoreSelectStorage
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

		// Storage selection for backup creation.
		if m.mode == detailSelectBackupStorage {
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
				m.mode = detailNormal
				m.actionBusy = true
				m.statusMsg = "Creating backup..."
				m.statusErr = false
				return m, tea.Batch(m.createBackupCmd(storageName), m.spinner.Tick)
			case "esc":
				m.mode = detailNormal
				m.actionBusy = false
				return m, nil
			}
			return m, nil
		}

		// Storage selection for restore target.
		if m.mode == detailRestoreSelectStorage {
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
				m.mode = detailNormal
				m.actionBusy = true
				m.statusMsg = "Restoring backup..."
				m.statusErr = false
				return m, tea.Batch(m.restoreBackupCmd(m.pendingVolid, m.pendingRestoreID, m.pendingRestoreName, storageName), m.spinner.Tick)
			case "esc":
				m.mode = detailNormal
				m.actionBusy = false
				m.pendingVolid = ""
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
		case "U":
			m.actionBusy = true
			m.statusMsg = "Shutting down..."
			m.statusErr = false
			return m, tea.Batch(m.powerCmd("shutdown"), m.spinner.Tick)
		case "R":
			m.actionBusy = true
			m.statusMsg = "Rebooting..."
			m.statusErr = false
			return m, tea.Batch(m.powerCmd("reboot"), m.spinner.Tick)
		case "c":
			return m, m.loadCloneNextIDCmd()
		case "D":
			m.mode = detailConfirmDeleteResource
			return m, nil
		case "ctrl+r", "f5":
			m.loading = true
			m.loadErr = nil
			m.backupLoading = true
			m.backupLoadErr = nil
			m.statusMsg = ""
			m.statusErr = false
			return m, tea.Batch(m.loadSnapshotsCmd(), m.loadBackupsCmd(), m.refreshResourceCmd(), m.spinner.Tick)
		case "tab":
			// Toggle between Snapshots and Backups tabs.
			if m.activeTab == 0 {
				m.activeTab = 1
			} else {
				m.activeTab = 0
			}
			return m, nil
		}

		// Tab-specific keys in normal mode.
		if m.activeTab == 0 {
			// Snapshots tab (Alt/Option + key)
			switch msg.String() {
			case "alt+s", "ß":
				m.mode = detailInputName
				m.input.Focus()
				return m, textinput.Blink
			case "alt+d", "∂":
				if len(m.snapshots) == 0 {
					return m, nil
				}
				m.mode = detailConfirmDelete
				return m, nil
			case "alt+r", "®":
				if len(m.snapshots) == 0 {
					return m, nil
				}
				m.mode = detailConfirmRollback
				return m, nil
			case "esc":
				return m, nil
			}
		} else {
			// Backups tab (Alt/Option + key)
			switch msg.String() {
			case "alt+b", "∫":
				m.mode = detailSelectBackupStorage
				m.actionBusy = true
				m.statusMsg = "Loading storages..."
				m.statusErr = false
				return m, tea.Batch(m.loadBackupStoragesCmd(), m.spinner.Tick)
			case "alt+d", "∂":
				if len(m.backups) == 0 {
					return m, nil
				}
				m.mode = detailConfirmDeleteBackup
				return m, nil
			case "alt+r", "®":
				if len(m.backups) == 0 {
					return m, nil
				}
				volid, _ := m.selectedBackupInfo()
				m.pendingVolid = volid
				return m, m.loadNextIDCmd()
			case "esc":
				return m, nil
			}
		}
	}

	// Delegate table navigation to the active tab's table.
	var cmd tea.Cmd
	if m.activeTab == 0 {
		m.snapTable, cmd = m.snapTable.Update(msg)
	} else {
		m.backupTable, cmd = m.backupTable.Update(msg)
	}
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

func (m detailModel) selectedBackupInfo() (volid, storage string) {
	if len(m.backups) == 0 {
		return "", ""
	}
	cursor := m.backupTable.Cursor()
	if cursor < 0 || cursor >= len(m.backups) {
		return "", ""
	}
	return m.backups[cursor].Volid, m.backups[cursor].Storage
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
	lines = append(lines, StyleHelp.Render("[s] start  [S] stop  [U] shutdown  [R] reboot  [c] clone  [D] delete"))
	lines = append(lines, sep)

	// Tab bar
	lines = append(lines, m.renderTabBar())

	// Tab content
	if m.activeTab == 0 {
		lines = append(lines, m.viewSnapshotsTab()...)
	} else {
		lines = append(lines, m.viewBackupsTab()...)
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

	lines = append(lines, StyleHelp.Render("[Esc] back   [Q] quit"))

	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (m detailModel) renderTabBar() string {
	snapLabel := fmt.Sprintf("Snapshots (%d)", len(m.snapshots))
	backupLabel := fmt.Sprintf("Backups (%d)", len(m.backups))

	if m.activeTab == 0 {
		return StyleTitle.Render(snapLabel) + "  " + StyleDim.Render(backupLabel) +
			"                " + StyleHelp.Render("[Tab] switch")
	}
	return StyleDim.Render(snapLabel) + "  " + StyleTitle.Render(backupLabel) +
		"                " + StyleHelp.Render("[Tab] switch")
}

func (m detailModel) viewSnapshotsTab() []string {
	var lines []string
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
	return lines
}

func (m detailModel) viewBackupsTab() []string {
	var lines []string
	switch {
	case m.backupLoading:
		lines = append(lines, StyleWarning.Render(m.spinner.View()+" Loading backups..."))
	case m.backupLoadErr != nil:
		lines = append(lines, StyleError.Render("  Error: "+m.backupLoadErr.Error()))
		lines = append(lines, StyleHelp.Render("  [ctrl+r] retry"))
	case len(m.backups) == 0:
		lines = append(lines, StyleDim.Render("  No backups"))
	default:
		lines = append(lines, m.backupTable.View())
	}
	return lines
}

func (m detailModel) viewOverlay() []string {
	var lines []string

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

	case detailConfirmDeleteBackup:
		volid, _ := m.selectedBackupInfo()
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(
			fmt.Sprintf("Delete backup %q? [Enter] confirm   [Esc] cancel", volid),
		))

	case detailConfirmDeleteResource:
		typeStr := "VM"
		if m.resource.Type == "lxc" {
			typeStr = "CT"
		}
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(
			fmt.Sprintf("Delete %s %d (%s)? This cannot be undone. [Enter] confirm   [Esc] cancel", typeStr, m.resource.VMID, m.resource.Name),
		))

	case detailCloneInput:
		typeStr := "VM"
		if m.resource.Type == "lxc" {
			typeStr = "CT"
		}
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(fmt.Sprintf("Clone %s %d (%s)", typeStr, m.resource.VMID, m.resource.Name)))
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

	case detailRestoreInputID:
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
		lines = append(lines, StyleHelp.Render("[Tab] switch field  [Enter] confirm  [Esc] cancel"))

	case detailSelectBackupStorage:
		if len(m.availableStorages) > 0 {
			lines = append(lines, "")
			lines = append(lines, StyleWarning.Render("Select backup storage:"))
			for i, s := range m.availableStorages {
				cursor := "  "
				if i == m.storageIdx {
					cursor = "> "
				}
				lines = append(lines, StyleWarning.Render(
					fmt.Sprintf("%s%s (%s free, %s)", cursor, s.Name, s.Avail, s.Type),
				))
			}
			lines = append(lines, StyleHelp.Render("[Enter] select  [Esc] cancel"))
		}

	case detailRestoreSelectStorage:
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
			lines = append(lines, StyleHelp.Render("[Enter] select  [Esc] cancel"))
		}

	default:
		lines = append(lines, "")
		if m.activeTab == 0 {
			if len(m.snapshots) > 0 {
				lines = append(lines, StyleHelp.Render("[Alt+s] new  [Alt+d] delete  [Alt+r] rollback  |  [ctrl+r] refresh"))
			} else {
				lines = append(lines, StyleHelp.Render("[Alt+s] new snapshot"))
			}
		} else {
			if len(m.backups) > 0 {
				lines = append(lines, StyleHelp.Render("[Alt+b] backup  [Alt+d] delete  [Alt+r] restore  |  [ctrl+r] refresh"))
			} else {
				lines = append(lines, StyleHelp.Render("[Alt+b] new backup"))
			}
		}
	}

	return lines
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
