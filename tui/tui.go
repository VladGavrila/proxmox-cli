// Package tui implements the interactive terminal user interface for pxve.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	proxmox "github.com/luthermonson/go-proxmox"

	"github.com/chupakbra/proxmox-cli/internal/config"
)

type screen int

const (
	screenSelector   screen = iota
	screenList               // VMs / containers
	screenUsers              // Proxmox users
	screenBackups            // cluster-wide backups
	screenDetail             // VM / CT detail + snapshots
	screenUserDetail         // User detail + tokens + ACLs
)

// instanceSelectedMsg is sent by the selector when the user connects to an instance.
type instanceSelectedMsg struct {
	client *proxmox.Client
	name   string
}

// resourceSelectedMsg is sent by the list when the user selects a VM or CT.
type resourceSelectedMsg struct {
	resource proxmox.ClusterResource
}

// userInfo carries the display fields from a proxmox.User for screen transitions.
type userInfo struct {
	UserID    string
	Firstname string
	Lastname  string
	Email     string
}

// userSelectedMsg is sent by the users screen when the user selects a Proxmox user.
type userSelectedMsg struct {
	info userInfo
}

// appModel is the top-level Bubble Tea model acting as a screen router.
type appModel struct {
	screen     screen
	width      int
	height     int
	selector   selectorModel
	list       listModel
	users      usersModel
	backups    backupsScreenModel
	detail     detailModel
	userDetail userDetailModel

	// Caches to avoid re-connecting and re-fetching when switching instances.
	clientCache map[string]*proxmox.Client
	listCache   map[string]listModel
}

func newAppModel(cfg *config.Config) appModel {
	cc := make(map[string]*proxmox.Client)
	return appModel{
		screen:      screenSelector,
		selector:    newSelectorModel(cfg, cc),
		clientCache: cc,
		listCache:   make(map[string]listModel),
	}
}

func (a appModel) Init() tea.Cmd {
	return a.selector.init()
}

