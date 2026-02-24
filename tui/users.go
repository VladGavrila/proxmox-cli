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

type usersMode int

const (
	usersNormal     usersMode = iota
	usersAdding               // add-user form open
	usersConfirmDel           // confirm user deletion

	// Group tab modes.
	usersGroupNormal
	usersGroupAdding            // add-group form open
	usersGroupConfirmDel        // confirm group deletion
	usersGroupDetail            // show group members overlay
	usersGroupSelectMember        // pick from existing users to add
	usersGroupAddMember           // text input to type a userid
	usersGroupConfirmRemoveMember // confirm removal of cursor-selected member
)

// usersFetchedMsg is sent when the async fetch of users completes.
type usersFetchedMsg struct {
	users   proxmox.Users
	err     error
	fetchID int64 // matches usersModel.fetchID; stale responses are discarded
}

// usersActionMsg is sent after a create/delete user action completes.
type usersActionMsg struct {
	message string
	err     error
	reload  bool
}

// groupsFetchedMsg is sent when the async fetch of groups completes.
type groupsFetchedMsg struct {
	groups proxmox.Groups
	err    error
}

// groupsActionMsg is sent after a create/delete group action completes.
type groupsActionMsg struct {
	message string
	err     error
	reload  bool
}

// groupDetailLoadedMsg is sent when a single group's details are fetched.
type groupDetailLoadedMsg struct {
	group *proxmox.Group
	err   error
}

// memberCandidatesLoadedMsg carries the filtered list of users available to add.
type memberCandidatesLoadedMsg struct {
	candidates []string
	err        error
}

type usersModel struct {
	client   *proxmox.Client
	instName string
	users    proxmox.Users
	loading  bool
	err      error
	table    table.Model
	spinner  spinner.Model
	mode     usersMode
	fetchID  int64

	// Add-user form: [0]=userid [1]=firstname [2]=lastname [3]=email.
	addInputs [4]textinput.Model
	addFocus  int

	// Groups tab state.
	groupsTab       bool // true when viewing groups tab
	groups          proxmox.Groups
	groupTable      table.Model
	groupsLoading   bool
	groupsErr       error
	groupsRefreshed time.Time

	// Add-group form: [0]=groupid [1]=comment.
	groupAddInputs [2]textinput.Model
	groupAddFocus  int

	// Group detail overlay.
	selectedGroup      *proxmox.Group
	memberCursor       int
	memberCandidates   []string // users not already in the group
	memberSelectCursor int
	memberAddInput     textinput.Model

	statusMsg     string
	statusErr     bool
	lastRefreshed time.Time

	// Filter state
	userFilter           tableFilter
	groupFilter          tableFilter
	filteredUserIndices  []int // maps table row → m.users index
	filteredGroupIndices []int // maps table row → m.groups index

	width  int
	height int
}

func newUsersModel(c *proxmox.Client, instName string, w, h int) usersModel {
	s := spinner.New()
	s.Spinner = CLISpinner
	s.Style = StyleSpinner

	placeholders := [4]string{
		"user@pve (required)",
		"First name",
		"Last name",
		"Email",
	}
	var inputs [4]textinput.Model
	for i := range inputs {
		ti := textinput.New()
		ti.Placeholder = placeholders[i]
		ti.CharLimit = 100
		inputs[i] = ti
	}

	// Group add form.
	groupPlaceholders := [2]string{
		"group-id (required)",
		"Comment (optional)",
	}
	var groupInputs [2]textinput.Model
	for i := range groupInputs {
		ti := textinput.New()
		ti.Placeholder = groupPlaceholders[i]
		ti.CharLimit = 100
		groupInputs[i] = ti
	}

	memberInput := textinput.New()
	memberInput.Placeholder = "user@realm (e.g. alice@pve)"
	memberInput.CharLimit = 100

	return usersModel{
		client:         c,
		instName:       instName,
		loading:        true,
		spinner:        s,
		fetchID:        time.Now().UnixNano(),
		addInputs:      inputs,
		groupAddInputs: groupInputs,
		memberAddInput: memberInput,
		width:          w,
		height:         h,
	}
}

