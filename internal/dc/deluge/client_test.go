package deluge_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/italolelis/seedbox_downloader/internal/dc/deluge"
	"github.com/stretchr/testify/assert"
)

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
			map[string]string{"ID": "abc123", "FileName": "file1.mkv", "Label": "mytag", "SavePath": "/downloads"},
		},
		{
			"no match",
			map[string]any{
				"result": map[string]any{},
				"error":  nil,
				"id":     2,
			},
			"othertag",
			0,
			nil,
		},
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
			assert.NoError(t, err)
			assert.Len(t, torrents, tt.expectCount)
			if tt.expectCount > 0 && tt.expectFields != nil {
				assert.Equal(t, tt.expectFields["ID"], torrents[0].ID)
				assert.Equal(t, tt.expectFields["Label"], torrents[0].Label)
				assert.Equal(t, tt.expectFields["SavePath"], torrents[0].SavePath)
			}
		})
	}
}
