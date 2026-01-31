# Coding Conventions

**Analysis Date:** 2026-01-31

## Naming Patterns

**Files:**
- Package structure uses lowercase names without underscores: `deluge`, `putio`, `sqlite`, `transfer`
- Test files use `_test.go` suffix: `client_test.go`
- No file name variations or abbreviations; names match package purposes
- Internal packages prefixed with `internal/` directory structure

**Functions:**
- PascalCase for exported (public) functions: `NewClient()`, `Authenticate()`, `GetDownloads()`
- Functions starting with action verbs: `New*`, `Get*`, `Set*`, `Check*`, `Claim*`, `Update*`, `Watch*`, `Download*`
- Receiver methods (funcs with receivers) follow same PascalCase pattern: `(c *Client) Authenticate()`
- Private helper functions use lowercase: `buildDownloadClient()`, `setupServer()`, `initializeConfig()`
- Constructor pattern: `NewTypeNameHere()` consistently returns `*TypeNameHere`

**Variables:**
- PascalCase for package-level declarations and struct fields: `Downloader`, `BaseURL`, `Username`, `Password`, `OnFileDownloadError`
- camelCase for local variables and parameters: `ctx`, `cfg`, `err`, `logger`, `transferID`, `baseURL` (when local)
- Short variable names in tight scopes: `t` for Transfer, `r` for Repository, `d` for Downloader
- Acronyms preserved in struct fields: `APIKey`, `APIPath`, `BaseURL`, `OTELAddress` (uppercase)
- Error variables consistently named: `err`
- Logger variables consistently named: `logger`
- Context parameter always first: `func (c *Client) Method(ctx context.Context, ...)`

**Types:**
- Struct names are PascalCase: `Downloader`, `Transfer`, `DownloadRepository`, `TransferOrchestrator`, `Client`
- Interface names are PascalCase ending with "-er": `Notifier`, `DownloadClient`, `TransferClient`, `DownloadRepository`
- Type aliases for primitives use PascalCase: `contextKey` (exception: internal use only)

## Code Style

**Formatting:**
- Standard Go formatting with gofmt
- Line length limit: 150 characters (configured in `.golangci.yml`)
- Function line length limit: 100 lines, 50 statements (configured in `.golangci.yml`)
- Cyclomatic complexity limit: 20 (configured in `.golangci.yml`)
- Indentation: tab character (Go standard)

**Linting:**
- Using `.golangci.yml` with custom configuration at project root
- Default linters enabled except: `errcheck`, `depguard`, `mnd`, `tagalign` (disabled)
- Output format: colored-line-number

## Import Organization

**Order:**
1. Standard library packages: `context`, `fmt`, `io`, `log/slog`, `net/http`, `strings`, `time`
2. Third-party packages: `github.com/dustin/go-humanize`, `github.com/go-chi/chi`, `github.com/stretchr/testify/assert`
3. Internal packages: `github.com/italolelis/seedbox_downloader/internal/...`

**Example from `cmd/seedbox_downloader/main.go`:**
```go
import (
	"context"      // stdlib
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi"              // third-party
	"github.com/italolelis/seedbox_downloader/internal/dc/deluge"  // internal
	...
)
```

**Path Aliases:**
- Not used; imports use full qualified paths
- No path alias shortcuts like `import c "crypto"` observed

## Error Handling

**Patterns:**
- Errors wrapped with context using `fmt.Errorf()` with `%w` formatter
- Pattern: `return fmt.Errorf("descriptive message: %w", err)` for wrapping
- Direct error return without wrapping for interface implementations: `return err` when minimal context needed
- Custom errors defined as variables in storage package: `var ErrDownloaded = errors.New("Download already completed")`
- Error checking follows standard Go pattern: `if err != nil { ... }`
- Errors logged before returning in some contexts, returned directly in others
- No panic usage in business logic; graceful degradation preferred

**Example from `internal/svc/arr/arr.go`:**
```go
if resp.StatusCode != http.StatusOK {
	return false, fmt.Errorf("url: %s, status: %d", url, resp.StatusCode)
}
```