// fixedUsersColWidth: FIRSTNAME(15)+LASTNAME(15)+EMAIL(30)+ENABLED(8) = 68 + separators ~10 = 78
const fixedUsersColWidth = 68 + 10

func (m usersModel) userIDColWidth() int {
	w := m.width - fixedUsersColWidth - 4
	if w < 15 {
		w = 15
	}
	return w
}

func (m usersModel) withRebuiltTable() usersModel {
	uidWidth := m.userIDColWidth()
	cols := []table.Column{
		{Title: "USERID", Width: uidWidth},
		{Title: "FIRSTNAME", Width: 15},
		{Title: "LASTNAME", Width: 15},
		{Title: "EMAIL", Width: 30},
		{Title: "ENABLED", Width: 8},
	}

	var rows []table.Row
	m.filteredUserIndices = nil
	for i, u := range m.users {
		enabled := "yes"
		if !bool(u.Enable) {
			enabled = "no"
		}
		if !m.userFilter.matches(u.UserID, u.Firstname, u.Lastname, u.Email) {
			continue
		}
		rows = append(rows, table.Row{u.UserID, u.Firstname, u.Lastname, u.Email, enabled})
		m.filteredUserIndices = append(m.filteredUserIndices, i)
	}

	tableHeight := m.height - 11 // padding(2) + title(1) + tab bar(1) + blank(1) + status(1) + help(2) + table border(2) + filter(1)
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

// fixedGroupsColWidth: COMMENT(40) + MEMBERS(10) = 50 + separators ~6 = 56
const fixedGroupsColWidth = 50 + 6

func (m usersModel) groupIDColWidth() int {
	w := m.width - fixedGroupsColWidth - 4
	if w < 15 {
		w = 15
	}
	return w
}

func (m usersModel) withRebuiltGroupTable() usersModel {
	gidWidth := m.groupIDColWidth()
	cols := []table.Column{
		{Title: "GROUPID", Width: gidWidth},
		{Title: "COMMENT", Width: 40},
		{Title: "MEMBERS", Width: 10},
	}

	var rows []table.Row
	m.filteredGroupIndices = nil
	for i, g := range m.groups {
		members := "-"
		if g.Users != "" {
			count := len(strings.Split(g.Users, ","))
			members = fmt.Sprintf("%d", count)
		}
		if !m.groupFilter.matches(g.GroupID, g.Comment) {
			continue
		}
		rows = append(rows, table.Row{g.GroupID, g.Comment, members})
		m.filteredGroupIndices = append(m.filteredGroupIndices, i)
	}

	tableHeight := m.height - 11
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
	m.groupTable = t
	return m
}

func (m usersModel) init() tea.Cmd {
	return tea.Batch(fetchAllUsers(m.client, m.fetchID), m.spinner.Tick)
}

func (m usersModel) update(msg tea.Msg) (usersModel, tea.Cmd) {
	switch msg := msg.(type) {
	case usersFetchedMsg:
		if msg.fetchID != m.fetchID {
			return m, nil // stale response from a previous fetch; discard
		}
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.users = msg.users
		m.lastRefreshed = time.Now()
		m = m.withRebuiltTable()
		return m, nil

	case usersActionMsg:
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
			return m, tea.Batch(fetchAllUsers(m.client, m.fetchID), m.spinner.Tick)
		}
		return m, nil

	case groupsFetchedMsg:
		m.groupsLoading = false
		if msg.err != nil {
			m.groupsErr = msg.err
			return m, nil
		}
		m.groupsErr = nil
		m.groups = msg.groups
		m.groupsRefreshed = time.Now()
		m = m.withRebuiltGroupTable()
		return m, nil

	case groupsActionMsg:
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
		} else {
			m.statusMsg = msg.message
			m.statusErr = false
		}
		if msg.reload {
			m.groupsLoading = true
			return m, tea.Batch(fetchAllGroups(m.client), m.spinner.Tick)
		}
		return m, nil

	case groupDetailLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
			m.mode = usersGroupNormal
			return m, nil
		}
		m.selectedGroup = msg.group
		m.memberCursor = 0
		m.mode = usersGroupDetail
		return m, nil

	case memberCandidatesLoadedMsg:
		if msg.err != nil || len(msg.candidates) == 0 {
			// No candidates available — fall back to free text input.
			m.mode = usersGroupAddMember
			m.memberAddInput.Reset()
			m.memberAddInput.Focus()
			return m, textinput.Blink
		}
		m.memberCandidates = msg.candidates
		m.memberSelectCursor = 0
		m.mode = usersGroupSelectMember
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.loading || m.groupsLoading {
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		if m.groupsTab {
			return m.updateGroupsTab(msg)
		}
		return m.updateUsersTab(msg)
	}

	// Delegate table updates to the active tab's table.
	if m.groupsTab {
		var cmd tea.Cmd
		m.groupTable, cmd = m.groupTable.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m usersModel) updateUsersTab(msg tea.KeyMsg) (usersModel, tea.Cmd) {
	// Filter input mode.
	if m.userFilter.active {
		var rebuild bool
		m.userFilter, rebuild = m.userFilter.handleKey(msg)
		if rebuild {
			m = m.withRebuiltTable()
		}
		return m, nil
	}

	// Add-user form mode.
	if m.mode == usersAdding {
		switch msg.String() {
		case "esc":
			m.mode = usersNormal
			m.clearAddForm()
			return m, nil
		case "enter":
			if m.addFocus < 3 {
				m.addInputs[m.addFocus].Blur()
				m.addFocus++
				m.addInputs[m.addFocus].Focus()
				return m, textinput.Blink
			}
			// Submit on last field.
			uid := strings.TrimSpace(m.addInputs[0].Value())
			firstname := strings.TrimSpace(m.addInputs[1].Value())
			lastname := strings.TrimSpace(m.addInputs[2].Value())
			email := strings.TrimSpace(m.addInputs[3].Value())
			m.mode = usersNormal
			m.clearAddForm()
			if uid == "" {
				m.statusMsg = "User ID is required"
				m.statusErr = true
				return m, nil
			}
			return m, m.createUserCmd(uid, firstname, lastname, email)
		default:
			var cmd tea.Cmd
			m.addInputs[m.addFocus], cmd = m.addInputs[m.addFocus].Update(msg)
			return m, cmd
		}
	}

	// Confirm-delete mode.
	if m.mode == usersConfirmDel {
		switch msg.String() {
		case "enter":
			m.mode = usersNormal
			if len(m.users) == 0 {
				return m, nil
			}
			row := m.table.SelectedRow()
			if len(row) == 0 {
				return m, nil
			}
			userid := row[0]
			return m, m.deleteUserCmd(userid)
		case "esc":
			m.mode = usersNormal
			return m, nil
		}
		return m, nil
	}

	// Normal mode.
	switch msg.String() {
	case "/":
		m.userFilter.active = true
		return m, nil
	case "ctrl+u":
		if m.userFilter.hasActiveFilter() {
			m.userFilter.text = ""
			m = m.withRebuiltTable()
		}
		return m, nil
	case "enter":
		if len(m.filteredUserIndices) == 0 {
			return m, nil
		}
		cursor := m.table.Cursor()
		if cursor >= 0 && cursor < len(m.filteredUserIndices) {
			u := m.users[m.filteredUserIndices[cursor]]
			info := userInfo{
				UserID:    u.UserID,
				Firstname: u.Firstname,
				Lastname:  u.Lastname,
				Email:     u.Email,
			}
			return m, func() tea.Msg {
				return userSelectedMsg{info: info}
			}
		}
	case "a":
		m.mode = usersAdding
		m.addFocus = 0
		m.addInputs[0].Focus()
		return m, textinput.Blink
	case "d":
		if len(m.users) == 0 {
			return m, nil
		}
		m.mode = usersConfirmDel
		return m, nil
	case "ctrl+r":
		m.loading = true
		m.err = nil
		m.fetchID = time.Now().UnixNano()
		return m, tea.Batch(fetchAllUsers(m.client, m.fetchID), m.spinner.Tick)
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m usersModel) updateGroupsTab(msg tea.KeyMsg) (usersModel, tea.Cmd) {
	// Filter input mode.
	if m.groupFilter.active {
		var rebuild bool
		m.groupFilter, rebuild = m.groupFilter.handleKey(msg)
		if rebuild {
			m = m.withRebuiltGroupTable()
		}
		return m, nil
	}

	// Add-group form mode.
	if m.mode == usersGroupAdding {
		switch msg.String() {
		case "esc":
			m.mode = usersGroupNormal
			m.clearGroupAddForm()
			return m, nil
		case "enter":
			if m.groupAddFocus < 1 {
				m.groupAddInputs[m.groupAddFocus].Blur()
				m.groupAddFocus++
				m.groupAddInputs[m.groupAddFocus].Focus()
				return m, textinput.Blink
			}
			// Submit on last field.
			groupid := strings.TrimSpace(m.groupAddInputs[0].Value())
			comment := strings.TrimSpace(m.groupAddInputs[1].Value())
			m.mode = usersGroupNormal
			m.clearGroupAddForm()
			if groupid == "" {
				m.statusMsg = "Group ID is required"
				m.statusErr = true
				return m, nil
			}
			return m, m.createGroupCmd(groupid, comment)
		default:
			var cmd tea.Cmd
			m.groupAddInputs[m.groupAddFocus], cmd = m.groupAddInputs[m.groupAddFocus].Update(msg)
			return m, cmd
		}
	}

	// Confirm-delete mode.
	if m.mode == usersGroupConfirmDel {
		switch msg.String() {
		case "enter":
			m.mode = usersGroupNormal
			if len(m.groups) == 0 {
				return m, nil
			}
			row := m.groupTable.SelectedRow()
			if len(row) == 0 {
				return m, nil
			}
			groupid := row[0]
			return m, m.deleteGroupCmd(groupid)
		case "esc":
			m.mode = usersGroupNormal
			return m, nil
		}
		return m, nil
	}

	// Group detail overlay — interactive member management.
	if m.mode == usersGroupDetail {
		switch msg.String() {
		case "esc":
			m.mode = usersGroupNormal
			m.selectedGroup = nil
			m.memberCursor = 0
			return m, nil
		case "up":
			if m.selectedGroup != nil && m.memberCursor > 0 {
				m.memberCursor--
			}
			return m, nil
		case "down":
			if m.selectedGroup != nil && m.memberCursor < len(m.selectedGroup.Members)-1 {
				m.memberCursor++
			}
			return m, nil
		case "a":
			m.statusMsg = "Loading users..."
			m.statusErr = false
			return m, m.loadMemberCandidatesCmd()
		case "d":
			if m.selectedGroup != nil && len(m.selectedGroup.Members) > 0 {
				m.mode = usersGroupConfirmRemoveMember
			}
			return m, nil
		}
		return m, nil
	}

	// Select-member picker mode.
	if m.mode == usersGroupSelectMember {
		switch msg.String() {
		case "esc":
			m.mode = usersGroupDetail
			m.memberCandidates = nil
			m.statusMsg = ""
			return m, nil
		case "up":
			if m.memberSelectCursor > 0 {
				m.memberSelectCursor--
			}
			return m, nil
		case "down":
			// +1 to account for the "Type manually..." sentinel.
			if m.memberSelectCursor < len(m.memberCandidates) {
				m.memberSelectCursor++
			}
			return m, nil
		case "enter":
			if m.memberSelectCursor == len(m.memberCandidates) {
				// "Type manually..." selected — switch to text input.
				m.mode = usersGroupAddMember
				m.memberAddInput.Reset()
				m.memberAddInput.Focus()
				m.memberCandidates = nil
				return m, textinput.Blink
			}
			userid := m.memberCandidates[m.memberSelectCursor]
			groupid := m.selectedGroup.GroupID
			m.mode = usersGroupNormal
			m.selectedGroup = nil
			m.memberCursor = 0
			m.memberCandidates = nil
			return m, m.addMemberCmd(groupid, userid)
		}
		return m, nil
	}

	// Add-member text input mode.
	if m.mode == usersGroupAddMember {
		switch msg.String() {
		case "esc":
			m.mode = usersGroupDetail
			m.memberAddInput.Reset()
			m.memberAddInput.Blur()
			return m, nil
		case "enter":
			userid := strings.TrimSpace(m.memberAddInput.Value())
			m.memberAddInput.Reset()
			m.memberAddInput.Blur()
			if userid == "" {
				m.statusMsg = "User ID is required"
				m.statusErr = true
				m.mode = usersGroupDetail
				return m, nil
			}
			groupid := m.selectedGroup.GroupID
			m.mode = usersGroupNormal
			m.selectedGroup = nil
			m.memberCursor = 0
			return m, m.addMemberCmd(groupid, userid)
		default:
			var cmd tea.Cmd
			m.memberAddInput, cmd = m.memberAddInput.Update(msg)
			return m, cmd
		}
	}

	// Confirm remove member mode.
	if m.mode == usersGroupConfirmRemoveMember {
		switch msg.String() {
		case "enter":
			if m.selectedGroup != nil && m.memberCursor < len(m.selectedGroup.Members) {
				userid := m.selectedGroup.Members[m.memberCursor]
				groupid := m.selectedGroup.GroupID
				m.mode = usersGroupNormal
				m.selectedGroup = nil
				m.memberCursor = 0
				return m, m.removeMemberCmd(groupid, userid)
			}
			m.mode = usersGroupDetail
			return m, nil
		case "esc":
			m.mode = usersGroupDetail
			return m, nil
		}
		return m, nil
	}

	// Normal groups mode.
	switch msg.String() {
	case "/":
		m.groupFilter.active = true
		return m, nil
	case "ctrl+u":
		if m.groupFilter.hasActiveFilter() {
			m.groupFilter.text = ""
			m = m.withRebuiltGroupTable()
		}
		return m, nil
	case "enter":
		if len(m.groups) == 0 {
			return m, nil
		}
		row := m.groupTable.SelectedRow()
		if len(row) == 0 {
			return m, nil
		}
		groupid := row[0]
		return m, m.loadGroupDetailCmd(groupid)
	case "a":
		m.mode = usersGroupAdding
		m.groupAddFocus = 0
		m.groupAddInputs[0].Focus()
		return m, textinput.Blink
	case "D":
		if len(m.groups) == 0 {
			return m, nil
		}
		m.mode = usersGroupConfirmDel
		return m, nil
	case "ctrl+r":
		m.groupsLoading = true
		m.groupsErr = nil
		return m, tea.Batch(fetchAllGroups(m.client), m.spinner.Tick)
	}

	var cmd tea.Cmd
	m.groupTable, cmd = m.groupTable.Update(msg)
	return m, cmd
}

func (m *usersModel) clearAddForm() {
	for i := range m.addInputs {
		m.addInputs[i].Reset()
		m.addInputs[i].Blur()
	}
	m.addFocus = 0
}

func (m *usersModel) clearGroupAddForm() {
	for i := range m.groupAddInputs {
		m.groupAddInputs[i].Reset()
		m.groupAddInputs[i].Blur()
	}
	m.groupAddFocus = 0
}

func (m usersModel) view() string {
	if m.width == 0 {
		return ""
	}

	if m.groupsTab {
		return m.viewGroupsTab()
	}
	return m.viewUsersTab()
}

// isNormalMode returns true when the users screen is in a base state (no
// overlay/form active) so the router can safely consume Q/Tab/Esc.
func (m usersModel) isNormalMode() bool {
	if m.groupsTab {
		return m.mode == usersGroupNormal || m.mode == usersNormal
	}
	return m.mode == usersNormal
}

// clearFilters resets both tab filters and rebuilds the tables to show all rows.
func (m *usersModel) clearFilters() {
	if m.userFilter.hasActiveFilter() || m.userFilter.active {
		m.userFilter.clear()
		*m = m.withRebuiltTable()
	}
	if m.groupFilter.hasActiveFilter() || m.groupFilter.active {
		m.groupFilter.clear()
		*m = m.withRebuiltGroupTable()
	}
}

// activeFilter returns the filter for the currently active tab.
func (m usersModel) activeFilter() tableFilter {
	if m.groupsTab {
		return m.groupFilter
	}
	return m.userFilter
}

func (m usersModel) renderTabBar() string {
	usersLabel := " Users "
	groupsLabel := " Groups "
	if !m.groupsTab {
		usersLabel = StyleTitle.Render(usersLabel)
		groupsLabel = StyleDim.Render(groupsLabel)
	} else {
		usersLabel = StyleDim.Render(usersLabel)
		groupsLabel = StyleTitle.Render(groupsLabel)
	}
	return usersLabel + StyleDim.Render(" | ") + groupsLabel
}

func (m usersModel) viewUsersTab() string {
	title := StyleTitle.Render(fmt.Sprintf("Users and Groups — %s", m.instName))

	if m.loading {
		return lipgloss.NewStyle().Padding(1, 2).Render(
			title + "\n" + m.renderTabBar() + "\n\n" + StyleWarning.Render(m.spinner.View()+" Loading..."),
		)
	}

	if m.err != nil {
		lines := []string{
			title,
			m.renderTabBar(),
			"",
			StyleError.Render("Error: " + m.err.Error()),
			"",
			renderHelp("[ctrl+r] retry"),
			renderHelp("[Esc] back   [Q] quit"),
		}
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
	}

	var count string
	if m.userFilter.hasActiveFilter() {
		count = StyleDim.Render(fmt.Sprintf(" (%d/%d)", len(m.filteredUserIndices), len(m.users)))
	} else {
		count = StyleDim.Render(fmt.Sprintf(" (%d)", len(m.users)))
	}

	// Add-user form overlay.
	if m.mode == usersAdding {
		labels := []string{"User ID:", "First name:", "Last name:", "Email:"}
		lines := []string{title + count, m.renderTabBar(), "", StyleTitle.Render("Add User"), ""}
		for i, inp := range m.addInputs {
			label := fmt.Sprintf("  %-12s", labels[i])
			if i == m.addFocus {
				lines = append(lines, StyleWarning.Render(label)+inp.View())
			} else {
				lines = append(lines, StyleDim.Render(label)+inp.View())
			}
		}
		lines = append(lines, "")
		lines = append(lines, renderHelp("[Enter] next/save   [Esc] cancel"))
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
	}

	var lines []string
	lines = append(lines, headerLine(title+count, m.width, m.lastRefreshed))
	lines = append(lines, m.renderTabBar())
	lines = append(lines, "")
	lines = append(lines, m.table.View())

	// Filter line.
	if fl := m.userFilter.renderLine(); fl != "" {
		lines = append(lines, fl)
	} else {
		lines = append(lines, "")
	}

	// Confirm-delete overlay.
	if m.mode == usersConfirmDel {
		row := m.table.SelectedRow()
		uid := ""
		if len(row) > 0 {
			uid = row[0]
		}
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(
			fmt.Sprintf("Delete user %q? [Enter] confirm   [Esc] cancel", uid),
		))
	} else {
		// Status line (only shown for errors/success, not refresh time).
		if m.statusMsg != "" && m.statusErr {
			lines = append(lines, StyleError.Render(m.statusMsg))
		} else if m.statusMsg != "" {
			lines = append(lines, StyleSuccess.Render(m.statusMsg))
		} else {
			lines = append(lines, "")
		}
		lines = append(lines, renderHelp("[a] add   [d] delete  [/] filter  |  [Tab] Groups  |  [ctrl+r] refresh"))
		lines = append(lines, renderHelp("[Esc] back   [Q] quit"))
	}

	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (m usersModel) viewGroupsTab() string {
	title := StyleTitle.Render(fmt.Sprintf("Users — %s", m.instName))

	if m.groupsLoading {
		return lipgloss.NewStyle().Padding(1, 2).Render(
			title + "\n" + m.renderTabBar() + "\n\n" + StyleWarning.Render(m.spinner.View()+" Loading groups..."),
		)
	}

	if m.groupsErr != nil {
		lines := []string{
			title,
			m.renderTabBar(),
			"",
			StyleError.Render("Error: " + m.groupsErr.Error()),
			"",
			renderHelp("[ctrl+r] retry"),
			renderHelp("[Esc] back   [Q] quit"),
		}
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
	}

	var count string
	if m.groupFilter.hasActiveFilter() {
		count = StyleDim.Render(fmt.Sprintf(" (%d/%d)", len(m.filteredGroupIndices), len(m.groups)))
	} else {
		count = StyleDim.Render(fmt.Sprintf(" (%d)", len(m.groups)))
	}

	// Add-group form overlay.
	if m.mode == usersGroupAdding {
		labels := []string{"Group ID:", "Comment:"}
		lines := []string{title + count, m.renderTabBar(), "", StyleTitle.Render("Add Group"), ""}
		for i, inp := range m.groupAddInputs {
			label := fmt.Sprintf("  %-12s", labels[i])
			if i == m.groupAddFocus {
				lines = append(lines, StyleWarning.Render(label)+inp.View())
			} else {
				lines = append(lines, StyleDim.Render(label)+inp.View())
			}
		}
		lines = append(lines, "")
		lines = append(lines, renderHelp("[Enter] next/save   [Esc] cancel"))
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
	}

	// Group detail overlay (including select-member, add-member, and confirm-remove sub-modes).
	if (m.mode == usersGroupDetail || m.mode == usersGroupSelectMember || m.mode == usersGroupAddMember || m.mode == usersGroupConfirmRemoveMember) && m.selectedGroup != nil {
		g := m.selectedGroup
		lines := []string{title, m.renderTabBar(), ""}
		lines = append(lines, StyleTitle.Render(fmt.Sprintf("Group: %s", g.GroupID)))
		if g.Comment != "" {
			lines = append(lines, StyleDim.Render(fmt.Sprintf("Comment: %s", g.Comment)))
		}
		lines = append(lines, "")
		if len(g.Members) == 0 {
			lines = append(lines, StyleDim.Render("No members."))
		} else {
			lines = append(lines, fmt.Sprintf("Members (%d):", len(g.Members)))
			for i, member := range g.Members {
				prefix := "  "
				if i == m.memberCursor {
					prefix = StyleWarning.Render("> ")
				}
				lines = append(lines, prefix+member)
			}
		}

		// Select-member picker overlay.
		if m.mode == usersGroupSelectMember {
			lines = append(lines, "")
			lines = append(lines, StyleWarning.Render("Select a user to add:"))
			for i, u := range m.memberCandidates {
				if i == m.memberSelectCursor {
					lines = append(lines, StyleWarning.Render("> ")+u)
				} else {
					lines = append(lines, "  "+u)
				}
			}
			manualCursor := "  "
			if m.memberSelectCursor == len(m.memberCandidates) {
				manualCursor = "> "
			}
			lines = append(lines, StyleDim.Render(manualCursor+"Type manually..."))
			lines = append(lines, "")
			lines = append(lines, renderHelp("[Enter] select   [Esc] back"))
		} else if m.mode == usersGroupAddMember {
			// Free text input fallback.
			lines = append(lines, "")
			lines = append(lines, StyleWarning.Render("  Add user: ")+m.memberAddInput.View())
			lines = append(lines, "")
			lines = append(lines, renderHelp("[Enter] add   [Esc] cancel"))
		} else if m.mode == usersGroupConfirmRemoveMember {
			member := ""
			if m.memberCursor < len(g.Members) {
				member = g.Members[m.memberCursor]
			}
			lines = append(lines, "")
			lines = append(lines, StyleWarning.Render(
				fmt.Sprintf("Remove %q from group? [Enter] confirm   [Esc] cancel", member),
			))
		} else {
			lines = append(lines, "")
			lines = append(lines, renderHelp("[a] add member   [d] remove   [↑/↓] navigate   [Esc] back"))
		}
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
	}

	var lines []string
	lines = append(lines, headerLine(title+count, m.width, m.groupsRefreshed))
	lines = append(lines, m.renderTabBar())
	lines = append(lines, "")
	lines = append(lines, m.groupTable.View())

	// Filter line.
	if fl := m.groupFilter.renderLine(); fl != "" {
		lines = append(lines, fl)
	} else {
		lines = append(lines, "")
	}

	// Confirm-delete overlay.
	if m.mode == usersGroupConfirmDel {
		row := m.groupTable.SelectedRow()
		gid := ""
		if len(row) > 0 {
			gid = row[0]
		}
		lines = append(lines, "")
		lines = append(lines, StyleWarning.Render(
			fmt.Sprintf("Delete group %q? [Enter] confirm   [Esc] cancel", gid),
		))
	} else {
		if m.statusMsg != "" && m.statusErr {
			lines = append(lines, StyleError.Render(m.statusMsg))
		} else if m.statusMsg != "" {
			lines = append(lines, StyleSuccess.Render(m.statusMsg))
		} else {
			lines = append(lines, "")
		}
		lines = append(lines, renderHelp("[a] add   [D] delete   [Enter] details  [/] filter  |  [Tab] Backups  |  [ctrl+r] refresh"))
		lines = append(lines, renderHelp("[Esc] back   [Q] quit"))
	}

	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func fetchAllUsers(c *proxmox.Client, fetchID int64) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		users, err := actions.ListUsers(ctx, c)
		return usersFetchedMsg{users: users, err: err, fetchID: fetchID}
	}
}

