package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/maccavelli/mcp-server-socratic-thinker/internal/telemetry"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
)

var dashboardCmd = &cobra.Command{
	Use:     "dashboard",
	Aliases: []string{"dash"},
	Short:   "View the telemetry dashboard",
	Run: func(cmd *cobra.Command, args []string) {
		m := initialModel()

		// Create program
		p := tea.NewProgram(m, tea.WithAltScreen())

		// One-shot LLM backplane probe for initial state
		go func() {
			configured, active := probeLLMBackplane()
			p.Send(llmProbeMsg{configured: configured, active: active})
		}()

		// Start persistent UDP client goroutine with auto-reconnect
		go func() {
			conn, boundPort := sweepPorts()
			if conn == nil {
				slog.Warn("could not connect to any telemetry port; will retry")
			} else {
				p.Send(reconnectMsg{port: boundPort})
			}

			buf := make([]byte, 4096)
			pingTicker := time.NewTicker(telemetry.EmissionInterval)
			defer pingTicker.Stop()

			const maxConsecutiveFailures = 6
			consecutiveFailures := 0
			backoff := 2 * time.Second
			const maxBackoff = 10 * time.Second

			for range pingTicker.C {
				if conn == nil {
					// Attempt reconnect with backoff
					time.Sleep(backoff)
					conn, boundPort = sweepPorts()
					if conn != nil {
						consecutiveFailures = 0
						backoff = 2 * time.Second
						p.Send(reconnectMsg{port: boundPort})
						slog.Info("telemetry reconnected", "port", boundPort)
					} else {
						backoff = min(backoff*2, maxBackoff)
					}
					continue
				}

				// Send ping
				_, err := conn.Write([]byte{0x01})
				if err != nil {
					if isClosedErr(err) {
						return
					}
					consecutiveFailures++
					if consecutiveFailures >= maxConsecutiveFailures {
						slog.Warn("telemetry connection lost, initiating re-sweep",
							"failures", consecutiveFailures)
						_ = conn.Close() //nolint:errcheck // best-effort reconnect after repeated write failures
						conn = nil
					}
					continue
				}

				// Drain ALL pending packets from the UDP receive buffer.
				// The server sends two packets per tick (ACK from listener +
				// payload from emission goroutine). Reading only one per tick
				// causes buffer saturation where stale packets accumulate
				// and new packets get dropped by the kernel.
				var latestPayload *telemetry.MetricPayload
				gotAny := false

				// First read: use a generous deadline to wait for the ping response.
				if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
					consecutiveFailures++
					continue
				}
				for {
					n, err := conn.Read(buf)
					if err != nil {
						if isClosedErr(err) {
							return
						}
						if !gotAny {
							// No packets at all — count as a failure.
							consecutiveFailures++
							if consecutiveFailures >= maxConsecutiveFailures {
								slog.Warn("telemetry connection lost, initiating re-sweep",
									"failures", consecutiveFailures)
								_ = conn.Close() //nolint:errcheck // best-effort reconnect after repeated read failures
								conn = nil
							}
						}
						break // buffer drained or timeout
					}

					gotAny = true
					var payload telemetry.MetricPayload
					if json.Unmarshal(buf[:n], &payload) == nil && payload.PipelineStage != "ACK" {
						latestPayload = &payload
					}

					// Subsequent reads: use a very short deadline to drain
					// remaining buffered packets without blocking.
					if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
						break
					}
				}

				if gotAny {
					consecutiveFailures = 0
				}
				if latestPayload != nil {
					p.Send(sessionMsg(*latestPayload))
				}
			}
		}()

		// Start self-polling goroutine for system metrics
		go func() {
			startTime := time.Now()
			for {
				var memStats runtime.MemStats
				runtime.ReadMemStats(&memStats)
				p.Send(systemMsg{
					UptimeSeconds:    int64(time.Since(startTime).Seconds()),
					MemoryAllocBytes: memStats.Alloc,
					ActiveGoroutines: runtime.NumGoroutine(),
					GCPauseNs:        memStats.PauseTotalNs,
					HeapObjects:      memStats.HeapObjects,
					SysMemory:        memStats.Sys,
				})
				time.Sleep(1 * time.Second)
			}
		}()

		// Run blocks until user quits
		if _, err := p.Run(); err != nil {
			fmt.Printf("Error running dashboard: %v\n", err)
			os.Exit(1)
		}
	},
}

// isClosedErr checks if the error indicates a closed socket.
func isClosedErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "use of closed")
}

// reconnectMsg notifies the BubbleTea program of a port change.
type reconnectMsg struct {
	port int
}

// llmProbeMsg carries the result of the one-shot LLM backplane probe at dashboard startup.
type llmProbeMsg struct {
	configured bool
	active     bool
}

