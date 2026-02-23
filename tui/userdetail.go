package tui

import (
	"context"
	"fmt"
	"sort"
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

type userDetailMode int

const (
	userDetailNormal             userDetailMode = iota
	userDetailInputToken                        // entering token name/id to create
	userDetailShowToken                         // displaying newly created token value (one-time)
	userDetailConfirmDeleteToken                // confirming token deletion
	userDetailInputGrant                        // entering path + role to grant
	userDetailConfirmRevoke                     // confirming ACL revocation
)

// userDetailLoadedMsg is sent when the async load of tokens+ACLs+roles completes.
type userDetailLoadedMsg struct {
	tokens proxmox.Tokens
	acls   []proxmox.ACL // filtered for this user
	roles  []string      // available role IDs
	err    error
}

// userDetailActionMsg is sent after a token/ACL action completes.
type userDetailActionMsg struct {
	message       string
	err           error
	reload        bool
	newTokenID    string // set when a token was just created
	newTokenValue string // set when a token was just created (shown once)
}

type userDetailModel struct {
	client *proxmox.Client
	info   userInfo

	tokens proxmox.Tokens
	acls   []proxmox.ACL
	roles  []string // available role names for the grant form hint

	loading bool
	loadErr error

	activeTab   int // 0=Tokens, 1=ACLs
	tokensTable table.Model
	aclsTable   table.Model
	spinner     spinner.Model
	mode        userDetailMode

	// Token creation input.
	tokenInput textinput.Model

	// Newly created token shown once.
	newTokenID    string
	newTokenValue string

	// Grant form: [0]=path [1]=role.
	grantInputs [2]textinput.Model
	grantFocus  int

	statusMsg     string
	statusErr     bool
	lastRefreshed time.Time

	width  int
	height int
}

func newUserDetailModel(c *proxmox.Client, info userInfo, w, h int) userDetailModel {
	s := spinner.New()
	s.Spinner = CLISpinner
	s.Style = StyleSpinner

	ti := textinput.New()
	ti.Placeholder = "token-name"
	ti.CharLimit = 64

	grantPlaceholders := [2]string{"/vms/100  (or / for all)", "PVEVMAdmin"}
	var grantInputs [2]textinput.Model
	for i := range grantInputs {
		gi := textinput.New()
		gi.Placeholder = grantPlaceholders[i]
		gi.CharLimit = 100
		grantInputs[i] = gi
	}

	return userDetailModel{
		client:      c,
		info:        info,
		loading:     true,
		spinner:     s,
		tokenInput:  ti,
		grantInputs: grantInputs,
		width:       w,
		height:      h,
	}
}

func (m userDetailModel) withRebuiltTables() userDetailModel {
	// Tokens table.
	tokenIDWidth := 25
	commentWidth := 25
	expiresWidth := 12
	privsepWidth := 8

	if m.width > 0 {
		remaining := m.width - commentWidth - expiresWidth - privsepWidth - 12
		if remaining > tokenIDWidth {
			tokenIDWidth = remaining
		}
	}

	tokenCols := []table.Column{
		{Title: "TOKEN ID", Width: tokenIDWidth},
		{Title: "COMMENT", Width: commentWidth},
		{Title: "EXPIRES", Width: expiresWidth},
		{Title: "PRIVSEP", Width: privsepWidth},
	}
	tokenRows := make([]table.Row, len(m.tokens))
	for i, tok := range m.tokens {
		privsep := "no"
		if bool(tok.Privsep) {
			privsep = "yes"
		}
		tokenRows[i] = table.Row{tok.TokenID, tok.Comment, formatTokenExpire(tok.Expire), privsep}
	}

	// ACLs table.
	pathWidth := 30
	roleWidth := 20
	propagateWidth := 10
	if m.width > 0 {
		remaining := m.width - roleWidth - propagateWidth - 12
		if remaining > pathWidth {
			pathWidth = remaining
		}
	}

	aclCols := []table.Column{
		{Title: "PATH", Width: pathWidth},
		{Title: "ROLE", Width: roleWidth},
		{Title: "PROPAGATE", Width: propagateWidth},
	}
	aclRows := make([]table.Row, len(m.acls))
	for i, acl := range m.acls {
		propagate := "no"
		if bool(acl.Propagate) {
			propagate = "yes"
		}
		aclRows[i] = table.Row{acl.Path, acl.RoleID, propagate}
	}

	// Fixed overhead: padding(2) + title(1) + tabBar(1) + blank(1) + count(1) + status(1) + help(2) = 9
	// Plus table header+border(2) = 11.
	// ACLs tab adds roles hint lines: 1 (header) + ceil(len(roles)/3).
	rolesLines := 0
	if len(m.roles) > 0 {
		rolesLines = 1 + (len(m.roles)+2)/3
	}
	overhead := 11 + rolesLines
	tableHeight := m.height - overhead
	if tableHeight < 3 {
		tableHeight = 3
	}

	buildTable := func(cols []table.Column, rows []table.Row) table.Model {
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
		return t
	}

	m.tokensTable = buildTable(tokenCols, tokenRows)
	m.aclsTable = buildTable(aclCols, aclRows)
	return m
}

func (m userDetailModel) init() tea.Cmd {
	return tea.Batch(m.loadCmd(), m.spinner.Tick)
}

func (m userDetailModel) loadCmd() tea.Cmd {
	c := m.client
	userid := m.info.UserID
	return func() tea.Msg {
		ctx := context.Background()

		tokens, err := actions.ListTokens(ctx, c, userid)
		if err != nil {
			return userDetailLoadedMsg{err: err}
		}

		allACLs, err := actions.ListACLs(ctx, c)
		if err != nil {
			return userDetailLoadedMsg{err: err}
		}
		var filteredACLs []proxmox.ACL
		for _, a := range allACLs {
			if a.Type == "user" && a.UGID == userid {
				filteredACLs = append(filteredACLs, *a)
			}
		}

		allRoles, err := actions.ListRoles(ctx, c)
		if err != nil {
			return userDetailLoadedMsg{err: err}
		}
		var roleNames []string
		for _, r := range allRoles {
			roleNames = append(roleNames, r.RoleID)
		}
		sort.Strings(roleNames)

		return userDetailLoadedMsg{tokens: tokens, acls: filteredACLs, roles: roleNames}
	}
}

func (m userDetailModel) update(msg tea.Msg) (userDetailModel, tea.Cmd) {
	switch msg := msg.(type) {
	case userDetailLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		m.loadErr = nil
		m.tokens = msg.tokens
		m.acls = msg.acls
		m.roles = msg.roles
		m.lastRefreshed = time.Now()
		m = m.withRebuiltTables()
		return m, nil

	case userDetailActionMsg:
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
			m.statusErr = true
			return m, nil
		}
		m.statusMsg = msg.message
		m.statusErr = false
		if msg.newTokenValue != "" {
			m.newTokenID = msg.newTokenID
			m.newTokenValue = msg.newTokenValue
			m.mode = userDetailShowToken
		}
		if msg.reload {
			m.loading = true
			return m, tea.Batch(m.loadCmd(), m.spinner.Tick)
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
		// Show-token mode: any key dismisses.
		if m.mode == userDetailShowToken {
			if msg.String() == "enter" || msg.String() == "esc" {
				m.mode = userDetailNormal
				m.newTokenID = ""
				m.newTokenValue = ""
			}
			return m, nil
		}

		// Token input mode.
		if m.mode == userDetailInputToken {
			switch msg.String() {
			case "esc":
				m.mode = userDetailNormal
				m.tokenInput.Reset()
				m.tokenInput.Blur()
				return m, nil
			case "enter":
				name := strings.TrimSpace(m.tokenInput.Value())
				m.tokenInput.Reset()
				m.tokenInput.Blur()
				m.mode = userDetailNormal
				if name == "" {
					return m, nil
				}
				return m, m.createTokenCmd(name)
			default:
				var cmd tea.Cmd
				m.tokenInput, cmd = m.tokenInput.Update(msg)
				return m, cmd
			}
		}

		// Confirm-delete-token mode.
		if m.mode == userDetailConfirmDeleteToken {
			switch msg.String() {
			case "enter":
				m.mode = userDetailNormal
				if len(m.tokens) == 0 {
					return m, nil
				}
				cursor := m.tokensTable.Cursor()
				if cursor < 0 || cursor >= len(m.tokens) {
					return m, nil
				}
				tokenID := m.tokens[cursor].TokenID
				return m, m.deleteTokenCmd(tokenID)
			case "esc":
				m.mode = userDetailNormal
				return m, nil
			}
			return m, nil
		}

		// Grant input mode.
		if m.mode == userDetailInputGrant {
			switch msg.String() {
			case "esc":
				m.mode = userDetailNormal
				m.clearGrantForm()
				return m, nil
			case "enter":
				if m.grantFocus < 1 {
					m.grantInputs[m.grantFocus].Blur()
					m.grantFocus++
					m.grantInputs[m.grantFocus].Focus()
					return m, textinput.Blink
				}
				// Submit.
				path := strings.TrimSpace(m.grantInputs[0].Value())
				role := strings.TrimSpace(m.grantInputs[1].Value())
				m.mode = userDetailNormal
				m.clearGrantForm()
				if path == "" || role == "" {
					m.statusMsg = "Path and Role are required"
					m.statusErr = true
					return m, nil
				}
				return m, m.grantCmd(path, role)
			default:
				var cmd tea.Cmd
				m.grantInputs[m.grantFocus], cmd = m.grantInputs[m.grantFocus].Update(msg)
				return m, cmd
			}
		}

		// Confirm-revoke mode.
		if m.mode == userDetailConfirmRevoke {
			switch msg.String() {
			case "enter":
				m.mode = userDetailNormal
				if len(m.acls) == 0 {
					return m, nil
				}
				cursor := m.aclsTable.Cursor()
				if cursor < 0 || cursor >= len(m.acls) {
					return m, nil
				}
				acl := m.acls[cursor]
				return m, m.revokeCmd(acl.Path, acl.RoleID)
			case "esc":
				m.mode = userDetailNormal
				return m, nil
			}
			return m, nil
		}

		// Normal mode.
		switch msg.String() {
		case "tab":
			m.activeTab = 1 - m.activeTab // toggle between 0 and 1
			return m, nil
		case "ctrl+r":
			m.loading = true
			m.loadErr = nil
			m.statusMsg = ""
			m.statusErr = false
			return m, tea.Batch(m.loadCmd(), m.spinner.Tick)
		case "t":
			if m.activeTab == 0 {
				m.mode = userDetailInputToken
				m.tokenInput.Focus()
				return m, textinput.Blink
			}
		case "d":
			if m.activeTab == 0 && len(m.tokens) > 0 {
				m.mode = userDetailConfirmDeleteToken
				return m, nil
			}
		case "r":
			if m.activeTab == 1 && len(m.acls) > 0 {
				m.mode = userDetailConfirmRevoke
				return m, nil
			}
		case "g":
			if m.activeTab == 1 {
				m.mode = userDetailInputGrant
				m.grantFocus = 0
				m.grantInputs[0].Focus()
				return m, textinput.Blink
			}
		}

		// Delegate navigation to the active table.
		if m.activeTab == 0 {
			var cmd tea.Cmd
			m.tokensTable, cmd = m.tokensTable.Update(msg)
			return m, cmd
		}
		var cmd tea.Cmd
		m.aclsTable, cmd = m.aclsTable.Update(msg)
		return m, cmd
	}

	// Delegate non-key messages to the active table.
	if m.activeTab == 0 {
		var cmd tea.Cmd
		m.tokensTable, cmd = m.tokensTable.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.aclsTable, cmd = m.aclsTable.Update(msg)
	return m, cmd
}

func (m *userDetailModel) clearGrantForm() {
	for i := range m.grantInputs {
		m.grantInputs[i].Reset()
		m.grantInputs[i].Blur()
	}
	m.grantFocus = 0
}

func (m userDetailModel) view() string {
	if m.width == 0 {
		return ""
	}

	info := m.info
	fullName := strings.TrimSpace(info.Firstname + " " + info.Lastname)
	titleStr := fmt.Sprintf("User: %s", info.UserID)
	if fullName != "" {
		titleStr += fmt.Sprintf("  (%s)", fullName)
	}
	if info.Email != "" {
		titleStr += fmt.Sprintf("  <%s>", info.Email)
	}
	title := StyleTitle.Render(titleStr)

	// Token show mode — full-screen overlay.
	if m.mode == userDetailShowToken {
		lines := []string{
			title,
			"",
			StyleSuccess.Render("✓ API token created successfully"),
			"",
			StyleWarning.Render("Full Token ID:  ") + info.UserID + "!" + m.newTokenID,
			StyleWarning.Render("Token value:    ") + StyleSuccess.Render(m.newTokenValue),
			"",
			StyleError.Render("IMPORTANT: This value is shown only once. Save it now!"),
			"",
			renderHelp("[Enter] / [Esc] dismiss"),
		}
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
	}

	// Build tab bar.
	tabs := []string{"Tokens", "ACLs"}
	var tabBar strings.Builder
	for i, t := range tabs {
		if i == m.activeTab {
			tabBar.WriteString(StyleTitle.Render("[ " + t + " ]"))
		} else {
			tabBar.WriteString(StyleDim.Render("  " + t + "  "))
		}
		if i < len(tabs)-1 {
			tabBar.WriteString("  ")
		}
	}

	var lines []string
	lines = append(lines, headerLine(title, m.width, m.lastRefreshed))
	lines = append(lines, tabBar.String())
	lines = append(lines, "")

	if m.loading {
		lines = append(lines, StyleWarning.Render(m.spinner.View()+" Loading..."))
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
	}

	if m.loadErr != nil {
		lines = append(lines, StyleError.Render("Error: "+m.loadErr.Error()))
		lines = append(lines, renderHelp("[ctrl+r] retry"))
		lines = append(lines, renderHelp("[Esc] back   [Q] quit"))
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
	}

	// Content based on active tab.
	if m.activeTab == 0 {
		// Tokens tab.
		tokenCount := StyleDim.Render(fmt.Sprintf("Tokens (%d)", len(m.tokens)))
		lines = append(lines, tokenCount)
		if len(m.tokens) == 0 {
			lines = append(lines, StyleDim.Render("  No API tokens"))
		} else {
			lines = append(lines, m.tokensTable.View())
		}

		// Mode overlays for tokens.
		switch m.mode {
		case userDetailInputToken:
			lines = append(lines, "")
			lines = append(lines, StyleWarning.Render("New token name: ")+m.tokenInput.View())
			lines = append(lines, renderHelp("[Enter] create   [Esc] cancel"))
		case userDetailConfirmDeleteToken:
			cursor := m.tokensTable.Cursor()
			name := ""
			if cursor >= 0 && cursor < len(m.tokens) {
				name = m.tokens[cursor].TokenID
			}
			lines = append(lines, "")
			lines = append(lines, StyleWarning.Render(
				fmt.Sprintf("Delete token %q? [Enter] confirm   [Esc] cancel", name),
			))
		default:
			lines = append(lines, m.statusLine())
			lines = append(lines, renderHelp("[t] new token   [d] delete  |  [Tab] ACLs  |  [ctrl+r] refresh"))
			lines = append(lines, renderHelp("[Esc] back   [Q] quit"))
		}
	} else {
		// ACLs tab.
		aclCount := StyleDim.Render(fmt.Sprintf("ACLs (%d)", len(m.acls)))
		lines = append(lines, aclCount)
		if len(m.acls) == 0 {
			lines = append(lines, StyleDim.Render("  No ACLs for this user"))
		} else {
			lines = append(lines, m.aclsTable.View())
		}

		// Show available roles as a hint (3 per line).
		if len(m.roles) > 0 {
			lines = append(lines, StyleDim.Render("Available roles:"))
			for i := 0; i < len(m.roles); i += 3 {
				end := i + 3
				if end > len(m.roles) {
					end = len(m.roles)
				}
				lines = append(lines, StyleDim.Render("  "+strings.Join(m.roles[i:end], ", ")))
			}
		}

		// Mode overlays for ACLs.
		switch m.mode {
		case userDetailInputGrant:
			labels := []string{"Path:", "Role:"}
			lines = append(lines, "")
			lines = append(lines, StyleTitle.Render("Grant Access"))
			for i, inp := range m.grantInputs {
				label := fmt.Sprintf("  %-8s", labels[i])
				if i == m.grantFocus {
					lines = append(lines, StyleWarning.Render(label)+inp.View())
				} else {
					lines = append(lines, StyleDim.Render(label)+inp.View())
				}
			}
			lines = append(lines, renderHelp("[Enter] next/save   [Esc] cancel"))
		case userDetailConfirmRevoke:
			cursor := m.aclsTable.Cursor()
			var path, role string
			if cursor >= 0 && cursor < len(m.acls) {
				path = m.acls[cursor].Path
				role = m.acls[cursor].RoleID
			}
			lines = append(lines, "")
			lines = append(lines, StyleWarning.Render(
				fmt.Sprintf("Revoke role %q on %q? [Enter] confirm   [Esc] cancel", role, path),
			))
		default:
			lines = append(lines, m.statusLine())
			lines = append(lines, renderHelp("[g] grant   [r] revoke  |  [Tab] Tokens  |  [ctrl+r] refresh"))
			lines = append(lines, renderHelp("[Esc] back   [Q] quit"))
		}
	}

	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (m userDetailModel) statusLine() string {
	if m.statusMsg != "" && m.statusErr {
		return StyleError.Render(m.statusMsg)
	}
	if m.statusMsg != "" {
		return StyleSuccess.Render(m.statusMsg)
	}
	return ""
}

func formatTokenExpire(expire int) string {
	if expire == 0 {
		return "never"
	}
	return time.Unix(int64(expire), 0).Format("2006-01-02")
}

func (m userDetailModel) createTokenCmd(tokenID string) tea.Cmd {
	c := m.client
	userid := m.info.UserID
	return func() tea.Msg {
		ctx := context.Background()
		tok := proxmox.Token{
			TokenID: tokenID,
			Privsep: proxmox.IntOrBool(false),
		}
		newTok, err := actions.CreateToken(ctx, c, userid, tok)
		if err != nil {
			return userDetailActionMsg{err: err}
		}
		return userDetailActionMsg{
			message:       fmt.Sprintf("Token %q created", tokenID),
			newTokenID:    tokenID,
			newTokenValue: newTok.Value,
			reload:        true,
		}
	}
}

func (m userDetailModel) deleteTokenCmd(tokenID string) tea.Cmd {
	c := m.client
	userid := m.info.UserID
	return func() tea.Msg {
		ctx := context.Background()
		if err := actions.DeleteToken(ctx, c, userid, tokenID); err != nil {
			return userDetailActionMsg{err: err}
		}
		return userDetailActionMsg{message: fmt.Sprintf("Token %q deleted", tokenID), reload: true}
	}
}

func (m userDetailModel) grantCmd(path, role string) tea.Cmd {
	c := m.client
	userid := m.info.UserID
	return func() tea.Msg {
		ctx := context.Background()
		if err := actions.GrantAccessByPath(ctx, c, userid, path, role, false); err != nil {
			return userDetailActionMsg{err: err}
		}
		return userDetailActionMsg{
			message: fmt.Sprintf("Granted %q on %q", role, path),
			reload:  true,
		}
	}
}

func (m userDetailModel) revokeCmd(path, role string) tea.Cmd {
	c := m.client
	userid := m.info.UserID
	return func() tea.Msg {
		ctx := context.Background()
		if err := actions.RevokeAccessByPath(ctx, c, userid, path, role); err != nil {
			return userDetailActionMsg{err: err}
		}
		return userDetailActionMsg{
			message: fmt.Sprintf("Revoked %q on %q", role, path),
			reload:  true,
		}
	}
}