func fetchAllGroups(c *proxmox.Client) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		groups, err := actions.ListGroups(ctx, c)
		return groupsFetchedMsg{groups: groups, err: err}
	}
}

func (m usersModel) createUserCmd(userid, firstname, lastname, email string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		u := &proxmox.NewUser{
			UserID:    userid,
			Firstname: firstname,
			Lastname:  lastname,
			Email:     email,
			Enable:    true,
		}
		if err := actions.CreateUser(ctx, c, u); err != nil {
			return usersActionMsg{err: err}
		}
		return usersActionMsg{message: fmt.Sprintf("User %q created", userid), reload: true}
	}
}

func (m usersModel) deleteUserCmd(userid string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		if err := actions.DeleteUser(ctx, c, userid); err != nil {
			return usersActionMsg{err: err}
		}
		return usersActionMsg{message: fmt.Sprintf("User %q deleted", userid), reload: true}
	}
}

func (m usersModel) createGroupCmd(groupid, comment string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		if err := actions.CreateGroup(ctx, c, groupid, comment); err != nil {
			return groupsActionMsg{err: err}
		}
		return groupsActionMsg{message: fmt.Sprintf("Group %q created", groupid), reload: true}
	}
}

func (m usersModel) deleteGroupCmd(groupid string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		if err := actions.DeleteGroup(ctx, c, groupid); err != nil {
			return groupsActionMsg{err: err}
		}
		return groupsActionMsg{message: fmt.Sprintf("Group %q deleted", groupid), reload: true}
	}
}

