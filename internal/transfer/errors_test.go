package transfer

import (
	"errors"
	"fmt"
	"testing"
)

// TestInvalidContentError_Error verifies error message formatting
func TestInvalidContentError_Error(t *testing.T) {
	err := &InvalidContentError{
		Filename: "test.torrent",
		Reason:   "file too large",
	}

	expected := "invalid torrent content in test.torrent: file too large"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

// TestNetworkError_Error verifies error message formatting
func TestNetworkError_Error(t *testing.T) {
	tests := []struct {
		name       string
		err        *NetworkError
		wantFormat string
	}{
		{
			name: "with HTTP status code",
			err: &NetworkError{
				Operation:  "upload",
				StatusCode: 503,
				APIMessage: "service unavailable",
			},
			wantFormat: "network error during upload (HTTP 503): service unavailable",
		},
		{
			name: "without HTTP status code",
			err: &NetworkError{
				Operation:  "upload",
				StatusCode: 0,
				APIMessage: "connection timeout",
			},
			wantFormat: "network error during upload: connection timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantFormat {
				t.Errorf("Error() = %q, want %q", got, tt.wantFormat)
			}
		})
	}
}

// TestDirectoryError_Error verifies error message formatting
func TestDirectoryError_Error(t *testing.T) {
	err := &DirectoryError{
		DirectoryName: "itv",
		Reason:        "not found",
	}

	expected := "directory error for 'itv': not found"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

// TestAuthenticationError_Error verifies error message formatting
func TestAuthenticationError_Error(t *testing.T) {
	err := &AuthenticationError{
		Operation: "upload_torrent",
	}

	expected := "authentication failed during upload_torrent"
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}

// TestInvalidContentError_Unwrap verifies error chain traversal
func TestInvalidContentError_Unwrap(t *testing.T) {
	cause := errors.New("underlying cause")
	err := &InvalidContentError{
		Filename: "test.torrent",
		Reason:   "validation failed",
		Err:      cause,
	}

	unwrapped := errors.Unwrap(err)
	if unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}

	// Verify errors.Is works through the chain
	wrapped := fmt.Errorf("context: %w", err)
	if !errors.Is(wrapped, cause) {
		t.Error("errors.Is() should find cause in wrapped chain")
	}
}

// TestNetworkError_Unwrap verifies error chain traversal
func TestNetworkError_Unwrap(t *testing.T) {
	cause := errors.New("connection reset")
	err := &NetworkError{
		Operation:  "upload",
		StatusCode: 500,
		APIMessage: "internal server error",
		Err:        cause,
	}

	unwrapped := errors.Unwrap(err)
	if unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}

	// Verify errors.Is works through the chain
	wrapped := fmt.Errorf("context: %w", err)
	if !errors.Is(wrapped, cause) {
		t.Error("errors.Is() should find cause in wrapped chain")
	}
}

// TestDirectoryError_Unwrap verifies error chain traversal
func TestDirectoryError_Unwrap(t *testing.T) {
	cause := errors.New("permission denied")
	err := &DirectoryError{
		DirectoryName: "itv",
		Reason:        "access denied",
		Err:           cause,
	}

	unwrapped := errors.Unwrap(err)
	if unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}

	// Verify errors.Is works through the chain
	wrapped := fmt.Errorf("context: %w", err)
	if !errors.Is(wrapped, cause) {
		t.Error("errors.Is() should find cause in wrapped chain")
	}
}

// TestAuthenticationError_Unwrap verifies error chain traversal
func TestAuthenticationError_Unwrap(t *testing.T) {
	cause := errors.New("token expired")
	err := &AuthenticationError{
		Operation: "upload_torrent",
		Err:       cause,
	}

	unwrapped := errors.Unwrap(err)
	if unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}

	// Verify errors.Is works through the chain
	wrapped := fmt.Errorf("context: %w", err)
	if !errors.Is(wrapped, cause) {
		t.Error("errors.Is() should find cause in wrapped chain")
	}
}

