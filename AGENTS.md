# Aubar Agent Guide

This document helps AI agents understand and work effectively in the Aubar codebase.

## Essential Commands

### Build
- `./build.sh` - Build native binaries for current platform (default: ./dist/native)
- `./build.sh --all` - Build for all common platforms (linux/amd64, linux/arm64, windows/amd64, windows/arm64, darwin/amd64, darwin/arm64)
- `./build.sh --os linux --arch arm64` - Build for specific OS/arch
- `./build.sh --output-dir ./custom-dist` - Specify output directory
- PowerShell equivalent: `.\build.ps1`

### Testing
- `go test ./...` - Run all tests
- `go test -v ./internal/...` - Verbose tests for internal packages
- Test files are located alongside source code with `_test.go` suffix

### Running the Application
- `./dist/native/aubar setup` - Initialize configuration (launches TUI)
- `./dist/native/aubar run` - Start background updater process
- `./dist/native/aubar once` - Collect usage data once and print banner
- `./dist/native/aubar show` - Display latest cached banner
- `./dist/native/aubar status` - Check updater status
- `./dist/native/aubar doctor` - Run diagnostics
- `./dist/native/aubar tui` - Same as setup
- `./dist/native/aubar tmux` - Print tmux configuration snippet

### Key Management
- `./dist/native/aubar key set <provider>` - Store credential for provider (openai/claude/gemini)
- `./dist/native/aubar key delete <provider>` - Remove stored credential

## Code Organization

```
├── cmd/                     # Main application entry points
│   ├── aubar/               # Primary aubar command
│   ├── quota/               # Claude quota debug wrapper
│   └── gemini-quota/        # Gemini quota debug wrapper
├── internal/                # Private application code
│   ├── app/                 # CLI command handling and main application logic
│   ├── provider/            # Provider adapters and factory
│   ├── runner/              # Periodic collection service with backoff handling
│   ├── render/              # Banner formatting and tmux color tokens
│   ├── cache/               # Atomic file writes for status and JSON cache
│   ├── config/              # Settings loading, defaults, and validation
│   ├── doctor/              # Diagnostic reporting
│   ├── diagnose/            # Provider-specific diagnostics
│   ├── tui/                 # Text-based user interface for setup
│   ├── envload/             # Environment variable loading
│   ├── keyringx/            # System keyring abstraction
│   ├── auth/                # Credential validation
│   └── domain/              # Shared data structures and interfaces
├── tests/                   # Integration test helpers (if any)
├── docs/                    # Documentation
│   ├── architecture.md      # High-level architecture
│   └── provider-plugin-contract.md # Provider development guidelines
├── build.sh                 # Cross-platform build script
├── build.ps1                # PowerShell build script
├── go.mod                   # Go module definition
└── go.sum                   # Go module checksums
```

## Naming Conventions & Style

- **Go Standards**: Follows standard Go formatting and conventions
- **Interfaces**: Named with `-er` suffix when appropriate (e.g., `Provider`, `CLIExecutor`)
- **Structs**: MixedCase, fields exported when needed across packages
- **Variables**: mixedCase, prefer descriptive names
- **Constants**: MixedCase or ALL_CAPS for enum-like values
- **Errors**: Wrapped with `%w` or `errors.Join()` when appropriate, checked explicitly
- **Logging**: Uses `fmt.Fprintf` to Stdout/Stderr in app layer; internal packages return errors/data for upper layers to handle
- **Context**: Passed through call chains for cancellation and timeouts
- **Dependency Injection**: Uses constructor functions and interface implementation swapping (visible in tests)

## Testing Approach

- **Table-driven tests**: Common pattern using anonymous structs for test cases
- **Mocking**: Uses dependency injection rather than mocking frameworks (see `provider.DefaultCLIExecutor{}`)
- **Test files**: Located next to source code with `_test.go` suffix
- **Temporary directories**: Uses `t.TempDir()` for isolation
- **Environment isolation**: Helper functions like `isolateCacheEnv()` to control HOME/XDG_CACHE_HOME
- **Golden tests**: Some tests compare output against expected strings (using `strings.Contains`)
- **Error checking**: Tests often verify specific error conditions and messages

