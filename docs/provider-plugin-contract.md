# Provider Plugin Contract (v2.0)

## 🎯 Goals

Add new providers with **minimal changes** to the core scheduler, rendering, and CLI layers while maintaining:

- **Strong isolation** between providers and the rest of the system
- **Consistent snapshots** for rendering and automation
- **Clear failure modes** (no panics, predictable degraded states)
- **Modern Go patterns** with context-aware design and testability
- **Performance optimization** with intelligent caching and rate limiting

## 🔌 Provider Interface

Implement `internal/provider.Provider`:

```go
type Provider interface {
    // ID returns the unique provider identifier
    ID() domain.ProviderID
    
    // FetchUsage retrieves current usage/quota data with context awareness
    FetchUsage(ctx context.Context) domain.ProviderSnapshot
    
    // MinInterval returns the minimum allowed refresh interval
    MinInterval() time.Duration
    
    // ValidateConfig checks provider-specific configuration
    ValidateConfig(config map[string]interface{}) error
    
    // Close performs cleanup when the provider is shut down
    Close() error
}
```

### Implementation Requirements

Your implementation should be:

- **Context-aware**: Respect `ctx.Done()` for timeouts and cancellations
- **Side-effect-safe**: No global state writes beyond logging and metrics
- **Idempotent per call**: Given the same external state, multiple calls behave identically
- **Testable**: Support dependency injection for unit testing
- **Observable**: Provide meaningful metrics and structured logging

## 📋 Behavior Requirements

### 1. Snapshot Consistency

**Always return a valid snapshot** (including degraded/error cases); never panic:

```go
func (p *MyProvider) FetchUsage(ctx context.Context) domain.ProviderSnapshot {
    // Always return a snapshot, even in error cases
    snapshot := domain.ProviderSnapshot{
        Provider:   p.ID(),
        ObservedAt: time.Now(),
        Source:     "my-provider",
    }
    
    // Implement your logic here
    // ...
    
    return snapshot
}
```

### 2. Status Classification

Populate `Status` as one of:
- `ok`: Usable, fresh quota/usage data
- `degraded`: Partial or approximate data; banner can still render meaningful state
- `error`: Provider unreachable, unauthenticated, or fundamentally misconfigured

### 3. Source Identification

Fill `Source` with a small, stable label:
- `api`, `cli`, `combined`
- Provider-specific helper labels like `claude-quota`, `gemini-oauth`

### 4. Graceful Degradation

If native remaining quota is unavailable:
- Set a human-readable `Reason`
- Leave `RemainingValue`, `RemainingPercent` as `nil`
- Optionally populate `Metadata` with raw usage counters for debugging

### 5. Rate Limiting

Respect provider rate constraints via `MinInterval()`:
- Prefer conservative intervals that stay well inside official limits
- Assume callers may be running multiple Aubar instances
- Implement exponential backoff for transient failures

### 6. CLI Fallback Handling

If you rely on a CLI fallback:
- Capture CLI exit code, stderr hints, and retry guidance in `Metadata`
- Distinguish between transient failures (network, 5xx) and permanent misconfig
- Implement proper timeout handling with context cancellation

## 🏗️ Registration Pattern

### 1. Configuration Defaults

Add provider-specific defaults in `internal/config/settings.go`:

```go
type ProviderSettings struct {
    Enabled             bool                   `json:"enabled"`
    SourceOrder         []string               `json:"source_order"`
    TimeoutSeconds      int                    `json:"timeout_seconds"`
    MinIntervalSeconds  int                    `json:"min_interval_seconds"`
    CredentialRef       string                 `json:"credential_ref"`
    AdditionalConfig    map[string]interface{} `json:"additional_config,omitempty"`
}
```

### 2. Provider Implementation

Implement the adapter in `internal/provider/<name>.go`:

```go
package provider

import (
    "context"
    "time"
    "github.com/routerr/aubar/internal/domain"
)

type MyProvider struct {
    config  map[string]interface{}
    timeout time.Duration
    // Add dependencies for testability
    client  APIClient // interface
    logger  Logger     // interface
}

func NewMyProvider(config map[string]interface{}) (*MyProvider, error) {
    // Validate and initialize configuration
    return &MyProvider{
        config:  config,
        timeout: time.Duration(config["timeout_seconds"].(int)) * time.Second,
    }, nil
}

// Implement Provider interface methods...
```

### 3. Factory Registration

Register the provider in `internal/provider/factory.go`:

```go
func BuildProviders(config *config.Settings) ([]Provider, error) {
    var providers []Provider
    
    if config.Providers.MyProvider.Enabled {
        myProvider, err := NewMyProvider(config.Providers.MyProvider.AdditionalConfig)
        if err != nil {
            return nil, fmt.Errorf("failed to create myprovider: %w", err)
        }
        providers = append(providers, myProvider)
    }
    
    return providers, nil
}
```

### 4. Comprehensive Testing

Add tests under `internal/provider/<name>_test.go`:

```go
func TestMyProvider_Success(t *testing.T) {
    // Test successful data retrieval
}

func TestMyProvider_Degraded(t *testing.T) {
    // Test partial data scenarios
}

func TestMyProvider_Error(t *testing.T) {
    // Test error conditions
}

func TestMyProvider_Timeout(t *testing.T) {
    // Test context cancellation
}
```

## ✅ Compatibility Checklist

### Authentication Sources
- [ ] Environment variable names documented
- [ ] Keyring key mapping implemented
- [ ] Clear error messages for missing/invalid auth
- [ ] OAuth flow support (if applicable)
- [ ] Token refresh logic (if applicable)

### Endpoint / CLI Contract
- [ ] Local helper / CLI invocation shape defined
- [ ] Response shape and error envelope specified
- [ ] Backward-compatibility assumptions documented
- [ ] Version compatibility matrix

### Quota Semantics
- [ ] Provider exposes remaining quota or usage-only
- [ ] Limits scope (per-org, per-project, per-user, per-key)
- [ ] Explicit N/A behavior for unavailable fields
- [ ] Currency and unit conversion handling

### Polling Safety
- [ ] Provider-specific minimum interval configured
- [ ] Burst and concurrency limits defined
- [ ] Exponential backoff implemented
- [ ] Circuit breaker pattern (optional)

### Observability
- [ ] Useful `Metadata` fields for `aubar doctor`
- [ ] Structured logging with appropriate levels
- [ ] Metrics collection (success rate, latency)
- [ ] Clear `Reason` strings for degraded/error states

### Testing Coverage
- [ ] Success snapshots with realistic values
- [ ] Failure snapshots (network, auth, 4xx/5xx, CLI error)
- [ ] Degraded snapshots with partial information
- [ ] Timeout and cancellation tests
- [ ] Integration tests with real services (if possible)
- [ ] Performance benchmarks

### Documentation
- [ ] API documentation links
- [ ] Rate limit documentation
- [ ] Setup and configuration guide
- [ ] Troubleshooting section
- [ ] Example configurations

## 🚀 Best Practices

### Error Handling

```go
func (p *MyProvider) FetchUsage(ctx context.Context) domain.ProviderSnapshot {
    snapshot := domain.ProviderSnapshot{
        Provider:   p.ID(),
        ObservedAt: time.Now(),
        Source:     "my-provider",
    }
    
    // Use context-aware operations
    data, err := p.fetchWithTimeout(ctx)
    if err != nil {
        if errors.Is(err, context.Canceled) {
            snapshot.Status = domain.StatusError
            snapshot.Reason = "operation cancelled"
            return snapshot
        }
        
        if isTransientError(err) {
            snapshot.Status = domain.StatusDegraded
            snapshot.Reason = "temporary failure: " + err.Error()
            // Add retry info to metadata
            snapshot.Metadata = map[string]any{
                "retry_after": p.calculateBackoff(),
                "error_type": "transient",
            }
        } else {
            snapshot.Status = domain.StatusError
            snapshot.Reason = "permanent failure: " + err.Error()
        }
        return snapshot
    }
    
    // Process successful response
    return p.processSuccessResponse(data, snapshot)
}
```

### Performance Optimization

```go
// Implement intelligent caching
type CachedProvider struct {
    cache     *cache.Cache
    provider  Provider
    ttl       time.Duration
}

func (c *CachedProvider) FetchUsage(ctx context.Context) domain.ProviderSnapshot {
    // Try cache first
    if cached, found := c.cache.Get(c.provider.ID()); found {
        if snapshot, ok := cached.(domain.ProviderSnapshot); ok {
            if time.Since(snapshot.ObservedAt) < c.ttl {
                return snapshot
            }
        }
    }
    
    // Fetch fresh data
    snapshot := c.provider.FetchUsage(ctx)
    c.cache.Set(c.provider.ID(), snapshot, c.ttl)
    return snapshot
}
```

### Structured Logging

```go
import "github.com/sirupsen/logrus"

func (p *MyProvider) FetchUsage(ctx context.Context) domain.ProviderSnapshot {
    logger := logrus.WithFields(logrus.Fields{
        "provider": p.ID(),
        "source":  "my-provider",
    })
    
    logger.Debug("starting usage fetch")
    // ...
    logger.WithFields(logrus.Fields{
        "status": snapshot.Status,
        "usage":  snapshot.UsageValue,
    }).Info("usage fetch completed")
    
    return snapshot
}
```

## 📚 Examples

See existing providers for reference:

- `internal/provider/openai.go` - API-based provider
- `internal/provider/claude.go` - CLI helper provider
- `internal/provider/gemini.go` - OAuth + API provider

## 🤝 Contributing

When adding a new provider:

1. **Design Phase**: Create an issue describing the provider and data source
2. **Implementation**: Follow this contract and best practices
3. **Testing**: Ensure comprehensive test coverage
4. **Documentation**: Update relevant documentation
5. **Review**: Submit a pull request for review

For questions, reach out in the project discussions or issues.
