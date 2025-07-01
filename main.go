package main

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

var (
	// Styles
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("57")).
			Padding(0, 1)

	systemInfoStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("10"))

	processTableStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240"))
)

type tickMsg time.Time
type systemStats struct {
	uptime      time.Duration
	loadAvg     *load.AvgStat
	cpuPercent  []float64
	memStats    *mem.VirtualMemoryStat
	processes   []*process.Process
	processInfo []ProcessInfo
}

type ProcessInfo struct {
	PID     int32
	Name    string
	CPUPerc float64
	MemPerc float32
	Status  string
	User    string
}

type model struct {
	table      table.Model
	stats      systemStats
	sortBy     string
	ascending  bool
	lastUpdate time.Time
	err        error
}

func initialModel() model {
	columns := []table.Column{
		{Title: "PID", Width: 8},
		{Title: "USER", Width: 10},
		{Title: "CPU%", Width: 8},
		{Title: "MEM%", Width: 8},
		{Title: "STATUS", Width: 10},
		{Title: "COMMAND", Width: 30},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(15),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	return model{
		table:     t,
		sortBy:    "cpu",
		ascending: false,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), updateStats())
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func updateStats() tea.Cmd {
	return func() tea.Msg {
		stats := systemStats{}

		// Get uptime
		if hostInfo, err := host.Info(); err == nil {
			stats.uptime = time.Duration(hostInfo.Uptime) * time.Second
		}

		// Get load average
		if loadStats, err := load.Avg(); err == nil {
			stats.loadAvg = loadStats
		}

		// Get CPU usage
		if cpuPercs, err := cpu.Percent(0, true); err == nil {
			stats.cpuPercent = cpuPercs
		}

		// Get memory stats
		if memStats, err := mem.VirtualMemory(); err == nil {
			stats.memStats = memStats
		}

		// Get processes
		if processes, err := process.Processes(); err == nil {
			stats.processes = processes
			stats.processInfo = getProcessInfo(processes)
		}

		return stats
	}
}

func getProcessInfo(processes []*process.Process) []ProcessInfo {
	var processInfo []ProcessInfo

	for _, p := range processes {
		if p == nil {
			continue
		}

		name, _ := p.Name()
		if name == "" {
			continue
		}

		cpuPerc, _ := p.CPUPercent()
		memPerc, _ := p.MemoryPercent()
		status, _ := p.Status()
		username, _ := p.Username()

		info := ProcessInfo{
			PID:     p.Pid,
			Name:    name,
			CPUPerc: cpuPerc,
			MemPerc: memPerc,
			Status:  status,
			User:    username,
		}

		// Limit username length
		if len(info.User) > 8 {
			info.User = info.User[:8]
		}

		processInfo = append(processInfo, info)
	}

	return processInfo
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "c":
			m.sortBy = "cpu"
			m.ascending = !m.ascending
		case "m":
			m.sortBy = "memory"
			m.ascending = !m.ascending
		case "p":
			m.sortBy = "pid"
			m.ascending = !m.ascending
		case "n":
			m.sortBy = "name"
			m.ascending = !m.ascending
		}

	case tickMsg:
		m.lastUpdate = time.Time(msg)
		return m, tea.Batch(tickCmd(), updateStats())

	case systemStats:
		m.stats = msg
		m.updateTable()

	case tea.WindowSizeMsg:
		m.table.SetWidth(msg.Width - 4)
		m.table.SetHeight(msg.Height - 12)
	}

	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *model) updateTable() {
	// Sort processes
	sort.Slice(m.stats.processInfo, func(i, j int) bool {
		switch m.sortBy {
		case "cpu":
			if m.ascending {
				return m.stats.processInfo[i].CPUPerc < m.stats.processInfo[j].CPUPerc
			}
			return m.stats.processInfo[i].CPUPerc > m.stats.processInfo[j].CPUPerc
		case "memory":
			if m.ascending {
				return m.stats.processInfo[i].MemPerc < m.stats.processInfo[j].MemPerc
			}
			return m.stats.processInfo[i].MemPerc > m.stats.processInfo[j].MemPerc
		case "pid":
			if m.ascending {
				return m.stats.processInfo[i].PID < m.stats.processInfo[j].PID
			}
			return m.stats.processInfo[i].PID > m.stats.processInfo[j].PID
		case "name":
			if m.ascending {
				return m.stats.processInfo[i].Name < m.stats.processInfo[j].Name
			}
			return m.stats.processInfo[i].Name > m.stats.processInfo[j].Name
		}
		return false
	})

	// Convert to table rows
	var rows []table.Row
	for _, proc := range m.stats.processInfo {
		if len(rows) >= 50 { // Limit to top 50 processes
			break
		}

		// Truncate command name if too long
		command := proc.Name
		if len(command) > 28 {
			command = command[:28] + ".."
		}

		rows = append(rows, table.Row{
			strconv.Itoa(int(proc.PID)),
			proc.User,
			fmt.Sprintf("%.1f", proc.CPUPerc),
			fmt.Sprintf("%.1f", proc.MemPerc),
			proc.Status,
			command,
		})
	}

	m.table.SetRows(rows)
}