// probeLLMBackplane checks the LLM env vars and fires a one-shot HTTP probe
// to determine initial LLM Configured and LLM Status values.
// This avoids importing internal/llm — the dashboard only needs a one-shot check.
func probeLLMBackplane() (configured, active bool) {
	if os.Getenv("MCP_LLM_ENABLED") != "true" {
		return false, false
	}
	addr := os.Getenv("MCP_LLM_ADDR")
	token := os.Getenv("MCP_LLM_TOKEN")
	if addr == "" || token == "" {
		return false, false
	}
	configured = true

	const maxAttempts = 3
	const retryDelay = 5 * time.Second

	client := &http.Client{Timeout: 3 * time.Second}

	for attempt := range maxAttempts {
		body, err := json.Marshal(map[string]any{
			"prompt":      "Hi",
			"max_tokens":  10,
			"server_name": "socratic-thinker-dashboard",
		})
		if err != nil {
			return configured, false
		}
		//nolint:gosec // G704: addr is orchestrator-injected MCP_LLM_ADDR, not user input
		req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/llm/generate", bytes.NewReader(body))
		if err != nil {
			return configured, false
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)

		//nolint:gosec // G704: probe target is orchestrator-injected MCP_LLM_ADDR
		resp, err := client.Do(req)
		if err == nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				slog.Debug("llm probe response close failed", "error", closeErr)
			}
			if resp.StatusCode == http.StatusOK {
				return configured, true
			}
		}

		// Sleep before next retry (but not after the last attempt).
		if attempt < maxAttempts-1 {
			time.Sleep(retryDelay)
		}
	}

	return configured, false
}

// dialAndValidate connects to a port and verifies the server responds.
func dialAndValidate(port int) *net.UDPConn {
	addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}
	c, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil
	}
	if _, writeErr := c.Write([]byte{0x01}); writeErr != nil {
		_ = c.Close() //nolint:errcheck // probe cleanup after failed write
		return nil
	}
	if err := c.SetReadDeadline(time.Now().Add(1 * time.Second)); err != nil {
		_ = c.Close() //nolint:errcheck // probe cleanup after deadline failure
		return nil
	}
	buf := make([]byte, 4096)
	_, err = c.Read(buf)
	if err != nil {
		_ = c.Close() //nolint:errcheck // probe cleanup after failed read
		return nil
	}
	return c
}

// sweepPorts attempts to connect to the first responding telemetry port.
func sweepPorts() (*net.UDPConn, int) {
	for _, port := range telemetry.GetTelemetryPorts() {
		if c := dialAndValidate(port); c != nil {
			return c, port
		}
	}
	return nil, 0
}

// Styling Variables matching MagicDev
var (
	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 2).
			Width(30)

	navItemStyle = lipgloss.NewStyle().
			Padding(0, 1)

	activeNavItemStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("62")).
				Foreground(lipgloss.Color("230")).
				Padding(0, 1).
				Bold(true)

	windowStyle = lipgloss.NewStyle().
			Padding(1, 4)

	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("62")).
			Bold(true).
			MarginBottom(1)

	subTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("62")).
			Bold(true).
			MarginTop(1).
			MarginBottom(1)

	metricLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))

	successStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warningStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	tableBorderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))

	// gridCardStyle enforces fixed widths for the 2×2 symmetrical dashboard grid.
	gridCardStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1).
			MarginRight(2).
			MarginBottom(1).
			Width(50)
)

// renderStyledTable builds a lipgloss table from headers and rows.
func renderStyledTable(headers []string, rows [][]string) string {
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(tableBorderStyle).
		Headers(headers...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if col == 0 {
				return lipgloss.NewStyle().Width(20)
			}
			if col == 1 {
				return lipgloss.NewStyle().Width(22)
			}
			return lipgloss.NewStyle()
		})

	for _, row := range rows {
		t.Row(row...)
	}

	return t.Render()
}

const (
	tabSummary = iota
	tabQuit
)

var navItems = []string{
	"Summary",
	"Quit",
}

// systemMsg carries self-polled system metrics from the dashboard process.
type systemMsg struct {
	UptimeSeconds    int64
	MemoryAllocBytes uint64
	ActiveGoroutines int
	GCPauseNs        uint64
	HeapObjects      uint64
	SysMemory        uint64
}

// sessionMsg carries UDP-received session metrics from the serve process.
type sessionMsg telemetry.MetricPayload