**Example from `cmd/seedbox_downloader/main.go`:**
```go
if err := envconfig.Process("", &cfg); err != nil {
	return nil, nil, fmt.Errorf("failed to load the env vars: %w", err)
}
```

## Logging

**Framework:** Standard library `log/slog` (structured logging)

**Patterns:**
- Logger retrieved from context: `logger := logctx.LoggerFromContext(ctx)`
- Using `.WithGroup()` for grouping related logs: `logger.WithGroup("notification")`
- Using `.With()` for adding structured fields: `logger.With("method", "auth.login")`
- Log levels used: `Info()`, `Debug()`, `Warn()`, `Error()`, `ErrorContext()`
- Context-aware logging: `logger.ErrorContext(ctx, "message", "key", value)`
- Attribute format: key-value pairs: `"transfer_id", t.ID, "transfer_name", t.Name`
- Entry point logs version and important config: `"log_level", cfg.LogLevel, "version", version`
- Shutdown operations logged at Info level: `logger.Info("shutting down...")`
- Errors logged with full context before error return: `logger.Error("failed to update transfer status", "transfer_id", t.ID, "err", err)`

**Example from `internal/dc/deluge/client.go`:**
```go
logger := logctx.LoggerFromContext(ctx).With("method", "auth.login")
logger.Info("authenticating with deluge", "username", c.Username)
logger.Error("failed to marshal payload", "err", err)
```

## Comments

**When to Comment:**
- Exported functions and types: JSDoc-style comment
- Unexported helper functions with non-obvious purpose: Brief comment explaining intent
- Complex business logic: Inline comments explaining "why" not "what"
- Factory functions and abstract patterns: Inline comment explaining pattern
- Minimal commenting; code should be self-documenting through clear naming

**Example from `cmd/seedbox_downloader/main.go`:**
```go
// Config struct for environment variables.
type config struct {
	...
}

// This is an abstract factory for the download client.
func buildDownloadClient(cfg *config) (transfer.DownloadClient, error) {
	...
}

// setupServer prepares the handlers and services to create the http rest server.
func setupServer(ctx context.Context, cfg *config) (*http.Server, error) {
	...
}
```

**JSDoc/TSDoc:**
- Simple one-line comments for exported functions: `// NewClient creates a new *arr API client.`
- Located directly above the function declaration
- Describes what the function does, not implementation details
- No parameter or return documentation observed

## Function Design

**Size:**
- General target: 20-50 lines for most functions
- Maximum 100 lines per function (linted via `.golangci.yml`)
- Maximum 50 statements per function (linted via `.golangci.yml`)
- Complex tasks broken into smaller helper functions with clear names

**Parameters:**
- Context always first parameter in functions that need it: `func (d *Downloader) DownloadTransfer(ctx context.Context, transfer *Transfer)`
- Receivers used for methods: `func (c *Client) Method()`
- Typically 2-4 parameters for most functions
- Struct types passed by pointer for modifications: `*Transfer`, `*Client`, `*Downloader`
- Simple types passed by value: `string`, `int`, `time.Duration`

**Return Values:**
- Error always last: `(result, error)` or `(bool, error)` or just `error`
- Multiple returns use tuple pattern: `([]DownloadRecord, error)` or `(*config, *slog.Logger, error)`
- Named returns not observed; implicit returns preferred

## Module Design

**Exports:**
- Exported types use PascalCase: `Downloader`, `Transfer`, `Client`
- Exported functions use PascalCase: `NewClient()`, `Authenticate()`
- Public struct fields PascalCase: `BaseURL`, `Username`, `APIKey`
- Private struct fields lowercase: `httpClient`, `db`, `cookie`

**Barrel Files:**
- Single-file packages are common: `dc/deluge/client.go`, `dc/putio/client.go`
- No barrel/index files observed (e.g., no `index.go` re-exporting)
- Package-level types and functions in same file as main implementation

**Package Cohesion:**
- Each package has clear responsibility: `deluge` handles Deluge client logic, `sqlite` handles database
- Related types grouped in same file
- Interface definitions in same package as implementations

---

*Convention analysis: 2026-01-31*
