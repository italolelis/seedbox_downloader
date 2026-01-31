# Testing Patterns

**Analysis Date:** 2026-01-31

## Test Framework

**Runner:**
- Go built-in `testing` package
- Standard `go test` command
- No external test runner framework detected (e.g., no testify/suite, no ginkgo)

**Assertion Library:**
- `github.com/stretchr/testify/assert` for assertions
- Assert functions used with table-driven tests

**Run Commands:**
```bash
go test ./...              # Run all tests
go test -v ./...           # Run with verbose output
go test -cover ./...       # Run with coverage
go test -race ./...        # Run with race detector
go test -timeout 5m ./... # Run with custom timeout (golangci timeout is 5m)
```

**Go Version:**
- Go 1.23.0 (specified in `go.mod`)
- Toolchain: go1.23.8

## Test File Organization

**Location:**
- Co-located with implementation: `internal/dc/deluge/client_test.go` lives alongside `internal/dc/deluge/client.go`
- Tests in same package as code: `package deluge_test` used but imports same module
- Pattern: test files immediately follow implementation files in same directory

**Naming:**
- Suffix pattern: `_test.go` for test files
- Test function pattern: `Test[FunctionName]_[Scenario]` or `Test[FunctionName]`
- Examples: `TestNewClient()`, `TestAuthenticate_Error()`, `TestGetTaggedTorrents()`

**Structure:**
```
internal/
├── dc/
│   ├── deluge/
│   │   ├── client.go        # Implementation
│   │   └── client_test.go   # Tests
│   └── putio/
│       ├── client.go
│       └── (no tests found)
├── svc/
│   └── arr/
│       └── arr.go           # (no tests found)
└── storage/
    └── sqlite/
        └── download_repository.go  # (no tests found)
```

## Test Structure

**Suite Organization:**

The codebase uses Go's standard table-driven test pattern. Example from `internal/dc/deluge/client_test.go`:

```go
func TestNewClient(t *testing.T) {
	tests := []struct {
		name         string
		baseURL      string
		apiPath      string
		completedDir string
		username     string
		password     string
	}{
		{"basic", "http://localhost", "/api", "/downloads", "user", "pass"},
		{"empty", "", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := deluge.NewClient(tt.baseURL, tt.apiPath, tt.completedDir, tt.username, tt.password)
			assert.Equal(t, tt.baseURL, client.BaseURL)
			assert.Equal(t, tt.apiPath, client.APIPath)
			assert.Equal(t, tt.username, client.Username)
			assert.Equal(t, tt.password, client.Password)
		})
	}
}
```

**Patterns:**

1. **Table-Driven Tests:**
   - Each test case is a struct in a slice: `tests := []struct{ name string; ... }`
   - Field `name` for test case identifier
   - Test cases include all inputs and expected outputs
   - Sub-tests using `t.Run(tt.name, func(t *testing.T) { ... })`

2. **Setup Pattern:**
   - Use `httptest.NewServer()` for HTTP mocking: `ts := httptest.NewServer(http.HandlerFunc(...))`
   - Inline setup within test functions, no separate setUp functions
   - Defer cleanup: `defer ts.Close()`

3. **Teardown Pattern:**
   - HTTP servers closed with `defer ts.Close()`
   - Deferred response body closing: `defer resp.Body.Close()`
   - No explicit cleanup functions; defers handle lifecycle

4. **Assertion Pattern:**
   - Using `testify/assert` package
   - Assert functions with receiver test: `assert.Equal(t, expected, actual)`
   - Assert for error conditions: `assert.Error(t, err)`, `assert.NoError(t, err)`
   - Error message matching: `assert.Contains(t, err.Error(), expectedMsg)`
   - Collection length assertions: `assert.Len(t, torrents, expectedCount)`

## Mocking

**Framework:** `httptest` package (standard library) for HTTP mocking

**Patterns:**

From `internal/dc/deluge/client_test.go`:

