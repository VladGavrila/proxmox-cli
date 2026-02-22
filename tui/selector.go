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

	"github.com/chupakbra/proxmox-cli/internal/client"
	"github.com/chupakbra/proxmox-cli/internal/config"
	"github.com/chupakbra/proxmox-cli/internal/discovery"
)

// selectorMode controls which overlay (if any) is active.
type selectorMode int

const (
	selectorNormal         selectorMode = iota
	selectorAdding                      // add-instance form is open
	selectorConfirmDel                  // delete-instance confirmation overlay
	selectorDiscoverInput               // typing a subnet to scan
	selectorDiscovering                 // scanning network
	selectorDiscoverResult              // showing discovered instances
)

// connectErrMsg is sent when connecting to a Proxmox instance fails.
type connectErrMsg struct{ err error }

type selectorModel struct {
	cfg        *config.Config
	instances  map[string]config.InstanceConfig
	current    string // name of the currently active instance
	table      table.Model
	spinner    spinner.Model
	connecting bool
	connectErr string

	// Overlay mode.
	mode selectorMode

	// Add-instance form: [0]=name [1]=url [2]=tokenID [3]=tokenSecret.
	addInputs [4]textinput.Model
	addFocus  int // which input is focused

	// Status feedback after add/remove.
	statusMsg string
	statusErr bool

	// Discovery.
	discoverInput textinput.Model // subnet input
	discovered    []discovery.Instance
	discoverTable table.Model

	// Shared cache of connected clients (owned by appModel).
	clientCache map[string]*proxmox.Client

	width  int
	height int
}

func newSelectorModel(cfg *config.Config, clientCache map[string]*proxmox.Client) selectorModel {
	s := spinner.New()
	s.Spinner = CLISpinner
	s.Style = StyleSpinner

	placeholders := [4]string{
		"e.g. home-lab",
		"https://192.168.1.10:8006",
		"user@pve!mytoken",
		"token secret",
	}
	var inputs [4]textinput.Model
	for i := range inputs {
		ti := textinput.New()
		ti.Placeholder = placeholders[i]
		ti.CharLimit = 120
		if i == 3 {
			ti.EchoMode = textinput.EchoPassword
			ti.EchoCharacter = '•'
		}
		inputs[i] = ti
	}

	di := textinput.New()
	di.Placeholder = "e.g. 172.20.20 (empty = local subnets)"
	di.CharLimit = 60

	m := selectorModel{
		cfg:           cfg,
		instances:     cfg.Instances,
		current:       cfg.CurrentInstance,
		spinner:       s,
		addInputs:     inputs,
		discoverInput: di,
		clientCache:   clientCache,
	}
	m.table = m.buildTable()
	return m
}

func (m selectorModel) buildTable() table.Model {
	nameWidth := 20
	urlWidth := 40
	defWidth := 9

	if m.width > 0 {
		remaining := m.width - urlWidth - defWidth - 10
		if remaining > nameWidth {
			nameWidth = remaining
		}
	}

	cols := []table.Column{
		{Title: "NAME", Width: nameWidth},
		{Title: "URL", Width: urlWidth},
		{Title: "DEFAULT", Width: defWidth},
	}

	var names []string
	for name := range m.instances {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]table.Row, len(names))
	for i, name := range names {
		inst := m.instances[name]
		def := ""
		if name == m.current {
			def = "✓"
		}
		rows[i] = table.Row{name, inst.URL, def}
	}

	tableHeight := 10
	if m.height > 0 {
		tableHeight = m.height - 10
		if tableHeight < 3 {
			tableHeight = 3
		}
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
		Background(lipgloss.Color("62")).
		Bold(true)
	t.SetStyles(s)
	return t
}

