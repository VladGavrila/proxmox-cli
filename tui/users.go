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

	statusMsg     string
	statusErr     bool
	lastRefreshed time.Time

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

	return usersModel{
		client:    c,
		instName:  instName,
		loading:   true,
		spinner:   s,
		fetchID:   time.Now().UnixNano(),
		addInputs: inputs,
		width:     w,
		height:    h,
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

	rows := make([]table.Row, len(m.users))
	for i, u := range m.users {
		enabled := "yes"
		if !bool(u.Enable) {
			enabled = "no"
		}
		rows[i] = table.Row{u.UserID, u.Firstname, u.Lastname, u.Email, enabled}
	}

	tableHeight := m.height - 9 // padding(2) + title(1) + blank(1) + status(1) + help(2) + table border(2)
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

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.loading {
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
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
		case "enter":
			if len(m.users) == 0 {
				return m, nil
			}
			cursor := m.table.Cursor()
			if cursor >= 0 && cursor < len(m.users) {
				u := m.users[cursor]
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
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *usersModel) clearAddForm() {
	for i := range m.addInputs {
		m.addInputs[i].Reset()
		m.addInputs[i].Blur()
	}
	m.addFocus = 0
}

func (m usersModel) view() string {
	if m.width == 0 {
		return ""
	}

	title := StyleTitle.Render(fmt.Sprintf("Users — %s", m.instName))

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

	count := StyleDim.Render(fmt.Sprintf(" (%d)", len(m.users)))

	// Add-user form overlay.
	if m.mode == usersAdding {
		labels := []string{"User ID:", "First name:", "Last name:", "Email:"}
		lines := []string{title + count, "", StyleTitle.Render("Add User"), ""}
		for i, inp := range m.addInputs {
			label := fmt.Sprintf("  %-12s", labels[i])
			if i == m.addFocus {
				lines = append(lines, StyleWarning.Render(label)+inp.View())
			} else {
				lines = append(lines, StyleDim.Render(label)+inp.View())
			}
		}
		lines = append(lines, "")
		lines = append(lines, StyleHelp.Render("[Enter] next/save   [Esc] cancel"))
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
	}

	var lines []string
	lines = append(lines, headerLine(title+count, m.width, m.lastRefreshed))
	lines = append(lines, "")
	lines = append(lines, m.table.View())

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
		lines = append(lines, StyleHelp.Render("[a] add   [d] delete  |  [Tab] backups  |  [ctrl+r] refresh"))
		lines = append(lines, StyleHelp.Render("[Esc] back   [Q] quit"))
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
