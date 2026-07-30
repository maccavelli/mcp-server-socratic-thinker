<!-- markdownlint-disable MD013 MD060 MD033 -->

# MagicSocratic-Thinker MCP Sub-Server

A high-performance Model Context Protocol (MCP) sub-server for structured, deep adversarial reasoning, trade-off evaluation, and self-correcting logic loops through Socratic dialectic.

## Overview

`mcp-server-socratic-thinker` provides an advanced dialectic engine. It is designed to help AI agents rigorously stress-test architectures, evaluate competing trade-offs, and synthesize opposing paradigms through structured adversarial debate.

### Core Capabilities

| Feature | Description |
|---|---|
| **Socratic Dialectic Engine** | Rigorously stress-tests architectures and trade-offs through an adversarial process. |
| **Multi-Stage Processing** | Enforces structured loops (THESIS, ANTITHESIS, DEFENSE, EVALUATE, CHAOS, APORIA). |
| **Paradox Resolution** | Detects paradoxes and identifies strategies to resolve broken synthesis attempts. |
| **Telemetry & Diagnostics** | Provides internal logging and real-time observability telemetry via a TUI dashboard. |

---

## Quick Start

### Step 1: Place the Binary

Download the `mcp-server-socratic-thinker` binary for your platform and place it in a directory on your system `PATH`.

#### Linux

```bash
# Move the binary to your local bin directory
mv mcp-server-socratic-thinker ~/.local/bin/mcp-server-socratic-thinker
chmod +x ~/.local/bin/mcp-server-socratic-thinker
```

#### macOS

```bash
# Move the binary to your local bin directory
mv mcp-server-socratic-thinker /usr/local/bin/mcp-server-socratic-thinker
chmod +x /usr/local/bin/mcp-server-socratic-thinker
```

#### Windows (PowerShell)

```powershell
# Create a directory for the binary if it doesn't exist
New-Item -ItemType Directory -Force -Path "$env:LOCALAPPDATA\Programs\socratic-thinker"

# Move the binary
Move-Item mcp-server-socratic-thinker.exe "$env:LOCALAPPDATA\Programs\socratic-thinker\mcp-server-socratic-thinker.exe"

# Add to your PATH (current user, persistent)
$currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
[Environment]::SetEnvironmentVariable("Path", "$currentPath;$env:LOCALAPPDATA\Programs\socratic-thinker", "User")
```

---

### Step 2: Initialize Configuration

`mcp-server-socratic-thinker` does not require manual setup or initialization commands.

---

### Step 3: Configure Your IDE

> **⚠️ IMPORTANT ORCHESTRATOR MESSAGING**
>
> While the standalone IDE configurations below are provided for testing and debugging, `mcp-server-socratic-thinker` is designed to be run as a downstream node behind the **`magictools` orchestrator** in production environments.
>
> When running in production, you should **only** configure `magictools` in your IDE, which will automatically proxy requests to `socratic-thinker` as needed.

If you are testing the server standalone, configure your IDE to launch the binary directly (the `serve` argument is optional but explicitly mapped below for clarity):

#### Antigravity (Google DeepMind)

| OS | Configuration File Path |
|---|---|
| Linux / macOS | `~/.gemini/antigravity/mcp_config.json` |
| Windows | `%USERPROFILE%\.gemini\antigravity\mcp_config.json` |

```json
{
  "mcpServers": {
    "socratic-thinker": {
      "command": "/home/youruser/.local/bin/mcp-server-socratic-thinker",
      "args": ["serve"],
      "env": {
        "HOME": "/home/youruser"
      }
    }
  }
}
```

#### Visual Studio Code (GitHub Copilot / Native MCP)

| OS | User-Level Configuration File Path |
|---|---|
| Linux | `~/.config/Code/User/mcp.json` |
| macOS | `~/Library/Application Support/Code/User/mcp.json` |
| Windows | `%APPDATA%\Code\User\mcp.json` |

```json
{
  "mcpServers": {
    "socratic-thinker": {
      "command": "/home/youruser/.local/bin/mcp-server-socratic-thinker",
      "args": ["serve"],
      "env": {
        "HOME": "/home/youruser"
      }
    }
  }
}
```

#### VSCode — Cline Extension

| OS | Configuration File Path |
|---|---|
| Linux | `~/.cline/data/settings/cline_mcp_settings.json` |
| macOS | `~/Library/Application Support/Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json` |
| Windows | `%APPDATA%\Code\User\globalStorage\saoudrizwan.claude-dev\settings\cline_mcp_settings.json` |

```json
{
  "mcpServers": {
    "socratic-thinker": {
      "command": "/home/youruser/.local/bin/mcp-server-socratic-thinker",
      "args": ["serve"],
      "env": {
        "HOME": "/home/youruser"
      }
    }
  }
}
```

#### Claude Desktop

| OS | Configuration File Path |
|---|---|
| macOS | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Windows | `%APPDATA%\Claude\claude_desktop_config.json` |

