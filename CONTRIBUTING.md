# Contributing to AI Usage Bar

Thank you for your interest in contributing to AI Usage Bar! This document provides guidelines and information to help you get started.

## 🚀 Getting Started

### Prerequisites

- Go 1.26 or later
- Git
- Make (optional, for using Makefile commands)
- Docker (optional, for containerized development)

### Development Setup

```bash
# Clone the repository
git clone https://github.com/raychang/ai-usage-bar.git
cd ai-usage-bar

# Install dependencies
go mod download

# Run tests to verify setup
make test

# Build the project
make build

# Enable the repo's pre-commit hooks
git config core.hooksPath .githooks
```

Before your first commit, run:

```bash
scripts/check-secrets.sh --all
```

This repo keeps real credentials outside git. Use clearly fake placeholders such as `fixture-example-value` in tests and docs.

## 🏗️ Project Structure

```
ai-usage-bar/
├── cmd/                    # Main entry points for CLI tools
│   ├── aubar/             # Main application
│   ├── quota/             # Claude quota helper
│   └── gemini-quota/      # Gemini quota helper
├── internal/              # Private application code
│   ├── app/              # CLI application logic
│   ├── cache/            # Cache management
│   ├── config/           # Configuration handling
│   ├── credentials/      # Credential management
│   ├── diagnose/         # Diagnostic tools
│   ├── doctor/           # Health checks
│   ├── domain/           # Domain types and interfaces
│   ├── envload/          # Environment loading
│   ├── keyringx/         # Keyring utilities
│   ├── provider/         # Provider implementations
│   ├── render/           # Banner rendering
│   ├── runner/           # Background runner
│   └── tui/              # Terminal UI
├── docs/                 # Documentation
├── tests/                # Integration tests
└── scripts/              # Build and utility scripts
```

## 🧪 Testing

### Running Tests

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run specific package tests
go test ./internal/provider/

# Run tests with verbose output
go test -v ./...
```

### Writing Tests

- Follow Go testing conventions
- Use table-driven tests for multiple scenarios
- Mock external dependencies for unit tests
- Include integration tests for provider implementations

Example test structure:

```go
func TestMyProvider_Success(t *testing.T) {
    tests := []struct {
        name     string
        config   map[string]interface{}
        expected domain.ProviderSnapshot
        wantErr  bool
    }{
        {
            name: "successful fetch",
            config: map[string]interface{}{
                "timeout_seconds": 10,
            },
            expected: domain.ProviderSnapshot{
                Status: domain.StatusOK,
                // ... other fields
            },
            wantErr: false,
        },
        // ... more test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            provider, err := NewMyProvider(tt.config)
            if (err != nil) != tt.wantErr {
                t.Fatalf("NewMyProvider() error = %v, wantErr %v", err, tt.wantErr)
            }

            ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()

            got := provider.FetchUsage(ctx)
            assert.Equal(t, tt.expected.Status, got.Status)
            // ... more assertions
        })
    }
}
```

## 📝 Code Style

### Go Conventions

- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use `gofmt` and `goimports` for formatting
- Prefer clear, descriptive names over abbreviations
- Keep functions small and focused
- Use interfaces for dependency injection

### Linting

```bash
# Run linter
make lint

# Format code
make fmt
```

We use the following tools:
- `gofmt` for code formatting
- `golangci-lint` for static analysis
- `goimports` for import management

## 🔧 Development Workflow

### 1. Create an Issue

Before starting work, create an issue describing:
- The problem you're solving
- Your proposed solution
- Any questions or concerns

### 2. Create a Branch

```bash
git checkout -b feature/your-feature-name
# or
git checkout -b fix/your-fix-name
```

### 3. Make Changes

- Write clear, focused commits
- Include tests for new functionality
- Update documentation as needed
- Ensure all tests pass

### 4. Submit Pull Request

- Create a pull request with a clear description
- Link to related issues
- Request review from maintainers
- Address feedback promptly

### Commit Message Format

Use conventional commit messages:

```
type(scope): description

[optional body]

[optional footer]
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes
- `refactor`: Code refactoring
- `test`: Test changes
- `chore`: Maintenance tasks

Examples:
```
feat(provider): add new Anthropic provider

Implement support for Anthropic's Claude API with proper
rate limiting and error handling.

Closes #123
```

```
fix(render): correct percentage calculation

Fix division by zero error when quota limit is zero.
```

## 🎯 Adding New Providers

When adding a new AI provider:

1. **Design Phase**
   - Create an issue describing the provider
   - Research API documentation and rate limits
   - Plan authentication method

2. **Implementation**
   - Follow the [Provider Plugin Contract](docs/provider-plugin-contract.md)
   - Implement the `Provider` interface
   - Add configuration options
   - Register in the provider factory

3. **Testing**
   - Unit tests for all scenarios
   - Integration tests with real API (if possible)
   - Error handling tests
   - Performance benchmarks

4. **Documentation**
   - Update README with provider information
   - Add setup instructions
   - Document configuration options
   - Update troubleshooting guide

## 🐛 Bug Reports

When reporting bugs:

1. Use the issue template
2. Provide detailed reproduction steps
3. Include environment information
4. Attach relevant logs or screenshots
5. Specify expected vs. actual behavior

## 💡 Feature Requests

When requesting features:

1. Check if it already exists or is planned
2. Provide clear use case and motivation
3. Consider implementation complexity
4. Be open to discussion and alternatives

## 📚 Documentation

### Types of Documentation

- **README.md**: Project overview and quick start
- **CONTRIBUTING.md**: Development guidelines (this file)
- **docs/**: Detailed documentation
  - `provider-plugin-contract.md`: Provider development guide
  - Additional guides as needed

### Writing Documentation

- Use clear, concise language
- Include code examples
- Keep documentation up to date
- Use consistent formatting

## 🏷️ Release Process

Releases are managed by maintainers:

1. Update version in `go.mod`
2. Update CHANGELOG.md
3. Create Git tag
4. Build and release binaries
5. Update documentation

## 🤝 Community Guidelines

### Code of Conduct

We are committed to providing a welcoming and inclusive environment. Please:

- Be respectful and constructive
- Welcome newcomers and help them learn
- Focus on what is best for the community
- Show empathy towards other community members

### Getting Help

- **GitHub Issues**: Bug reports and feature requests
- **GitHub Discussions**: General questions and ideas
- **Documentation**: Check existing docs first

## 📧 Contact

- **Maintainers**: @raychang
- **Repository**: https://github.com/raychang/ai-usage-bar
- **Issues**: https://github.com/raychang/ai-usage-bar/issues
- **Discussions**: https://github.com/raychang/ai-usage-bar/discussions

## 🙏 Acknowledgments

Thank you to all contributors who have helped make AI Usage Bar better!

Your contributions are greatly appreciated! 🎉
