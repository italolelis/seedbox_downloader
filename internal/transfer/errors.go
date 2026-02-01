package transfer

import "fmt"

// InvalidContentError represents errors related to malformed or invalid torrent content.
// This includes files exceeding size limits, missing .torrent extensions, or content
// rejected by the Put.io service.
type InvalidContentError struct {
	Filename string // Name of the file that failed validation
	Reason   string // Human-readable explanation of why the content is invalid
	Err      error  // Underlying error, if any
}

func (e *InvalidContentError) Error() string {
	return fmt.Sprintf("invalid torrent content in %s: %s", e.Filename, e.Reason)
}

func (e *InvalidContentError) Unwrap() error {
	return e.Err
}

// NetworkError represents network failures and API errors including 5xx responses,
// connection timeouts, and rate limiting.
type NetworkError struct {
	Operation  string // The operation that failed (e.g., "upload_torrent", "add_transfer")
	StatusCode int    // HTTP status code, if applicable (0 for non-HTTP errors)
	APIMessage string // Error message from the API or network layer
	Err        error  // Underlying error, if any
}

func (e *NetworkError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("network error during %s (HTTP %d): %s", e.Operation, e.StatusCode, e.APIMessage)
	}
	return fmt.Sprintf("network error during %s: %s", e.Operation, e.APIMessage)
}

func (e *NetworkError) Unwrap() error {
	return e.Err
}

// DirectoryError represents failures in directory resolution including directory
// not found, invalid directory paths, or access denied scenarios.
type DirectoryError struct {
	DirectoryName string // The directory name that caused the error
	Reason        string // Human-readable explanation of the directory error
	Err           error  // Underlying error, if any
}

func (e *DirectoryError) Error() string {
	return fmt.Sprintf("directory error for '%s': %s", e.DirectoryName, e.Reason)
}

func (e *DirectoryError) Unwrap() error {
	return e.Err
}

// AuthenticationError represents authentication and authorization failures
// including 401 Unauthorized and 403 Forbidden responses.
type AuthenticationError struct {
	Operation string // The operation that required authentication
	Err       error  // Underlying error, if any
}

func (e *AuthenticationError) Error() string {
	return fmt.Sprintf("authentication failed during %s", e.Operation)
}

func (e *AuthenticationError) Unwrap() error {
	return e.Err
}