type model struct {
	activeTab int
	width     int
	height    int

	// System metrics (self-polled, always live)
	sysUptime      int64
	sysMemAlloc    uint64
	sysGoroutines  int
	sysGCPause     uint64
	sysHeapObjects uint64
	sysSysMem      uint64

	// Session metrics (UDP-fed from serve process)
	sessNetIn        int64
	sessNetOut       int64
	sessPipeline     string
	trifectaReviews  int
	sessContextBytes int
	sessTokensEst    int
	sessConnected    bool
	sessLastUpdate   time.Time

	// Enhanced intelligence metrics (UDP-fed from serve process)
	llmGateActivations int
	llmGateRejects     int
	llmGateLatencyMs   int64
	recallConnected    bool

	// LLM backplane state
	llmConfigured bool
	llmActive     bool

	// Historical 30-day metrics
	histNetBytesIn     int64
	histNetBytesOut    int64
	histStagesRunCount int
	histTrifectaCount  int
	histContextBytes   int
	histTokensEst      int

	// Dashboard metadata
	boundPort int
	err       error
}

func initialModel() model {
	return model{
		activeTab: tabSummary,
	}
}

// Init returns nil since background goroutines feed data via p.Send().
func (m model) Init() tea.Cmd {
	return nil
}

// Update handles all incoming messages.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "up", "k":
			m.activeTab--
			if m.activeTab < 0 {
				m.activeTab = len(navItems) - 1
			}
		case "down", "j":
			m.activeTab++
			if m.activeTab >= len(navItems) {
				m.activeTab = 0
			}
		case "enter":
			if m.activeTab == tabQuit {
				return m, tea.Quit
			}
		}
	case systemMsg:
		m.sysUptime = msg.UptimeSeconds
		m.sysMemAlloc = msg.MemoryAllocBytes
		m.sysGoroutines = msg.ActiveGoroutines
		m.sysGCPause = msg.GCPauseNs
		m.sysHeapObjects = msg.HeapObjects
		m.sysSysMem = msg.SysMemory
	case sessionMsg:
		m.sessNetIn = msg.NetworkBytesRead
		m.sessNetOut = msg.NetworkBytesWritten
		m.sessPipeline = msg.PipelineStage
		m.trifectaReviews = msg.TrifectaReviewCount
		m.sessContextBytes = msg.SessionContextBytes
		m.sessTokensEst = msg.SessionTokensEst
		m.sessConnected = true
		m.sessLastUpdate = time.Now()
		m.llmGateActivations = msg.LLMGateActivations
		m.llmGateRejects = msg.LLMGateRejects
		m.llmGateLatencyMs = msg.LLMGateLatencyMs
		m.recallConnected = msg.RecallConnected
		m.llmConfigured = msg.LLMConfigured
		m.llmActive = msg.LLMActive
		m.histNetBytesIn = msg.HistoryNetBytesIn
		m.histNetBytesOut = msg.HistoryNetBytesOut
		m.histStagesRunCount = msg.HistoryStagesRunCount
		m.histTrifectaCount = msg.HistoryTrifectaCount
		m.histContextBytes = msg.HistoryContextBytes
		m.histTokensEst = msg.HistoryTokensEst
	case llmProbeMsg:
		m.llmConfigured = msg.configured
		m.llmActive = msg.active
	case reconnectMsg:
		m.boundPort = msg.port
	}

	return m, nil
}