// TestInvalidContentError_As verifies programmatic error type detection
func TestInvalidContentError_As(t *testing.T) {
	originalErr := &InvalidContentError{
		Filename: "test.torrent",
		Reason:   "file too large",
	}

	// Wrap the error
	wrapped := fmt.Errorf("context: %w", originalErr)

	// Extract typed error using errors.As
	var target *InvalidContentError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As() should extract InvalidContentError from wrapped chain")
	}

	// Verify extracted error has expected field values
	if target.Filename != "test.torrent" {
		t.Errorf("Filename = %q, want %q", target.Filename, "test.torrent")
	}
	if target.Reason != "file too large" {
		t.Errorf("Reason = %q, want %q", target.Reason, "file too large")
	}
}

// TestNetworkError_As verifies programmatic error type detection
func TestNetworkError_As(t *testing.T) {
	originalErr := &NetworkError{
		Operation:  "upload",
		StatusCode: 503,
		APIMessage: "service unavailable",
	}

	// Wrap the error
	wrapped := fmt.Errorf("context: %w", originalErr)

	// Extract typed error using errors.As
	var target *NetworkError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As() should extract NetworkError from wrapped chain")
	}

	// Verify extracted error has expected field values
	if target.Operation != "upload" {
		t.Errorf("Operation = %q, want %q", target.Operation, "upload")
	}
	if target.StatusCode != 503 {
		t.Errorf("StatusCode = %d, want %d", target.StatusCode, 503)
	}
	if target.APIMessage != "service unavailable" {
		t.Errorf("APIMessage = %q, want %q", target.APIMessage, "service unavailable")
	}
}

// TestDirectoryError_As verifies programmatic error type detection
func TestDirectoryError_As(t *testing.T) {
	originalErr := &DirectoryError{
		DirectoryName: "itv",
		Reason:        "not found",
	}

	// Wrap the error
	wrapped := fmt.Errorf("context: %w", originalErr)

	// Extract typed error using errors.As
	var target *DirectoryError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As() should extract DirectoryError from wrapped chain")
	}

	// Verify extracted error has expected field values
	if target.DirectoryName != "itv" {
		t.Errorf("DirectoryName = %q, want %q", target.DirectoryName, "itv")
	}
	if target.Reason != "not found" {
		t.Errorf("Reason = %q, want %q", target.Reason, "not found")
	}
}

// TestAuthenticationError_As verifies programmatic error type detection
func TestAuthenticationError_As(t *testing.T) {
	originalErr := &AuthenticationError{
		Operation: "upload_torrent",
	}

	// Wrap the error
	wrapped := fmt.Errorf("context: %w", originalErr)

	// Extract typed error using errors.As
	var target *AuthenticationError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As() should extract AuthenticationError from wrapped chain")
	}

	// Verify extracted error has expected field values
	if target.Operation != "upload_torrent" {
		t.Errorf("Operation = %q, want %q", target.Operation, "upload_torrent")
	}
}

// TestErrorTypes_Nil verifies nil error handling
func TestErrorTypes_Nil(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "InvalidContentError with nil Err",
			err:  &InvalidContentError{Filename: "test.torrent", Reason: "too large", Err: nil},
		},
		{
			name: "NetworkError with nil Err",
			err:  &NetworkError{Operation: "upload", StatusCode: 500, APIMessage: "error", Err: nil},
		},
		{
			name: "DirectoryError with nil Err",
			err:  &DirectoryError{DirectoryName: "itv", Reason: "not found", Err: nil},
		},
		{
			name: "AuthenticationError with nil Err",
			err:  &AuthenticationError{Operation: "upload", Err: nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Unwrap should return nil when Err is nil
			if unwrapped := errors.Unwrap(tt.err); unwrapped != nil {
				t.Errorf("Unwrap() = %v, want nil", unwrapped)
			}

			// Error() should still work
			if errMsg := tt.err.Error(); errMsg == "" {
				t.Error("Error() should return non-empty string even when Err is nil")
			}
		})
	}
}