func (a appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle router-level messages first.
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.selector.width = msg.Width
		a.selector.height = msg.Height
		a.selector.table = a.selector.buildTable()
		a.selector.discoverTable = a.selector.buildDiscoverTable()
		a.list.width = msg.Width
		a.list.height = msg.Height
		a.users.width = msg.Width
		a.users.height = msg.Height
		a.backups.width = msg.Width
		a.backups.height = msg.Height
		a.detail.width = msg.Width
		a.detail.height = msg.Height
		a.userDetail.width = msg.Width
		a.userDetail.height = msg.Height
		if !a.list.loading && len(a.list.resources) > 0 {
			a.list = a.list.withRebuiltTable()
		}
		if !a.users.loading && len(a.users.users) > 0 {
			a.users = a.users.withRebuiltTable()
		}
		if !a.users.groupsLoading && len(a.users.groups) > 0 {
			a.users = a.users.withRebuiltGroupTable()
		}
		if !a.backups.loading && len(a.backups.backups) > 0 {
			a.backups = a.backups.withRebuiltTable()
		}
		if !a.detail.loading {
			a.detail = a.detail.withRebuiltTable()
		}
		if !a.userDetail.loading {
			a.userDetail = a.userDetail.withRebuiltTables()
		}
		return a, nil

	case instanceSelectedMsg:
		a.screen = screenList
		a.selector.connecting = false // connection succeeded; allow re-selection on Esc
		a.clientCache[msg.name] = msg.client

		// Restore cached list if available (instant switch).
		if cached, ok := a.listCache[msg.name]; ok {
			a.list = cached
			a.list.width = a.width
			a.list.height = a.height
			if len(a.list.resources) > 0 {
				a.list = a.list.withRebuiltTable()
			}
			a.users = usersModel{}
			a.backups = backupsScreenModel{}
			return a, nil
		}

		a.list = newListModel(msg.client, msg.name, a.width, a.height)
		a.users = usersModel{}
		a.backups = backupsScreenModel{}
		return a, a.list.init()

	case resourceSelectedMsg:
		a.screen = screenDetail
		a.detail = newDetailModel(a.list.client, msg.resource, a.width, a.height)
		return a, a.detail.init()

	case resourceDeletedMsg:
		a.screen = screenList
		// Invalidate the list cache so the deleted resource disappears.
		delete(a.listCache, a.list.instName)
		a.list = newListModel(a.list.client, a.list.instName, a.width, a.height)
		return a, a.list.init()

	case userSelectedMsg:
		a.screen = screenUserDetail
		a.userDetail = newUserDetailModel(a.users.client, msg.info, a.width, a.height)
		return a, a.userDetail.init()

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return a, tea.Quit

		case "Q":
			// Let 'Q' pass through to sub-model when it's in a non-normal mode or filter is active.
			if a.screen == screenList && (a.list.mode != listNormal || a.list.filter.active) {
				break
			}
			if a.screen == screenDetail && (a.detail.mode != detailNormal || a.detail.activeFilter().active) {
				break
			}
			if a.screen == screenUserDetail && (a.userDetail.mode != userDetailNormal || a.userDetail.activeFilter().active) {
				break
			}
			if a.screen == screenSelector && a.selector.mode != selectorNormal {
				break
			}
			if a.screen == screenUsers && (!a.users.isNormalMode() || a.users.activeFilter().active) {
				break
			}
			if a.screen == screenBackups && (a.backups.mode != backupsScreenNormal || a.backups.filter.active) {
				break
			}
			return a, tea.Quit

		case "tab":
			// Don't switch screens when a sub-model is in a non-normal mode or filter is active.
			if a.screen == screenList && (a.list.mode != listNormal || a.list.filter.active) {
				break
			}
			if a.screen == screenUsers && (!a.users.isNormalMode() || a.users.activeFilter().active) {
				break
			}
			if a.screen == screenBackups && (a.backups.mode != backupsScreenNormal || a.backups.filter.active) {
				break
			}
			switch a.screen {
			case screenList:
				a.list.clearFilter()
				a.screen = screenUsers
				if a.users.client == nil {
					a.users = newUsersModel(a.list.client, a.list.instName, a.width, a.height)
					return a, a.users.init()
				}
				return a, nil
			case screenUsers:
				if !a.users.groupsTab {
					// Switch from users tab to groups tab (internal).
					a.users.clearFilters()
					a.users.groupsTab = true
					a.users.statusMsg = ""
					if a.users.groups == nil {
						a.users.groupsLoading = true
						return a, tea.Batch(fetchAllGroups(a.users.client), a.users.spinner.Tick)
					}
					return a, nil
				}
				// Switch from groups tab to backups screen.
				a.users.clearFilters()
				a.users.groupsTab = false
				a.screen = screenBackups
				if a.backups.client == nil {
					a.backups = newBackupsScreenModel(a.list.client, a.list.instName, a.width, a.height)
					return a, a.backups.init()
				}
				return a, nil
			case screenBackups:
				a.backups.clearFilter()
				a.screen = screenList
				return a, nil
			}

		case "shift+tab":
			if a.screen == screenList && (a.list.mode != listNormal || a.list.filter.active) {
				break
			}
			if a.screen == screenUsers && (!a.users.isNormalMode() || a.users.activeFilter().active) {
				break
			}
			if a.screen == screenBackups && (a.backups.mode != backupsScreenNormal || a.backups.filter.active) {
				break
			}
			switch a.screen {
			case screenList:
				a.list.clearFilter()
				a.screen = screenBackups
				if a.backups.client == nil {
					a.backups = newBackupsScreenModel(a.list.client, a.list.instName, a.width, a.height)
					return a, a.backups.init()
				}
				return a, nil
			case screenBackups:
				a.backups.clearFilter()
				a.screen = screenUsers
				if a.users.client == nil {
					a.users = newUsersModel(a.list.client, a.list.instName, a.width, a.height)
					return a, a.users.init()
				}
				a.users.groupsTab = true
				a.users.statusMsg = ""
				if a.users.groups == nil {
					a.users.groupsLoading = true
					return a, tea.Batch(fetchAllGroups(a.users.client), a.users.spinner.Tick)
				}
				return a, nil
			case screenUsers:
				if a.users.groupsTab {
					a.users.clearFilters()
					a.users.groupsTab = false
					a.users.statusMsg = ""
					return a, nil
				}
				a.users.clearFilters()
				a.screen = screenList
				return a, nil
			}

		case "esc":
			switch a.screen {
			case screenUserDetail:
				if a.userDetail.mode != userDetailNormal || a.userDetail.activeFilter().active {
					break // let userDetail handle dialog/filter dismissal
				}
				a.screen = screenUsers
				return a, nil
			case screenDetail:
				if a.detail.mode != detailNormal || a.detail.activeFilter().active {
					break // let detail handle dialog/filter dismissal
				}
				a.screen = screenList
				return a, nil
			case screenList:
				if a.list.mode != listNormal || a.list.filter.active {
					break // let list handle dialog/filter dismissal
				}
				a.list.clearFilter()
				a.listCache[a.list.instName] = a.list
				a.selector.current = a.list.instName
				a.selector.table = a.selector.buildTable()
				a.screen = screenSelector
				return a, nil
			case screenUsers:
				if !a.users.isNormalMode() || a.users.activeFilter().active {
					break // let users handle dialog/overlay/filter dismissal
				}
				a.users.clearFilters()
				a.listCache[a.list.instName] = a.list
				a.selector.current = a.list.instName
				a.selector.table = a.selector.buildTable()
				a.screen = screenSelector
				return a, nil
			case screenBackups:
				if a.backups.mode != backupsScreenNormal || a.backups.filter.active {
					break // let backups handle dialog/filter dismissal
				}
				a.backups.clearFilter()
				a.listCache[a.list.instName] = a.list
				a.selector.current = a.list.instName
				a.selector.table = a.selector.buildTable()
				a.screen = screenSelector
				return a, nil
			// screenSelector: fall through — selector handles esc (closes help / add form).
			}
		}
	}

	// Delegate all other messages to the active sub-model.
	var cmd tea.Cmd
	switch a.screen {
	case screenSelector:
		a.selector, cmd = a.selector.update(msg)
	case screenList:
		a.list, cmd = a.list.update(msg)
	case screenUsers:
		a.users, cmd = a.users.update(msg)
	case screenBackups:
		a.backups, cmd = a.backups.update(msg)
	case screenDetail:
		a.detail, cmd = a.detail.update(msg)
	case screenUserDetail:
		a.userDetail, cmd = a.userDetail.update(msg)
	}
	return a, cmd
}

func (a appModel) View() string {
	switch a.screen {
	case screenSelector:
		return a.selector.view()
	case screenList:
		return a.list.view()
	case screenUsers:
		return a.users.view()
	case screenBackups:
		return a.backups.view()
	case screenDetail:
		return a.detail.view()
	case screenUserDetail:
		return a.userDetail.view()
	}
	return ""
}

// LaunchTUI starts the Bubble Tea program and blocks until the user quits.
func LaunchTUI(cfg *config.Config) error {
	m := newAppModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