func renderSummary(m model) string {
	b := strings.Builder{}

	// Header in a box
	headerBox := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("238")).
		Padding(1, 4).
		Render(titleStyle.Render("Socratic Thinker Overview"))

	// Connection status
	connStatus := warningStyle.Render("○ Server Disconnected")
	if m.sessConnected && time.Since(m.sessLastUpdate) < 10*time.Second {
		connStatus = successStyle.Render("● Server Connected")
	}
	if m.boundPort > 0 {
		connStatus += metricLabelStyle.Render("  (udp:" + strconv.Itoa(m.boundPort) + ")")
	}

	b.WriteString(headerBox + "\n" + connStatus + "\n\n")

	// System Stats Table (self-polled — always live)
	sysRows := [][]string{
		{"Uptime", strconv.FormatInt(m.sysUptime, 10) + "s"},
		{"Memory Allocated", fmt.Sprintf("%.2f MB", float64(m.sysMemAlloc)/1024/1024)},
		{"Goroutines", strconv.Itoa(m.sysGoroutines)},
		{"GC Pause", fmt.Sprintf("%.2fms", float64(m.sysGCPause)/1e6)},
		{"Heap Objects", strconv.FormatUint(m.sysHeapObjects, 10)},
		{"Total OS Memory", fmt.Sprintf("%.2f MB", float64(m.sysSysMem)/1024/1024)},
	}
	sysTable := renderStyledTable([]string{dashFieldMetric, dashFieldValue}, sysRows)
	sysBox := gridCardStyle.Render(subTitleStyle.Render("System Stats") + "\n" + sysTable)

	// Session Stats Table (UDP-fed from serve process)
	pipelineStage := m.sessPipeline
	if pipelineStage == "" || pipelineStage == "IDLE" {
		if m.sessConnected {
			pipelineStage = successStyle.Render("Idle")
		} else {
			pipelineStage = metricLabelStyle.Render("No stream")
		}
	}
	sessRows := [][]string{
		{"Net Throughput In", strconv.FormatInt(m.sessNetIn/1024, 10) + " KB"},
		{"Net Throughput Out", strconv.FormatInt(m.sessNetOut/1024, 10) + " KB"},
		{"Pipeline Stage", pipelineStage},
		{"Trifecta Reviews", strconv.Itoa(m.trifectaReviews)},
		{"Context Utilized", strconv.Itoa(m.sessContextBytes/1024) + " KB"},
		{"Tokens (Est.)", strconv.Itoa(m.sessTokensEst)},
	}
	sessTable := renderStyledTable([]string{dashFieldMetric, dashFieldValue}, sessRows)
	sessBox := gridCardStyle.Render(subTitleStyle.Render("Session Flow") + "\n" + sessTable)

	// Enhanced Intelligence Table
	recallStatus := warningStyle.Render("Offline")
	if m.recallConnected {
		recallStatus = successStyle.Render("Online")
	}

	llmAvgLatency := "—"
	if m.llmGateActivations > 0 {
		llmAvgLatency = strconv.FormatInt(m.llmGateLatencyMs/int64(m.llmGateActivations), 10) + "ms"
	}

	llmConfiguredStr := warningStyle.Render("False")
	if m.llmConfigured {
		llmConfiguredStr = successStyle.Render("True")
	}

	llmStatusStr := warningStyle.Render("Inactive")
	if m.llmActive {
		llmStatusStr = successStyle.Render("Active")
	}

	intelRows := [][]string{
		{"LLM Configured", llmConfiguredStr},
		{"LLM Status", llmStatusStr},
		{"LLM Gate Invocations", strconv.Itoa(m.llmGateActivations)},
		{"Aporia Rejects", strconv.Itoa(m.llmGateRejects)},
		{"LLM Avg Latency", llmAvgLatency},
		{"Recall DB", recallStatus},
	}
	intelTable := renderStyledTable([]string{dashFieldMetric, dashFieldValue}, intelRows)
	intelBox := gridCardStyle.Render(subTitleStyle.Render("Enhanced Intelligence") + "\n" + intelTable)

	// Session History Table (30-day aggregates from local BuntDB)
	histRows := [][]string{
		{"Net Throughput In", strconv.FormatInt(m.histNetBytesIn/1024, 10) + " KB"},
		{"Net Throughput Out", strconv.FormatInt(m.histNetBytesOut/1024, 10) + " KB"},
		{"Total Stages Run", strconv.Itoa(m.histStagesRunCount)},
		{"Trifecta Reviews", strconv.Itoa(m.histTrifectaCount)},
		{"Context Utilized", strconv.Itoa(m.histContextBytes/1024) + " KB"},
		{"Tokens (Est.)", strconv.Itoa(m.histTokensEst)},
	}
	histTable := renderStyledTable([]string{dashFieldMetric, dashFieldValue}, histRows)
	histBox := gridCardStyle.Render(subTitleStyle.Render("Session History (30d)") + "\n" + histTable)

	// Fixed 2×2 symmetrical grid layout
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, sysBox, sessBox)
	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, intelBox, histBox)
	b.WriteString(lipgloss.JoinVertical(lipgloss.Left, topRow, bottomRow))

	b.WriteString("\n")

	// Recency footer
	if !m.sessLastUpdate.IsZero() {
		ago := int(time.Since(m.sessLastUpdate).Seconds())
		b.WriteString(metricLabelStyle.Render("Session data last received: " + strconv.Itoa(ago) + "s ago"))
	} else {
		b.WriteString(metricLabelStyle.Render("Awaiting session data from serve process..."))
	}

	return b.String()
}

// View renders the full TUI.
func (m model) View() string {
	// Build sidebar
	navLines := []string{
		titleStyle.Render("Socratic Dash"),
		"",
	}

	for i, item := range navItems {
		if i == m.activeTab {
			navLines = append(navLines, activeNavItemStyle.Render("> "+item))
		} else {
			navLines = append(navLines, navItemStyle.Render("  "+item))
		}
	}

	sidebar := sidebarStyle.Render(strings.Join(navLines, "\n"))

	// Build main content
	var content string
	if m.err != nil {
		content = titleStyle.Render("Error") + "\n\n" + fmt.Sprintf("%v", m.err) + "\n\nPress 'q' to quit."
	} else {
		switch m.activeTab {
		case tabSummary:
			content = renderSummary(m)
		case tabQuit:
			content = titleStyle.Render("Quit") + "\n\nPress Enter to exit the dashboard."
		}
	}

	mainView := windowStyle.Render(content)

	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, mainView)
}