```json
{
  "mcpServers": {
    "socratic-thinker": {
      "command": "/usr/local/bin/mcp-server-socratic-thinker",
      "args": ["serve"],
      "env": {
        "HOME": "/Users/youruser"
      }
    }
  }
}
```

#### Claude Code (CLI)

```bash
# Linux
claude mcp add socratic-thinker -s user -- /home/youruser/.local/bin/mcp-server-socratic-thinker serve

# macOS
claude mcp add socratic-thinker -s user -- /usr/local/bin/mcp-server-socratic-thinker serve

# Windows (PowerShell)
claude mcp add socratic-thinker -s user -- "C:\Users\YourUser\AppData\Local\Programs\socratic-thinker\mcp-server-socratic-thinker.exe" serve
```

#### Cursor

| OS | Global Configuration File Path |
|---|---|
| Linux / macOS | `~/.cursor/mcp.json` |
| Windows | `%USERPROFILE%\.cursor\mcp.json` |

```json
{
  "mcpServers": {
    "socratic-thinker": {
      "command": "/home/youruser/.local/bin/mcp-server-socratic-thinker",
      "args": ["serve"],
      "env": {
        "HOME": "/home/youruser"
      }
    }
  }
}
```

#### JetBrains IDEs (IntelliJ, GoLand, WebStorm, PyCharm)

| OS | Global Configuration File Path |
|---|---|
| Linux | `~/.config/JetBrains/AI/mcp.json` (or via UI: Settings > Tools > AI Assistant > MCP Servers) |
| macOS | `~/Library/Application Support/JetBrains/AI/mcp.json` (or via UI: Settings > Tools > AI Assistant > MCP Servers) |
| Windows | `%APPDATA%\JetBrains\AI\mcp.json` (or via UI: Settings > Tools > AI Assistant > MCP Servers) |

```json
{
  "mcpServers": {
    "socratic-thinker": {
      "command": "/home/youruser/.local/bin/mcp-server-socratic-thinker",
      "args": ["serve"],
      "env": {
        "HOME": "/home/youruser"
      }
    }
  }
}
```

---

## CLI Commands Reference

### `serve`

Starts the MCP server over stdio (the primary orchestrator backplane). Also binds an HTTP Streamable API (typically on `apiport` config) for internal telemetry access.

```bash
mcp-server-socratic-thinker serve
```

### `dashboard`

Launches the TUI observability dashboard for real-time metrics and diagnostics telemetry.

```bash
mcp-server-socratic-thinker dashboard
```

---

## ⚙️ Configuration & Environment Variables

`mcp-server-socratic-thinker` reads its configuration from a YAML config file stored at `~/.config/mcp-server-socratic-thinker/config.yaml` or through environment variables:

| Variable | Config Key | Default | Description |
|---|---|---|---|
| `MCP_ORCHESTRATOR_OWNED` | `mcp_orchestrator_owned` | `false` | Sets if the server is running inside orchestrator multiplexer mode. |
| `MCP_ENDPOINT_API_PORT` | `mcp_endpoint_api_port` | `47779` | Port for the HTTP Streamable API endpoint. |
| `MCP_REC_URL` | `mcp_rec_url` | `http://localhost:47669/mcp` | Recall server API URL. |
| `MCP_SOC_URL` | `mcp_soc_url` | `http://localhost:47779/mcp` | Socratic-Thinker server URL override. |

---

## 🛠️ MCP Tools & Resources Reference

Once the server is running, the following tools and resources are exposed:

### Tools

| Tool | Parameters | Description |
|---|---|---|
| `socratic_thinker` | `stage` (string), `problem` (string, optional), `thesis` (string, optional), `lemma` (string, optional), `aporia_synthesis` (string, optional), `machine_mode` (bool, optional) | Engage this stateful reasoning engine to stress-test designs and evaluate trade-offs. |
| `fetch_hfsc_logs` | `key` (string) | **[ROLE: DIAGNOSTIC]** Retrieves diagnostic logs from the High-Frequency State Channel (HFSC) memory buffer. |
| `get_internal_logs` | None | **[SERVER: socratic-thinker]** Retrieves execution telemetry logs from the internal ring buffer. |

### Resources

| Resource URI | Description |
|---|---|
| `socratic-thinker://logs` | Exposes the tail of the internal server diagnostics logs. |

---

## 📋 Data Storage Locations

| Data | Linux | macOS | Windows |
|---|---|---|---|
| **Configuration** | `~/.config/mcp-server-socratic-thinker/config.yaml` | `~/Library/Application Support/mcp-server-socratic-thinker/config.yaml` | `%APPDATA%\mcp-server-socratic-thinker\config.yaml` |
| **Server Logs** | `stderr` (captured by IDE) | `stderr` (captured by IDE) | `stderr` (captured by IDE) |

---

*Built with ❤️. Part of the MagicTools Intelligence Suite.*
