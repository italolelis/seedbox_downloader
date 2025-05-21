package deluge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		apiPath  string
		username string
		password string
	}{
		{"basic", "http://localhost", "/api", "user", "pass"},
		{"empty", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.baseURL, tt.apiPath, tt.username, tt.password)
			assert.Equal(t, tt.baseURL, client.BaseURL)
			assert.Equal(t, tt.apiPath, client.APIPath)
			assert.Equal(t, tt.username, client.Username)
			assert.Equal(t, tt.password, client.Password)
			assert.NotNil(t, client.httpClient)
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

			client := NewClient(ts.URL, "", "user", "pass")
			err := client.Authenticate(context.Background())
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectErrorMsg)
		})
	}
}

func TestGetTaggedTorrents(t *testing.T) {
	tests := []struct {
		name         string
		jsonResp     map[string]interface{}
		tag          string
		expectCount  int
		expectFields map[string]string
	}{
		{
			"single match",
			map[string]interface{}{
				"result": map[string]interface{}{
					"abc123": map[string]interface{}{
						"label":     "mytag",
						"progress":  100.0,
						"name":      "file1",
						"save_path": "/downloads",
						"files": []interface{}{
							map[string]interface{}{"path": "file1.mkv"},
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
			map[string]interface{}{
				"result": map[string]interface{}{},
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

			client := NewClient(ts.URL, "", "user", "pass")
			client.cookie = "testcookie" // simulate authenticated

			torrents, err := client.GetTaggedTorrents(context.Background(), tt.tag)
			assert.NoError(t, err)
			assert.Len(t, torrents, tt.expectCount)
			if tt.expectCount > 0 && tt.expectFields != nil {
				assert.Equal(t, tt.expectFields["ID"], torrents[0].ID)
				assert.Equal(t, tt.expectFields["FileName"], torrents[0].FileName)
				assert.Equal(t, tt.expectFields["Label"], torrents[0].Label)
				assert.Equal(t, tt.expectFields["SavePath"], torrents[0].SavePath)
			}
		})
	}
}