func (m model) View() string {
	var b strings.Builder

	// Header
	header := headerStyle.Render("GoTop - System Monitor")
	b.WriteString(header + "\n\n")

	// System info
	if m.stats.uptime > 0 {
		uptime := formatDuration(m.stats.uptime)
		b.WriteString(systemInfoStyle.Render(fmt.Sprintf("Uptime: %s", uptime)))
		b.WriteString("  ")
	}

	if m.stats.loadAvg != nil {
		b.WriteString(systemInfoStyle.Render(fmt.Sprintf("Load: %.2f %.2f %.2f", 
			m.stats.loadAvg.Load1, m.stats.loadAvg.Load5, m.stats.loadAvg.Load15)))
		b.WriteString("  ")
	}

	b.WriteString(systemInfoStyle.Render(fmt.Sprintf("CPUs: %d", runtime.NumCPU())))
	b.WriteString("\n")

	// CPU usage
	if len(m.stats.cpuPercent) > 0 {
		b.WriteString(systemInfoStyle.Render("CPU: "))
		for i, usage := range m.stats.cpuPercent {
			if i > 0 {
				b.WriteString(" ")
			}
			b.WriteString(fmt.Sprintf("%.1f%%", usage))
			if i >= 7 { // Limit to first 8 cores for display
				if len(m.stats.cpuPercent) > 8 {
					b.WriteString(fmt.Sprintf(" (+%d more)", len(m.stats.cpuPercent)-8))
				}
				break
			}
		}
		b.WriteString("\n")
	}

	// Memory usage
	if m.stats.memStats != nil {
		memUsed := float64(m.stats.memStats.Used) / (1024 * 1024 * 1024)
		memTotal := float64(m.stats.memStats.Total) / (1024 * 1024 * 1024)
		b.WriteString(systemInfoStyle.Render(fmt.Sprintf("Memory: %.1fG/%.1fG (%.1f%%)", 
			memUsed, memTotal, m.stats.memStats.UsedPercent)))
		b.WriteString("\n\n")
	}

	// Sort indicator
	sortIndicator := fmt.Sprintf("Sorted by: %s (%s)", m.sortBy, 
		map[bool]string{true: "ascending", false: "descending"}[m.ascending])
	b.WriteString(sortIndicator + "\n\n")

	// Process table
	b.WriteString(processTableStyle.Render(m.table.View()))
	b.WriteString("\n\n")

	// Help
	help := "Controls: [c] CPU sort • [m] Memory sort • [p] PID sort • [n] Name sort • [q] Quit"
	b.WriteString(lipgloss.NewStyle().Faint(true).Render(help))

	return b.String()
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	} else if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v", err)
		os.Exit(1)
	}
}
