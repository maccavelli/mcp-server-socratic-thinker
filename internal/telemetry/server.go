// Package telemetry provides functionality for the telemetry subsystem.
package telemetry

import (
	"encoding/json"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

// Server handles the UDP broadcast of telemetry data to the dashboard.
type Server struct {
	conn            *net.UDPConn
	dashboardAddr   *net.UDPAddr
	dashboardAddrMu sync.Mutex
	payloadQueue    chan MetricPayload
	stopChan        chan struct{}
}

// NewServer initializes the UDP listener on the first available port.
func NewServer() *Server {
	var conn *net.UDPConn
	for _, port := range GetTelemetryPorts() {
		addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}
		c, err := net.ListenUDP("udp", addr)
		if err == nil {
			conn = c
			slog.Info("telemetry udp listener bound", "port", port)
			break
		}
		slog.Warn("telemetry port unavailable", "port", port, "error", err)
	}

	if conn == nil {
		slog.Warn("all telemetry ports exhausted; starting without dashboard emission")
		return nil
	}

	// Channel for non-blocking queue (drop-oldest), buffered to prevent memory accumulation.
	return &Server{
		conn:         conn,
		payloadQueue: make(chan MetricPayload, 100),
		stopChan:     make(chan struct{}),
	}
}

// Start begins listening for dashboard pings to register the client address and starts the emission loop.
func (s *Server) Start() {
	if s == nil || s.conn == nil {
		return
	}

	// Listener goroutine
	go func() {
		buf := make([]byte, 64)
		for {
			_, remoteAddr, err := s.conn.ReadFromUDP(buf)
			if err != nil {
				if strings.Contains(err.Error(), "use of closed") {
					return
				}
				continue
			}
			s.dashboardAddrMu.Lock()
			s.dashboardAddr = remoteAddr
			s.dashboardAddrMu.Unlock()

			// Always ACK pings to prevent dashboard timeouts, as the dashboard uses a synchronous write-read loop.
			ack := []byte(`{"pipeline_stage":"ACK"}`)
			if err := s.conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
				continue
			}
			if _, err := s.conn.WriteToUDP(ack, remoteAddr); err != nil {
				slog.Debug("telemetry ack write failed", "error", err)
			}
		}
	}()

	// Emission goroutine processing the queue
	go func() {
		for {
			select {
			case <-s.stopChan:
				return
			case payload := <-s.payloadQueue:
				s.dashboardAddrMu.Lock()
				target := s.dashboardAddr
				s.dashboardAddrMu.Unlock()

				if target == nil {
					continue
				}

				data, err := json.Marshal(payload)
				if err == nil {
					// Minimal deadline to avoid blocking indefinitely
					if deadlineErr := s.conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond)); deadlineErr != nil {
						continue
					}
					if _, writeErr := s.conn.WriteToUDP(data, target); writeErr != nil {
						slog.Debug("telemetry payload write failed", "error", writeErr)
					}
				}
			}
		}
	}()
}

// Broadcast sends the MetricPayload to the connected dashboard via a non-blocking queue.
func (s *Server) Broadcast(payload MetricPayload) {
	if s == nil || s.conn == nil {
		return
	}

	select {
	case s.payloadQueue <- payload:
		// added to queue
	default:
		// Queue full, drop oldest
		select {
		case <-s.payloadQueue: // pop oldest
		default: // double-check
		}
		// try inserting again
		select {
		case s.payloadQueue <- payload:
		default:
		}
	}
}

// Close gracefully shuts down the UDP listener.
func (s *Server) Close() {
	if s == nil || s.conn == nil {
		return
	}
	close(s.stopChan)
	if err := s.conn.Close(); err != nil {
		slog.Warn("telemetry udp close failed", "error", err)
	}
}