func (m usersModel) loadGroupDetailCmd(groupid string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		group, err := actions.GetGroup(ctx, c, groupid)
		return groupDetailLoadedMsg{group: group, err: err}
	}
}

func (m usersModel) loadMemberCandidatesCmd() tea.Cmd {
	c := m.client
	members := make(map[string]bool, len(m.selectedGroup.Members))
	for _, member := range m.selectedGroup.Members {
		members[member] = true
	}
	return func() tea.Msg {
		ctx := context.Background()
		users, err := actions.ListUsers(ctx, c)
		if err != nil {
			return memberCandidatesLoadedMsg{err: err}
		}
		var candidates []string
		for _, u := range users {
			if !members[u.UserID] {
				candidates = append(candidates, u.UserID)
			}
		}
		return memberCandidatesLoadedMsg{candidates: candidates}
	}
}

func (m usersModel) addMemberCmd(groupid, userid string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		if err := actions.AddUserToGroup(ctx, c, userid, groupid); err != nil {
			return groupsActionMsg{err: err}
		}
		return groupsActionMsg{message: fmt.Sprintf("User %q added to group %q", userid, groupid), reload: true}
	}
}

func (m usersModel) removeMemberCmd(groupid, userid string) tea.Cmd {
	c := m.client
	return func() tea.Msg {
		ctx := context.Background()
		if err := actions.RemoveUserFromGroup(ctx, c, userid, groupid); err != nil {
			return groupsActionMsg{err: err}
		}
		return groupsActionMsg{message: fmt.Sprintf("User %q removed from group %q", userid, groupid), reload: true}
	}
}
