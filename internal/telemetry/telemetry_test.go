package telemetry

import (
	"os"
	"reflect"
	"testing"
)

func TestGetTelemetryPorts(t *testing.T) {
	// Test Default
	os.Unsetenv("MCP_TELEMETRY_UDP_PORTS")
	ports := GetTelemetryPorts()
	if !reflect.DeepEqual(ports, DefaultTelemetryPorts) {
		t.Errorf("Expected %v, got %v", DefaultTelemetryPorts, ports)
	}

	// Test Single Port
	os.Setenv("MCP_TELEMETRY_UDP_PORTS", "12345")
	ports = GetTelemetryPorts()
	if !reflect.DeepEqual(ports, []int{12345}) {
		t.Errorf("Expected [12345], got %v", ports)
	}

	// Test Range and Multiple
	os.Setenv("MCP_TELEMETRY_UDP_PORTS", "100-102, 200")
	ports = GetTelemetryPorts()
	expected := []int{100, 101, 102, 200}
	if !reflect.DeepEqual(ports, expected) {
		t.Errorf("Expected %v, got %v", expected, ports)
	}

	// Test Malformed
	os.Setenv("MCP_TELEMETRY_UDP_PORTS", "abc")
	ports = GetTelemetryPorts()
	if !reflect.DeepEqual(ports, DefaultTelemetryPorts) {
		t.Errorf("Expected fallback %v, got %v", DefaultTelemetryPorts, ports)
	}

	// Cleanup
	os.Unsetenv("MCP_TELEMETRY_UDP_PORTS")
}

func TestServer(t *testing.T) {
	srv := NewServer()
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	// start and close immediately
	srv.Start()
	srv.Broadcast(MetricPayload{})
	srv.Close()
}
