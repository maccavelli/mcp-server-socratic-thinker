package config

import (
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

const (
	// Project Identity
	Name     = "mcp-server-socratic-thinker"
	Platform = "Socratic Thinker"
)

type State struct {
	OrchestratorOwned bool   `mapstructure:"mcp_orchestrator_owned"`
	EndpointAPIPort   int    `mapstructure:"mcp_endpoint_api_port"`
	RecallURL         string `mapstructure:"mcp_rec_url"`
	SocraticURL       string `mapstructure:"mcp_soc_url"`
}

type Config struct {
	mu    sync.RWMutex
	state State
	v     *viper.Viper
}

func New() *Config {
	cfg := &Config{
		v: viper.New(),
	}

	cfg.v.SetConfigName("config")
	cfg.v.SetConfigType("yaml")
	cfg.v.AddConfigPath(".")
	cfg.v.AddConfigPath("$HOME/.config/" + Name)

	// Bind Environment Variables
	cfg.v.BindEnv("mcp_orchestrator_owned", "MCP_ORCHESTRATOR_OWNED")
	cfg.v.BindEnv("mcp_endpoint_api_port", "MCP_ENDPOINT_API_PORT")
	cfg.v.BindEnv("mcp_rec_url", "MCP_REC_URL")
	cfg.v.BindEnv("mcp_soc_url", "MCP_SOC_URL")

	// Set Defaults
	cfg.v.SetDefault("mcp_orchestrator_owned", false)
	cfg.v.SetDefault("mcp_endpoint_api_port", 47779)
	cfg.v.SetDefault("mcp_rec_url", "")
	cfg.v.SetDefault("mcp_soc_url", "")

	if err := cfg.v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFoundError) {
			slog.Warn("Failed to read config file", "error", err)
		}
	}

	cfg.refreshState()

	cfg.v.WatchConfig()
	cfg.v.OnConfigChange(func(e fsnotify.Event) {
		slog.Info("[Viper] Config file modified", "file", e.Name)
		cfg.refreshState()
	})

	return cfg
}

func (c *Config) refreshState() {
	var newState State
	if err := c.v.Unmarshal(&newState); err != nil {
		slog.Error("Failed to unmarshal config state", "error", err)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = newState
}

// ResolveAPIPort returns the configured API port.
func (c *Config) ResolveAPIPort() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state.EndpointAPIPort > 0 {
		return c.state.EndpointAPIPort
	}
	return 47779
}

// ResolveRecallURL returns the canonicalized MCP_REC_URL environment variable.
func (c *Config) ResolveRecallURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val := c.state.RecallURL
	if val == "" {
		return "http://localhost:47669/mcp"
	}
	return strings.TrimSpace(val)
}

// ResolveSocraticURL returns the canonicalized MCP_SOC_URL environment variable.
func (c *Config) ResolveSocraticURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val := c.state.SocraticURL
	if val == "" {
		return "http://localhost:47779/mcp"
	}
	return strings.TrimSpace(val)
}
