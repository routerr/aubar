# AI Usage Bar

[![Go Version](https://img.shields.io/badge/Go-1.26+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Build Status](https://img.shields.io/badge/Build-Passing-brightgreen.svg)](https://github.com/raychang/ai-usage-bar/actions)

`aubar` is a modern, high-performance CLI tool for monitoring AI service usage and quotas across multiple providers. Built with Go 1.26+, it provides real-time usage visualization with tmux integration and extensible provider architecture.

## ✨ Features

- **Multi-Provider Support**: OpenAI/Codex, Claude, and Gemini quota monitoring
- **Real-time Updates**: Configurable refresh intervals with intelligent rate limiting
- **Tmux Integration**: Native tmux status bar support with color formatting
- **Extensible Architecture**: Plugin-based provider system for easy expansion
- **Robust Error Handling**: Graceful degradation and comprehensive diagnostics
- **Cross-Platform**: Native support for macOS and Linux
- **Modern CLI**: Rich terminal UI with Bubble Tea framework

## 📊 Current Output

Standalone `aubar once` / `aubar show` output looks like:

```text
❀ 98% 94% | ✽ 82% 85% |  3-78% 3-100% | 20:15:00
```

`status.txt` contains the same provider layout without the trailing timestamp.

- `❀`: OpenAI / Codex. First number is 5-hour remaining, second is 7-day remaining.
- `✽`: Claude. Same two-window remaining layout when quota is available.
- ``: Gemini. Left side is the selected Pro-chain model, right side is the selected Flash / Flash-lite chain model, both rendered as `<major>-<remaining%>`.

When `tmux.use_tmux_color_format` is enabled, Aubar writes tmux color tokens directly into `status.txt`:

- provider icons: green when connected, maroon when degraded, gray when unavailable
- Gemini major tags like `3-`: yellow `#f9e2af`
- percentages: `#bac2de`

## What The App Writes

Aubar writes two cache files on each successful refresh cycle:

- `status.txt`: one tmux-ready banner line
- `snapshot.json`: structured provider snapshots for `aubar status`, `aubar show`, debugging, and automation

It also maintains a PID file for the detached updater process.

## 🚀 Quick Start

### Prerequisites

- Go 1.26 or later
- tmux (optional, for status bar integration)

### Installation

```bash
# Clone the repository
git clone https://github.com/raychang/ai-usage-bar.git
cd ai-usage-bar

# Build all components
go build -o aubar ./cmd/aubar
go build -o quota ./cmd/quota
go build -o gemini-quota ./cmd/gemini-quota

# Initialize configuration
./aubar setup

# Start monitoring
./aubar run
```

### One-Liner Setup

```bash
curl -sSL https://raw.githubusercontent.com/raychang/ai-usage-bar/main/install.sh | bash
```

## ⚙️ Commands

```bash
aubar setup
aubar run [--foreground] [--print] [--json] [--settings PATH]
aubar once [--json] [--settings PATH]
aubar doctor [--json] [--settings PATH]
aubar show [--settings PATH]
aubar status [--json] [--settings PATH]
aubar restart [--foreground] [--print] [--json] [--settings PATH]
aubar stop
aubar key set <openai|claude|gemini> [--value SECRET]
aubar key delete <openai|claude|gemini>
aubar tui
aubar tmux
```

- `aubar run` starts the updater in the background.
- `aubar run --foreground --print` prints banner lines live in the current terminal.
- `aubar once` collects once, prints a banner, and refreshes both cache files.
- `aubar show` renders the latest cached banner with its stored timestamp.
- `aubar status` reports updater state plus the configured cache file freshness.
- `aubar doctor` runs environment and provider diagnostics.
- `aubar tmux` prints a starter tmux snippet for the default cache path.

## 🔧 Configuration

### Default Paths

**Configuration:**

- macOS: `~/Library/Application Support/ai-usage-bar/settings.json`
- Linux: `~/.config/ai-usage-bar/settings.json`

**Cache:**

- macOS: `~/Library/Caches/ai-usage-bar/`
- Linux: `~/.cache/ai-usage-bar/`

### Configuration Schema

```json
{
  "version": 1,
  "refresh": {
    "global_interval_seconds": 30,
    "timeout_seconds": 12
  },
  "providers": {
    "openai": {
      "enabled": true,
      "source_order": ["cli"],
      "timeout_seconds": 10,
      "min_interval_seconds": 30,
      "credential_ref": "provider/openai"
    },
    "claude": {
      "enabled": true,
      "source_order": ["cli"],
      "timeout_seconds": 10,
      "min_interval_seconds": 60,
      "credential_ref": "provider/claude"
    },
    "gemini": {
      "enabled": true,
      "source_order": ["cli"],
      "timeout_seconds": 10,
      "min_interval_seconds": 60,
      "credential_ref": "provider/gemini"
    }
  },
  "tmux": {
    "enabled": true,
    "status_file": "~/Library/Caches/ai-usage-bar/status.txt",
    "json_cache_file": "~/Library/Caches/ai-usage-bar/snapshot.json",
    "use_tmux_color_format": true,
    "position": "top"
  },
  "theme": {
    "name": "cozy-dark",
    "dark_mode": true
  }
}
```

Notes:

- `providers.*.source_order` currently supports only `["cli"]` in v1.
- `aubar status --settings PATH` reports against the configured `tmux.status_file`, not just the default cache path.
- `aubar key ...` exists, but the current default provider flow is local CLI/helper based rather than direct API polling.

### Runtime Credential Placement

Keep real credential-bearing files outside the repo and use an ignored runtime `.env` only for pointers and client config.

Recommended locations:

- runtime env: `~/Library/Application Support/ai-usage-bar/.env`
- Gemini OAuth creds file: `~/.gemini/oauth_creds.json`
- Claude captured quota file: `~/.claude/captured_quota.json`
- Claude OAuth token: macOS Keychain when available

Both `aubar` and `gemini-quota` load `.env` from:

- the current working directory
- the Aubar config directory

Recommended runtime variables:

```env
GEMINI_OAUTH_CREDS_PATH=/Users/your-user/.gemini/oauth_creds.json
GEMINI_OAUTH_CLIENT_ID=set-in-external-env
GEMINI_OAUTH_CLIENT_SECRET=set-in-external-env
CLAUDE_CAPTURED_QUOTA_PATH=/Users/your-user/.claude/captured_quota.json
```

Use clearly fake placeholders like `set-in-external-env` or `fixture-example-value` in docs and tests. Avoid examples that look like copy-pastable live credentials.

## Provider Behavior

### OpenAI / Codex

- The OpenAI provider reads local Codex rollout telemetry from `~/.codex/sessions/**/rollout-*.jsonl`.
- It does not currently call `codex usage --json` or the OpenAI org usage API.
- The provider returns two quota windows through metadata:
  - primary window: 300 minutes
  - secondary window: 10080 minutes
- The banner renders those as remaining percentages: `❀ <5h> <7d>`.

### Claude

- The Claude provider runs `./quota` first, then falls back to a sibling `quota` binary next to the Aubar executable.
- `quota` returns:
  - `subscription_quota`
  - `last_5_hours`
  - `last_7_days`
  - optional rate-limit probe data
- For live subscription quota, `quota` first tries the macOS Keychain Claude OAuth token.
- If Keychain lookup or live quota fetch fails, it can fall back to `~/.claude/captured_quota.json`.
- The helper can reuse cached captured `/api/oauth/usage` data when live Anthropic quota fetches are unavailable.
- When `subscription_quota` is present, Aubar renders Claude exactly like OpenAI: `✽ <5h> <7d>`.
- If quota is missing but usage data is still available, Claude can degrade to a cost summary instead of disappearing entirely.

Helpful helper flags:

```bash
./quota -no-probe
./quota -no-quota
```

The captured quota file path can be overridden with:

```bash
CLAUDE_CAPTURED_QUOTA_PATH=/custom/path/captured_quota.json
```

### Gemini

- The Gemini provider runs `./gemini-quota` first, then a sibling `gemini-quota` binary next to Aubar.
- `gemini-quota` reads `~/.gemini/oauth_creds.json`, refreshes the access token when needed, and fetches live Cloud Code quota.
- The OAuth creds path can be overridden with `GEMINI_OAUTH_CREDS_PATH`.
- OAuth refresh client config is loaded at runtime from `GEMINI_OAUTH_CLIENT_ID` and `GEMINI_OAUTH_CLIENT_SECRET`.
- Aubar maps model buckets into two display chains:
  - Pro chain: `gemini-3.1-pro -> gemini-3-pro -> gemini-2.5-pro -> gemini-1.5-pro`
  - Flash chain: `gemini-3.1-flash -> gemini-3.1-flash-lite -> gemini-3-flash -> gemini-3-flash-lite -> gemini-2.5-flash-lite -> gemini-2.5-flash -> gemini-1.5-flash`
- Non-zero models are preferred over exhausted ones.
- If every candidate in a chain is exhausted, Aubar still uses the first exhausted candidate so the banner remains stable.
- The banner renders Gemini as:

```text
 3-78% 3-100%
```

### Disconnected / Degraded Rendering

- If a provider has no remaining quota but still has a meaningful degraded state, Aubar can keep the provider visible.
- For Claude, degraded usage-only output can render as repeated trailing USD values like `✽ 0.10$ 0.08$`.
- Providers with no usable data are hidden unless the degraded/error state is intentionally displayable.

## 🎯 Tmux Integration

Start the updater:

```bash
aubar run
```

Minimal tmux integration:

```tmux
set -g status-position top
set -g status-right '#(cat ~/Library/Caches/ai-usage-bar/status.txt 2>/dev/null || echo "○ booting")'
```

Reload tmux:

```bash
tmux source-file ~/.tmux.conf
```

### Advanced Tmux Configuration

For a more feature-rich status bar:

```tmux
# Position and styling
set -g status-position top
set -g status-style bg=#1e1e2e,fg=#cdd6f4

# Left side: system info
set -g status-left '#[fg=#89b4fa]#S#[default] | '

# Right side: AI usage bar with fallback
set -g status-right '#{?client_prefix,#[fg=#f9e2af]#[bg#89b4fa] PREFIX #[default] ,}#[fg=#a6e3a1]#(cat ~/Library/Caches/ai-usage-bar/status.txt 2>/dev/null || echo "○ initializing")#[default] | #[fg=#f38ba8]%H:%M#[default]'

# Update interval
set -g status-interval 5
```

### Troubleshooting Tmux

```bash
# Check cache file contents
cat ~/Library/Caches/ai-usage-bar/status.txt

# Test manually
./aubar once

# Restart services
./aubar restart
```

## 🐛 Troubleshooting

### Common Issues

#### Provider Not Showing

```bash
# Check individual providers
./aubar once --json | jq '.snapshots[]'

# Run diagnostics
./aubar doctor --json

# Check helper binaries
./quota -no-probe
./gemini-quota
```

#### Tmux Status Not Updating

```bash
# Restart the updater
./aubar restart

# Check cache freshness
./aubar status --json

# Verify tmux configuration
tmux source-file ~/.tmux.conf
```

#### Authentication Issues

```bash
# Check runtime env placement
cat ~/Library/Application\\ Support/ai-usage-bar/.env

# Check Gemini helper directly
./gemini-quota

# Check Claude helper directly
./quota -no-probe
```

- If Gemini OAuth refresh fails, verify `GEMINI_OAUTH_CLIENT_ID`, `GEMINI_OAUTH_CLIENT_SECRET`, and `GEMINI_OAUTH_CREDS_PATH`.
- If Claude cached quota is missing, verify `CLAUDE_CAPTURED_QUOTA_PATH` or the default `~/.claude/captured_quota.json`.

### Debug Mode

Enable verbose logging:

```bash
export AUBAR_DEBUG=true
./aubar once --json
```

## 🏗️ Architecture

- `internal/app`: CLI commands, config loading, background process management, cache writes
- `internal/provider`: provider adapters and helper parsing
- `internal/runner`: periodic concurrent collection with refresh/backoff handling
- `internal/render`: banner formatting and tmux color tokens
- `internal/cache`: atomic cache and PID writes
- `internal/config`: settings defaults and validation
- `internal/doctor`, `internal/diagnose`, `internal/tui`: setup and diagnostics

## 🔌 Provider Plugin System

See [docs/provider-plugin-contract.md](docs/provider-plugin-contract.md) for detailed provider development guidelines.

### Supported Providers

| Provider        | Data Source            | Update Frequency | Status     |
|-----------------|------------------------|------------------|------------|
| OpenAI/Codex    | Local Codex telemetry  | 30s              | ✅ Stable  |
| Claude          | CLI helper + API       | 60s              | ✅ Stable  |
| Gemini          | OAuth + API            | 60s              | ✅ Stable  |

### Adding New Providers

The plugin system allows adding new providers with minimal changes:

1. Implement the `Provider` interface
2. Add configuration defaults
3. Register in the provider factory
4. Add comprehensive tests

See the [Provider Contract](docs/provider-plugin-contract.md) for detailed guidelines.