## Important Gotchas & Non-obvious Patterns

### Credential Management
- Real credentials should **never** be committed to the repository
- Runtime credentials go in specific locations:
  - macOS: `~/Library/Application Support/ai-usage-bar/.env`
  - Linux: `~/.config/ai-usage-bar/.env`
  - Gemini OAuth: `~/.gemini/oauth_creds.json` (override with `GEMINI_OAUTH_CREDS_PATH`)
  - Claude captured quota: `~/.claude/captured_quota.json` (override with `CLAUDE_CAPTURED_QUOTA_PATH`)
- Environment variables for OAuth clients:
  - `GEMINI_OAUTH_CLIENT_ID` and `GEMINI_OAUTH_CLIENT_SECRET`
  - `CLAUDE_CAPTURED_QUOTA_PATH`

### Build Specifics
- CGO is disabled (`CGO_ENABLED=0`) for maximum portability
- Uses `-trimpath` flag to eliminate absolute paths from binaries
- Keyring functionality works without CGO on most platforms via go-keyring

### Caching & Persistence
- Two cache files written per successful refresh:
  - `status.txt`: tmux-ready banner line (no timestamp)
  - `snapshot.json`: structured provider data
- PID file tracks background updater process
- Cache directories follow platform conventions:
  - macOS: `~/Library/Caches/ai-usage-bar/`
  - Linux: `~/.cache/ai-usage-bar/`
- Fallback to OS temp directory if primary cache location unwritable

### Provider Behavior
- **OpenAI/Codex**: Reads local telemetry from `~/.codex/sessions/**/rollout-*.jsonl`
- **Claude**: 
  - Tries macOS Keychain for OAuth token first
  - Falls back to `~/.claude/captured_quota.json`
  - Can degrade to cost-only display when quota unavailable
- **Gemini**:
  - Uses OAuth flow with token refresh
  - Reads `~/.gemini/oauth_creds.json`
  - Requires `GEMINI_OAUTH_CLIENT_ID`/`SECRET` for refresh
  - Maps models into Pro/Flash chains for display

### Process Management
- `aubar run` starts a detached background process by default
- Use `--foreground` to run in current terminal
- Background process logs to `aubar.log` in cache directory
- PID file management with fallback to process pattern matching
- Signal handling for graceful shutdown (SIGTERM)

### Tmux Integration
- Status file contains plain text banner (no timestamp)
- When `use_tmux_color_format`: true, includes tmux color tokens
- Typical tmux configuration:
  ```tmux
  set -g status-position top
  set -g status-right '#(cat ~/Library/Caches/ai-usage-bar/status.txt 2>/dev/null || echo "○ booting")'
  ```

### Error Handling
- Verbose logging enabled with `AUBAR_DEBUG=true`
- Diagnostic information available via `aubar doctor --json`
- Graceful degradation when providers fail (cached data reuse with staleness checks)
- Claude quota caching: reuses recent quota snapshots when live fetch fails (configurable max age)

## Provider Plugin System

See [docs/provider-plugin-contract.md] for detailed guidelines on adding new providers.

Basic steps:
1. Implement the `Provider` interface (in `internal/provider/provider.go`)
2. Add configuration defaults in `internal/config/settings.go`
3. Register in the provider factory (`internal/provider/factory.go`)
4. Add comprehensive tests

The interface requires:
- `Collect(context.Context) (*domain.Collection, error)`
- Methods for identifying the provider and its configuration

## Common Development Tasks

### Adding a New Provider
1. Create provider file in `internal/provider/` (e.g., `internal/provider/newprovider.go`)
2. Implement `Provider` interface methods
3. Add provider settings to `config.Settings` struct
4. Update `provider.BuildProviders()` to include new provider
5. Add tests in `internal/provider/newprovider_test.go`
6. Update documentation in `docs/provider-plugin-contract.md`

### Modifying Build Process
- Edit `build.sh` for Unix-like systems
- Edit `build.ps1` for PowerShell
- Both scripts share similar argument parsing and build logic

### Changing Cache Locations
- Modify `config.DefaultCacheDir()` and related functions in `internal/config/`
- Update path resolution throughout app/cmd layers
- Consider backward compatibility with existing installations
