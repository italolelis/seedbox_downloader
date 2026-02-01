package rest

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/italolelis/seedbox_downloader/internal/transfer"
	"github.com/stretchr/testify/require"
)

// mockPutioClient implements DownloadClient interface for testing.
type mockPutioClient struct {
	addTransferFunc          func(ctx context.Context, magnetLink, parentName string) (*transfer.Transfer, error)
	addTransferByBytesFunc   func(ctx context.Context, content []byte, filename, parentName string) (*transfer.Transfer, error)
	addTransferCalled        bool
	addTransferByBytesCalled bool
	lastMagnetLink           string
	lastFilename             string
	lastParentName           string
}

func (m *mockPutioClient) AddTransfer(ctx context.Context, magnetLink, parentName string) (*transfer.Transfer, error) {
	m.addTransferCalled = true
	m.lastMagnetLink = magnetLink
	m.lastParentName = parentName
	if m.addTransferFunc != nil {
		return m.addTransferFunc(ctx, magnetLink, parentName)
	}
	return &transfer.Transfer{ID: "mock-transfer-id", Name: "mock-transfer"}, nil
}

func (m *mockPutioClient) AddTransferByBytes(ctx context.Context, content []byte, filename, parentName string) (*transfer.Transfer, error) {
	m.addTransferByBytesCalled = true
	m.lastFilename = filename
	m.lastParentName = parentName
	if m.addTransferByBytesFunc != nil {
		return m.addTransferByBytesFunc(ctx, content, filename, parentName)
	}
	return &transfer.Transfer{ID: "mock-transfer-id", Name: "mock-transfer"}, nil
}

func (m *mockPutioClient) GetTaggedTorrents(ctx context.Context, label string) ([]*transfer.Transfer, error) {
	return []*transfer.Transfer{}, nil
}

func (m *mockPutioClient) RemoveTransfers(ctx context.Context, ids []string, deleteData bool) error {
	return nil
}

func TestValidateBencodeStructure(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		expectError bool
		errorReason string
	}{
		{
			name:        "valid torrent structure with info dict",
			data:        []byte("d4:infod4:name4:testee"), // {"info": {"name": "test"}}
			expectError: false,
		},
		{
			name:        "valid torrent with announce",
			data:        []byte("d8:announce3:url4:infod4:name4:testee"), // {"announce": "url", "info": {"name": "test"}}
			expectError: false,
		},
		{
			name:        "invalid bencode syntax - not bencode",
			data:        []byte("not bencode at all"),
			expectError: true,
			errorReason: "invalid bencode structure",
		},
		{
			name:        "invalid bencode syntax - truncated",
			data:        []byte("d4:info"), // Incomplete dictionary
			expectError: true,
			errorReason: "invalid bencode structure",
		},
		{
			name:        "root is list not dictionary",
			data:        []byte("l4:teste"), // ["test"]
			expectError: true,
			errorReason: "bencode root must be a dictionary",
		},
		{
			name:        "root is string not dictionary",
			data:        []byte("4:test"), // "test"
			expectError: true,
			errorReason: "bencode root must be a dictionary",
		},
		{
			name:        "root is integer not dictionary",
			data:        []byte("i42e"), // 42
			expectError: true,
			errorReason: "bencode root must be a dictionary",
		},
		{
			name:        "missing info field",
			data:        []byte("d4:name4:teste"), // {"name": "test"} - no "info"
			expectError: true,
			errorReason: "bencode missing required 'info' dictionary",
		},
		{
			name:        "empty dictionary",
			data:        []byte("de"), // {}
			expectError: true,
			errorReason: "bencode missing required 'info' dictionary",
		},
		{
			name:        "info is not dictionary",
			data:        []byte("d4:info4:teste"), // {"info": "test"} - info is string
			expectError: false,                    // Current implementation only checks for presence, not type
		},
		{
			name:        "empty data",
			data:        []byte{},
			expectError: true,
			errorReason: "invalid bencode structure",
		},
		{
			name:        "nil data treated as empty",
			data:        nil,
			expectError: true,
			errorReason: "invalid bencode structure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBencodeStructure(tt.data)

			if tt.expectError {
				require.Error(t, err, "expected error for case: %s", tt.name)

				// Verify error type is InvalidContentError
				var invalidErr *transfer.InvalidContentError
				require.True(t, errors.As(err, &invalidErr),
					"expected InvalidContentError, got %T", err)

				// Verify error reason contains expected substring
				require.Contains(t, invalidErr.Reason, tt.errorReason,
					"error reason should contain %q", tt.errorReason)
			} else {
				require.NoError(t, err, "unexpected error for case: %s", tt.name)
			}
		})
	}
}