func (m selectorModel) buildDiscoverTable() table.Model {
	ipWidth := 18
	urlWidth := 35

	if m.width > 0 {
		remaining := m.width - ipWidth - 10
		if remaining > urlWidth {
			urlWidth = remaining
		}
	}

	cols := []table.Column{
		{Title: "IP", Width: ipWidth},
		{Title: "URL", Width: urlWidth},
	}

	rows := make([]table.Row, len(m.discovered))
	for i, d := range m.discovered {
		rows[i] = table.Row{d.IP, d.URL}
	}

	tableHeight := 10
	if m.height > 0 {
		tableHeight = m.height - 12
		if tableHeight < 3 {
			tableHeight = 3
		}
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
		Background(lipgloss.Color("62")).
		Bold(true)
	t.SetStyles(s)
	return t
}

func (m selectorModel) init() tea.Cmd {
	return nil
}

func (m selectorModel) update(msg tea.Msg) (selectorModel, tea.Cmd) {
	switch msg := msg.(type) {
	case connectErrMsg:
		m.connecting = false
		m.connectErr = msg.err.Error()
		return m, nil

	case discoveryDoneMsg:
		if m.mode != selectorDiscovering {
			return m, nil
		}
		if msg.err != nil {
			m.mode = selectorNormal
			m.statusMsg = fmt.Sprintf("Discovery failed: %s", msg.err)
			m.statusErr = true
			return m, nil
		}
		scanned := strings.Join(msg.result.Subnets, ", ")
		if len(msg.result.Instances) == 0 {
			m.mode = selectorNormal
			m.statusMsg = fmt.Sprintf("No Proxmox instances found on %s", scanned)
			m.statusErr = false
			return m, nil
		}
		m.discovered = msg.result.Instances
		m.discoverTable = m.buildDiscoverTable()
		m.mode = selectorDiscoverResult
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.connecting || m.mode == selectorDiscovering {
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		if m.connecting {
			return m, nil
		}

		// Discover subnet input mode.
		if m.mode == selectorDiscoverInput {
			switch msg.String() {
			case "esc":
				m.mode = selectorNormal
				m.discoverInput.Blur()
				return m, nil
			case "enter":
				raw := strings.TrimSpace(m.discoverInput.Value())
				m.discoverInput.Blur()
				var subnets []string
				if raw != "" {
					// Support space/comma separated multiple subnets.
					for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
						return r == ',' || r == ' '
					}) {
						part = strings.TrimSpace(part)
						if part == "" {
							continue
						}
						cidr, err := discovery.NormalizeSubnet(part)
						if err != nil {
							m.mode = selectorNormal
							m.statusMsg = fmt.Sprintf("Invalid subnet: %s", err)
							m.statusErr = true
							return m, nil
						}
						subnets = append(subnets, cidr)
					}
				}
				m.mode = selectorDiscovering
				return m, tea.Batch(
					discoverInstancesCmd(subnets),
					m.spinner.Tick,
				)
			default:
				var cmd tea.Cmd
				m.discoverInput, cmd = m.discoverInput.Update(msg)
				return m, cmd
			}
		}

		// Discovering mode — only allow Esc to cancel.
		if m.mode == selectorDiscovering {
			if msg.String() == "esc" {
				m.mode = selectorNormal
				return m, nil
			}
			return m, nil
		}

		// Discover results mode.
		if m.mode == selectorDiscoverResult {
			switch msg.String() {
			case "enter":
				row := m.discoverTable.SelectedRow()
				if len(row) == 0 {
					return m, nil
				}
				// Pre-populate the add form with discovered URL.
				m.mode = selectorAdding
				m.addFocus = 0
				m.addInputs[0].Focus()
				m.addInputs[1].SetValue(row[1]) // URL column
				return m, textinput.Blink
			case "esc":
				m.mode = selectorNormal
				m.discovered = nil
				return m, nil
			default:
				var cmd tea.Cmd
				m.discoverTable, cmd = m.discoverTable.Update(msg)
				return m, cmd
			}
		}

		// Add-instance form mode.
		if m.mode == selectorAdding {
			switch msg.String() {
			case "esc":
				m.mode = selectorNormal
				m.clearAddForm()
				return m, nil
			case "enter":
				if m.addFocus < 3 {
					// Advance to next field.
					m.addInputs[m.addFocus].Blur()
					m.addFocus++
					m.addInputs[m.addFocus].Focus()
					return m, textinput.Blink
				}
				// Submit on last field.
				name := strings.TrimSpace(m.addInputs[0].Value())
				url := strings.TrimSpace(m.addInputs[1].Value())
				tokenID := strings.TrimSpace(m.addInputs[2].Value())
				tokenSecret := strings.TrimSpace(m.addInputs[3].Value())
				m.mode = selectorNormal
				m.clearAddForm()
				if name == "" || url == "" {
					m.statusMsg = "Name and URL are required"
					m.statusErr = true
					return m, nil
				}
				if _, exists := m.instances[name]; exists {
					m.statusMsg = fmt.Sprintf("Instance %q already exists", name)
					m.statusErr = true
					return m, nil
				}
				m.instances[name] = config.InstanceConfig{
					URL:         url,
					TokenID:     tokenID,
					TokenSecret: tokenSecret,
				}
				m.cfg.Instances = m.instances
				_ = config.Save(m.cfg)
				m.statusMsg = fmt.Sprintf("Instance %q added", name)
				m.statusErr = false
				m.table = m.buildTable()
				return m, nil
			default:
				var cmd tea.Cmd
				m.addInputs[m.addFocus], cmd = m.addInputs[m.addFocus].Update(msg)
				return m, cmd
			}
		}

		// Delete confirmation mode.
		if m.mode == selectorConfirmDel {
			switch msg.String() {
			case "enter":
				row := m.table.SelectedRow()
				m.mode = selectorNormal
				if len(row) == 0 {
					return m, nil
				}
				name := row[0]
				delete(m.instances, name)
				if m.current == name {
					m.current = ""
					m.cfg.CurrentInstance = ""
				}
				m.cfg.Instances = m.instances
				_ = config.Save(m.cfg)
				m.statusMsg = fmt.Sprintf("Instance %q removed", name)
				m.statusErr = false
				m.table = m.buildTable()
				return m, nil
			case "esc":
				m.mode = selectorNormal
				return m, nil
			}
			return m, nil
		}

		// Normal mode.
		switch msg.String() {
		case "enter":
			rows := m.table.Rows()
			if len(rows) == 0 {
				return m, nil
			}
			row := m.table.SelectedRow()
			if len(row) == 0 {
				return m, nil
			}
			name := row[0]

			m.connecting = true
			m.connectErr = ""
			m.statusMsg = ""

			// Reuse cached client if available (skip client creation).
			if c, ok := m.clientCache[name]; ok {
				return m, tea.Batch(
					reconnectToInstance(m.cfg, c, name),
					m.spinner.Tick,
				)
			}

			return m, tea.Batch(
				connectToInstance(m.cfg, m.instances, name),
				m.spinner.Tick,
			)
		case "a":
			m.mode = selectorAdding
			m.addFocus = 0
			m.addInputs[0].Focus()
			return m, textinput.Blink
		case "d":
			m.mode = selectorDiscoverInput
			m.discoverInput.Reset()
			m.discoverInput.Focus()
			m.statusMsg = ""
			m.connectErr = ""
			return m, textinput.Blink
		case "R":
			if len(m.table.Rows()) == 0 {
				return m, nil
			}
			m.mode = selectorConfirmDel
			return m, nil
		case "esc":
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *selectorModel) clearAddForm() {
	for i := range m.addInputs {
		m.addInputs[i].Reset()
		m.addInputs[i].Blur()
	}
	m.addFocus = 0
}

func (m selectorModel) view() string {
	if m.width == 0 {
		return ""
	}

	title := StyleTitle.Render("Proxmox Instances")

	// Add-instance form overlay.
	if m.mode == selectorAdding {
		labels := []string{"Name:", "URL:", "Token ID:", "Token Secret:"}
		lines := []string{title, "", StyleTitle.Render("Add Instance"), ""}
		for i, inp := range m.addInputs {
			label := fmt.Sprintf("  %-14s", labels[i])
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

	// Discover subnet input overlay.
	if m.mode == selectorDiscoverInput {
		lines := []string{title, "", StyleTitle.Render("Discover Instances"), ""}
		label := StyleWarning.Render("  Subnet:       ")
		lines = append(lines, label+m.discoverInput.View())
		lines = append(lines, "")
		lines = append(lines, StyleHelp.Render("[Enter] scan (empty = local subnets)   [Esc] cancel"))
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
	}

	// Discovery scanning overlay.
	if m.mode == selectorDiscovering {
		lines := []string{title, "", StyleWarning.Render(m.spinner.View() + " Scanning network for Proxmox instances...")}
		lines = append(lines, "", StyleHelp.Render("[Esc] cancel"))
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
	}

	// Discovery results overlay.
	if m.mode == selectorDiscoverResult {
		lines := []string{title, "", StyleTitle.Render("Discovered Instances"), ""}
		lines = append(lines, m.discoverTable.View())
		lines = append(lines, "", StyleHelp.Render("[Enter] add   [Esc] back"))
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
	}

	if len(m.instances) == 0 {
		notice := StyleDim.Render("No instances configured. Press 'a' to add one, or 'd' to discover.")
		lines := []string{title, "", notice, ""}
		if m.statusMsg != "" && m.statusErr {
			lines = append(lines, StyleError.Render(m.statusMsg))
		} else if m.statusMsg != "" {
			lines = append(lines, StyleSuccess.Render(m.statusMsg))
		} else {
			lines = append(lines, "")
		}
		lines = append(lines, StyleHelp.Render("[a] add   [d] discover   [Q] quit"))
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
	}

	var lines []string
	lines = append(lines, title)
	lines = append(lines, "")
	lines = append(lines, m.table.View())
	lines = append(lines, "")

	// Confirmation overlay.
	if m.mode == selectorConfirmDel {
		row := m.table.SelectedRow()
		name := ""
		if len(row) > 0 {
			name = row[0]
		}
		lines = append(lines, StyleWarning.Render(
			fmt.Sprintf("Remove instance %q? [Enter] confirm   [Esc] cancel", name),
		))
		return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
	}

	// Status / spinner / error.
	if m.connecting {
		lines = append(lines, StyleWarning.Render(m.spinner.View()+" Connecting..."))
	} else if m.connectErr != "" {
		lines = append(lines, StyleError.Render("Error: "+m.connectErr))
	} else if m.statusMsg != "" && m.statusErr {
		lines = append(lines, StyleError.Render(m.statusMsg))
	} else if m.statusMsg != "" {
		lines = append(lines, StyleSuccess.Render(m.statusMsg))
	} else {
		lines = append(lines, "") // keep height stable
	}

	lines = append(lines, StyleHelp.Render("[Enter] connect  |  [a] add   [d] discover   [R] remove"))
	lines = append(lines, StyleHelp.Render("[Q] quit"))

	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
}

// connectToInstance returns a tea.Cmd that creates a new client for the named
// instance, verifies connectivity, saves it as the default, and emits
// instanceSelectedMsg on success.
func connectToInstance(cfg *config.Config, instances map[string]config.InstanceConfig, name string) tea.Cmd {
	inst := instances[name]
	return func() tea.Msg {
		c, err := client.New(&inst)
		if err != nil {
			return connectErrMsg{fmt.Errorf("%s: %w", name, err)}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := c.Version(ctx); err != nil {
			return connectErrMsg{fmt.Errorf("connecting to %q: %w", name, err)}
		}
		cfg.CurrentInstance = name
		_ = config.Save(cfg) // best-effort; ignore save errors
		return instanceSelectedMsg{client: c, name: name}
	}
}

// reconnectToInstance returns a tea.Cmd that reuses an existing client,
// verifies connectivity with a Version() call, and emits instanceSelectedMsg
// on success. If the health check fails the cached client is stale and the
// caller should fall back to a full connectToInstance.
func reconnectToInstance(cfg *config.Config, c *proxmox.Client, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := c.Version(ctx); err != nil {
			return connectErrMsg{fmt.Errorf("connecting to %q: %w", name, err)}
		}
		cfg.CurrentInstance = name
		_ = config.Save(cfg)
		return instanceSelectedMsg{client: c, name: name}
	}
}