```go
func TestGetTaggedTorrents(t *testing.T) {
	tests := []struct {
		name         string
		jsonResp     map[string]any
		tag          string
		expectCount  int
		expectFields map[string]string
	}{
		{
			"single match",
			map[string]any{
				"result": map[string]any{
					"abc123": map[string]any{
						"label":     "mytag",
						"progress":  100.0,
						"name":      "file1",
						"save_path": "/downloads",
						"files": []any{
							map[string]any{"path": "file1.mkv"},
						},
					},
				},
				"error": nil,
				"id":    2,
			},
			"mytag",
			1,
			map[string]string{"ID": "abc123", ...},
		},
		// more test cases...
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonResp, _ := json.Marshal(tt.jsonResp)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write(jsonResp)
			}))
			defer ts.Close()

			client := deluge.NewClient(ts.URL, "", "", "user", "pass")
			torrents, err := client.GetTaggedTorrents(context.Background(), tt.tag)
			// assertions...
		})
	}
}
```

**HTTP Mocking with `httptest`:**
- Server creation: `httptest.NewServer(http.HandlerFunc(func(w, r) { ... }))`
- Response body writing: `w.Write(jsonResp)` or `fmt.Fprint(w, responseBody)`
- Response header setting: `w.Header().Set("Content-Type", "application/json")`
- Status code control: `w.WriteHeader(http.StatusCode)`
- URL available: `ts.URL` provides the mocked server address

**What to Mock:**
- HTTP clients and external API calls (Deluge, Put.io, Sonarr, Radarr)
- Use `httptest` for these; inject server URL into client constructor
- Avoid mocking internal service logic; test full flow where possible

**What NOT to Mock:**
- Core business logic (downloaders, transfer orchestrators)
- Internal struct fields; test through public interfaces
- No interface mocking library detected; suggest interfaces tested implicitly through integration

## Fixtures and Factories

**Test Data:**
- Inline table-driven test cases using anonymous structs
- JSON response payloads created as `map[string]any` then marshaled
- No separate fixture files or data builders

Example fixture approach from tests:
```go
jsonResp := map[string]any{
	"result": map[string]any{
		"abc123": map[string]any{
			"label":     "mytag",
			"progress":  100.0,
			"name":      "file1",
			"save_path": "/downloads",
		},
	},
}
```

**Location:**
- Test data embedded in test functions
- No `testdata/` directory used
- No fixture builders or factories observed

## Coverage

**Requirements:** No coverage target enforced

**View Coverage:**
```bash
go test -cover ./...           # Summary
go test -coverprofile=cover.out ./...
go tool cover -html=cover.out  # HTML report
```

**Known Gaps:**
- Only `internal/dc/deluge/client_test.go` has tests (1 test file out of 20+ Go files)
- No tests for: `downloader`, `transfer`, `sqlite` repository, `arr`, `svc`, `telemetry`, `notifier`, `http` handlers
- Critical paths untested: download orchestration, transfer management, database operations, Sonarr/Radarr integration

## Test Types

**Unit Tests:**
- Scope: Individual packages and client constructors
- Approach: Table-driven tests with isolated test cases
- Currently only HTTP client tests present
- Test external API communication (mocked via `httptest`)

**Integration Tests:**
- Not implemented
- Would test database operations, multi-service interaction, end-to-end download flow
- No test helper utilities or test containers detected

**E2E Tests:**
- Not implemented
- Would require docker-compose setup (telemetry stack available but not for E2E)
- No E2E test framework or helpers

## Common Patterns

**Async Testing:**
No goroutine testing observed in current test suite. The codebase uses goroutines heavily (channels, select statements) but these are not tested.

**Error Testing:**

From `internal/dc/deluge/client_test.go`:

```go
func TestAuthenticate_Error(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		responseBody   string
		expectErrorMsg string
	}{
		{"unauthorized", http.StatusUnauthorized, `{"error": "unauthorized"}`, "auth failed"},
		{"bad request", http.StatusBadRequest, `{"error": "bad request"}`, "auth failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				fmt.Fprint(w, tt.responseBody)
			}))
			defer ts.Close()

			client := deluge.NewClient(ts.URL, "", "", "user", "pass")
			err := client.Authenticate(context.Background())
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectErrorMsg)
		})
	}
}
```

Pattern:
- Test case includes error condition (status code, response)
- HTTP mock server returns the error state
- Assert error exists: `assert.Error(t, err)`
- Assert error message contains expected text: `assert.Contains(t, err.Error(), msg)`
- Each error scenario in separate test case

**Context Testing:**
- Context passed as `context.Background()` in tests
- No timeout or cancellation testing observed

---

*Testing analysis: 2026-01-31*