func TestBase64DecodingEdgeCases(t *testing.T) {
	// Create test data with characters that will produce + or / in base64
	// This ensures URLEncoding will actually differ from StdEncoding
	testDataWithSpecialChars := []byte{0xff, 0xff, 0xff, 0xff} // Produces "////" in StdEncoding, "____" in URLEncoding

	tests := []struct {
		name        string
		input       string
		expectError bool
		description string
	}{
		{
			name:        "valid StdEncoding base64",
			input:       base64.StdEncoding.EncodeToString([]byte("d4:infod4:name4:testee")),
			expectError: false,
			description: "Standard encoding should decode successfully",
		},
		{
			name:        "wrong variant - URLEncoding with special chars",
			input:       base64.URLEncoding.EncodeToString(testDataWithSpecialChars),
			expectError: true,
			description: "URLEncoding uses -_ instead of +/ - StdEncoding decoder should reject _ characters",
		},
		{
			name:        "invalid characters",
			input:       "!!!invalid-base64!!!",
			expectError: true,
			description: "Non-base64 characters should fail",
		},
		{
			name:        "wrong padding",
			input:       "SGVsbG8gV29ybGQ", // Missing padding (should be SGVsbG8gV29ybGQ=)
			expectError: true,              // Go's StdEncoding is strict about padding
			description: "Missing padding should fail",
		},
		{
			name:        "empty string",
			input:       "",
			expectError: true, // Empty decodes to empty bytes, but bencode validation fails
			description: "Empty input produces empty bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoded, err := base64.StdEncoding.DecodeString(tt.input)

			if tt.expectError {
				// Either base64 decode fails OR bencode validation fails
				if err != nil {
					return // Base64 decode failed - expected
				}
				// If decode succeeded, bencode validation should fail
				err = validateBencodeStructure(decoded)
				require.Error(t, err, "expected bencode validation to fail for: %s", tt.description)
			} else {
				require.NoError(t, err, "base64 decode should succeed for: %s", tt.description)
				// For valid cases, also check bencode validation passes
				if len(decoded) > 0 {
					err = validateBencodeStructure(decoded)
					require.NoError(t, err, "bencode validation should pass for: %s", tt.description)
				}
			}
		})
	}
}

func TestGenerateTorrentFilename(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantLen int
		wantExt string
	}{
		{
			name:    "generates hash-based filename",
			data:    []byte("d4:infod4:name4:testee"),
			wantLen: 24, // 16 chars hash + ".torrent" (8 chars)
			wantExt: ".torrent",
		},
		{
			name:    "different content produces different filename",
			data:    []byte("d4:infod4:name7:anothere"),
			wantLen: 24,
			wantExt: ".torrent",
		},
		{
			name:    "empty content still generates filename",
			data:    []byte{},
			wantLen: 24,
			wantExt: ".torrent",
		},
	}

	var previousFilename string
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filename := generateTorrentFilename(tt.data)

			require.Len(t, filename, tt.wantLen,
				"filename should be %d characters", tt.wantLen)

			require.True(t, len(filename) > len(tt.wantExt),
				"filename should be longer than extension")

			ext := filename[len(filename)-len(tt.wantExt):]
			require.Equal(t, tt.wantExt, ext,
				"filename should end with %s", tt.wantExt)

			// Verify different content produces different filename
			if previousFilename != "" && tt.name == "different content produces different filename" {
				require.NotEqual(t, previousFilename, filename,
					"different content should produce different filename")
			}
			previousFilename = filename
		})
	}
}

func TestFormatTransmissionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name: "InvalidContentError formatting",
			err: &transfer.InvalidContentError{
				Filename: "test.torrent",
				Reason:   "invalid base64 encoding",
			},
			expected: "invalid torrent: invalid base64 encoding",
		},
		{
			name: "NetworkError formatting",
			err: &transfer.NetworkError{
				Operation:  "upload",
				StatusCode: 503,
				APIMessage: "service unavailable",
			},
			expected: "upload failed: service unavailable",
		},
		{
			name: "DirectoryError formatting",
			err: &transfer.DirectoryError{
				DirectoryName: "itv",
				Reason:        "not found",
			},
			expected: "directory error: not found",
		},
		{
			name: "AuthenticationError formatting",
			err: &transfer.AuthenticationError{
				Operation: "upload",
			},
			expected: "authentication failed",
		},
		{
			name:     "generic error formatting",
			err:      errors.New("something went wrong"),
			expected: "error: something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTransmissionError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}
}
