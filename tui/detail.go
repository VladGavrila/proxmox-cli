package tui

import (
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	proxmox "github.com/luthermonson/go-proxmox"
)

// tagInputRegex validates tag names: letters, digits, hyphens, underscores, dots.
var tagInputRegex = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

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
	detailConfirmTemplate                // confirm convert to template
	detailResizeDisk                     // text inputs for disk resize (disk ID + size delta)
	detailTagManage                      // browse and remove tags (cursor list)
	detailTagSelect                      // pick an existing instance tag to add
	detailTagAdd                         // text input to add a new tag
	detailSelectMoveDisk                 // cursor picker: choose which disk to move
	detailSelectMoveStorage              // cursor picker: choose target storage for move
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

// allTagsLoadedMsg carries all unique tags across the instance, used for tag picker.
type allTagsLoadedMsg struct {
	tags []string
	err  error
}

// primaryDiskLoadedMsg carries the storage name of the resource's primary disk.
type primaryDiskLoadedMsg struct {
	location string // e.g. "local-lvm"; empty on error
}

// diskListLoadedMsg is sent when disk enumeration completes for move.
type diskListLoadedMsg struct {
	disks map[string]string // disk name → spec string
	err   error
}

// moveStoragesLoadedMsg is sent when move-target storage discovery completes.
type moveStoragesLoadedMsg struct {
	storages []storageChoice
	err      error
}

// tagUpdatedMsg is sent after a tag add/remove completes, carrying the new
// tags string so the display updates immediately without a cluster refresh.
type tagUpdatedMsg struct {
	newTags string
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

	// Disk resize input state
	resizeDiskInput textinput.Model
	resizeSizeInput textinput.Model
	resizeDiskField int // 0 = disk ID, 1 = size delta

	diskLocation string // storage name of primary disk (e.g. "local-lvm"), loaded on init

	// Tag management state
	tagInput      textinput.Model
	tagIdx        int      // cursor in current resource's tag list
	instanceTags  []string // all tags in the instance (loaded on demand for picker)
	tagSelectIdx  int      // cursor in the instance tag picker
	tagsDirty     bool     // true after tagUpdatedMsg; prevents resourceRefreshedMsg from overwriting Tags

	// Disk move state
	availableDisks map[string]string
	diskMoveKeys   []string // sorted keys of availableDisks
	diskMoveIdx    int
	pendingMoveDisk string
	moveStorages   []storageChoice
	moveStorageIdx int

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

	rdiskInput := textinput.New()
	rdiskInput.Placeholder = "e.g. scsi0, rootfs"
	rdiskInput.CharLimit = 20
	rdiskInput.Width = 20

	rsizeInput := textinput.New()
	rsizeInput.Placeholder = "e.g. 10G"
	rsizeInput.CharLimit = 10
	rsizeInput.Width = 10

	tagInput := textinput.New()
	tagInput.Placeholder = "tag name"
	tagInput.CharLimit = 40

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
		resizeDiskInput:  rdiskInput,
		resizeSizeInput:  rsizeInput,
		tagInput:         tagInput,
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
	return tea.Batch(m.loadSnapshotsCmd(), m.loadBackupsCmd(), m.loadPrimaryDiskCmd(), m.spinner.Tick)
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

	case allTagsLoadedMsg:
		m.actionBusy = false
		m.statusMsg = ""
		// Build a filtered list: exclude tags already on this resource.
		existing := parseTags(m.resource.Tags)
		existingSet := make(map[string]bool, len(existing))
		for _, t := range existing {
			existingSet[t] = true
		}
		var available []string
		for _, t := range msg.tags {
			if !existingSet[t] {
				available = append(available, t)
			}
		}
		// If no usable tags (error, none defined, or all already applied): text input.
		if msg.err != nil || len(available) == 0 {
			m.tagInput.Reset()
			m.tagInput.Focus()
			m.mode = detailTagAdd
			return m, textinput.Blink
		}
		m.instanceTags = available
		m.tagSelectIdx = 0
		m.mode = detailTagSelect
		return m, nil

	case primaryDiskLoadedMsg:
		m.diskLocation = msg.location
		return m, nil

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
			return m, tea.Batch(m.loadMoveStoragesCmd(), m.spinner.Tick)
		}
		m.diskMoveIdx = 0
		m.mode = detailSelectMoveDisk
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
			m.mode = detailNormal
			m.actionBusy = true
			m.statusMsg = "Moving disk..."
			return m, tea.Batch(m.moveDiskCmd(m.pendingMoveDisk, msg.storages[0].Name), m.spinner.Tick)
		}
		m.moveStorageIdx = 0
		m.mode = detailSelectMoveStorage
		return m, nil

	case tagUpdatedMsg:
		m.actionBusy = false
		m.resource.Tags = msg.newTags
		m.tagsDirty = true
		m.statusMsg = msg.message
		m.statusErr = false
		return m, tea.ClearScreen

	case resourceRefreshedMsg:
		if msg.err == nil {
			savedTags := m.resource.Tags
			m.resource = msg.resource
			if m.tagsDirty {
				m.resource.Tags = savedTags
				m.tagsDirty = false
			}
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
		switch m.mode {
		case detailInputName:
			return m.handleInputNameMode(msg)
		case detailConfirmDelete:
			return m.handleConfirmDeleteMode(msg)
		case detailConfirmRollback:
			return m.handleConfirmRollbackMode(msg)
		case detailConfirmDeleteBackup:
			return m.handleConfirmDeleteBackupMode(msg)
		case detailConfirmDeleteResource:
			return m.handleConfirmDeleteResourceMode(msg)
		case detailConfirmTemplate:
			return m.handleConfirmTemplateMode(msg)
		case detailCloneInput:
			return m.handleCloneInputMode(msg)
		case detailRestoreInputID:
			return m.handleRestoreInputIDMode(msg)
		case detailSelectBackupStorage:
			return m.handleSelectBackupStorageMode(msg)
		case detailRestoreSelectStorage:
			return m.handleRestoreSelectStorageMode(msg)
		case detailResizeDisk:
			return m.handleResizeDiskMode(msg)
		case detailSelectMoveDisk:
			return m.handleSelectMoveDiskMode(msg)
		case detailSelectMoveStorage:
			return m.handleSelectMoveStorageMode(msg)
		case detailTagManage:
			return m.handleTagManageMode(msg)
		case detailTagSelect:
			return m.handleTagSelectMode(msg)
		case detailTagAdd:
			return m.handleTagAddMode(msg)
		}
		// detailNormal falls through.
		if m.actionBusy {
			return m, nil
		}
		return m.handleNormalMode(msg)
	}

	// Only delegate non-key events to the table in normal mode.
	if m.mode != detailNormal {
		return m, nil
	}
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
