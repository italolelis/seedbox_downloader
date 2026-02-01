package putio

import (
	"context"
	"errors"
	"testing"

	"github.com/italolelis/seedbox_downloader/internal/transfer"
)

func TestValidateTorrentFilename_ValidExtensions(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		wantErr  bool
	}{
		{
			name:     "lowercase .torrent",
			filename: "test.torrent",
			wantErr:  false,
		},
		{
			name:     "uppercase .TORRENT",
			filename: "test.TORRENT",
			wantErr:  false,
		},
		{
			name:     "mixed case .Torrent",
			filename: "test.Torrent",
			wantErr:  false,
		},
		{
			name:     "invalid .txt extension",
			filename: "test.txt",
			wantErr:  true,
		},
		{
			name:     "invalid .tor extension",
			filename: "test.tor",
			wantErr:  true,
		},
		{
			name:     "no extension",
			filename: "test",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTorrentFilename(tt.filename)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTorrentFilename() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				var invalidErr *transfer.InvalidContentError
				if !errors.As(err, &invalidErr) {
					t.Errorf("expected InvalidContentError, got %T", err)
				}
				if invalidErr.Filename != tt.filename {
					t.Errorf("expected filename %q, got %q", tt.filename, invalidErr.Filename)
				}
			}
		})
	}
}

func TestAddTransferByBytes_InvalidExtension(t *testing.T) {
	client := NewClient("test-token")

	torrentBytes := []byte("fake content")
	_, err := client.AddTransferByBytes(context.Background(), torrentBytes, "test.txt", "")

	if err == nil {
		t.Fatal("expected error for invalid extension, got nil")
	}

	var invalidErr *transfer.InvalidContentError
	if !errors.As(err, &invalidErr) {
		t.Fatalf("expected InvalidContentError, got %T: %v", err, err)
	}

	if invalidErr.Filename != "test.txt" {
		t.Errorf("expected filename 'test.txt', got %q", invalidErr.Filename)
	}

	if invalidErr.Reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestAddTransferByBytes_FileTooLarge(t *testing.T) {
	client := NewClient("test-token")

	// Create 11MB of data (exceeds 10MB limit)
	torrentBytes := make([]byte, 11*1024*1024)
	_, err := client.AddTransferByBytes(context.Background(), torrentBytes, "large.torrent", "")

	if err == nil {
		t.Fatal("expected error for oversized file, got nil")
	}

	var invalidErr *transfer.InvalidContentError
	if !errors.As(err, &invalidErr) {
		t.Fatalf("expected InvalidContentError, got %T: %v", err, err)
	}

	if invalidErr.Filename != "large.torrent" {
		t.Errorf("expected filename 'large.torrent', got %q", invalidErr.Filename)
	}

	// Verify the error message mentions size
	expectedSubstring := "exceeds maximum"
	if invalidErr.Reason == "" {
		t.Error("expected non-empty reason")
	} else {
		// Check if reason contains expected substring
		found := false
		for i := 0; i+len(expectedSubstring) <= len(invalidErr.Reason); i++ {
			if invalidErr.Reason[i:i+len(expectedSubstring)] == expectedSubstring {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected reason to contain %q, got %q", expectedSubstring, invalidErr.Reason)
		}
	}
}
