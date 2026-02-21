package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	proxmox "github.com/luthermonson/go-proxmox"
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
	width         int
	height        int
}

func newListModel(c *proxmox.Client, instName string, w, h int) listModel {
	s := spinner.New()
	s.Spinner = CLISpinner
	s.Style = StyleSpinner

	return listModel{
		client:   c,
		instName: instName,
		loading:  true,
		spinner:  s,
		fetchID:  time.Now().UnixNano(),
		width:    w,
		height:   h,
	}
}

// fixedColWidth is the total width of all columns except NAME.
// VMID(6) + TYPE(4) + TMPL(5) + NODE(12) + STATUS(10) + CPU(7) + MEM(10) + DISK(10) = 64
// Plus separators/padding ~ 10.
const fixedColWidth = 64 + 10

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

	tableHeight := m.height - 8 // padding(2) + title(1) + blank(1) + help(2) + table border(2)
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
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.resources = msg.resources
		m.lastRefreshed = time.Now()
		m = m.withRebuiltTable()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.loading {
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
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
		case "ctrl+r":
			m.loading = true
			m.err = nil
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
		StyleHelp.Render("[Tab] users  |  [ctrl+r] refresh"),
		StyleHelp.Render("[Esc] back   [Q] quit"),
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(strings.Join(lines, "\n"))
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
