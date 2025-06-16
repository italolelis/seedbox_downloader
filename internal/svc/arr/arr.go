package arr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client represents an *arr API client.
type Client struct {
	client  *http.Client
	apiKey  string
	baseURL string
}

// NewClient creates a new *arr API client.
func NewClient(apiKey, baseURL string) *Client {
	return &Client{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		apiKey:  apiKey,
		baseURL: baseURL,
	}
}

type HistoryRecord struct {
	EventType string                 `json:"eventType"`
	Data      map[string]interface{} `json:"data"`
}

type HistoryResponse struct {
	Records      []HistoryRecord `json:"records"`
	TotalRecords int             `json:"totalRecords"`
}

// CheckImported checks if a target path has been imported into the *arr application.
func (c *Client) CheckImported(target string) (bool, error) {
	inspected := 0
	page := 0

	for {
		url := fmt.Sprintf("%s/api/v3/history?includeSeries=false&includeEpisode=false&page=%d&pageSize=1000", c.baseURL, page)

		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return false, fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("X-Api-Key", c.apiKey)

		resp, err := c.client.Do(req)
		if err != nil {
			return false, fmt.Errorf("failed to send request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return false, fmt.Errorf("url: %s, status: %d", url, resp.StatusCode)
		}

		var historyResponse HistoryResponse
		if err := json.NewDecoder(resp.Body).Decode(&historyResponse); err != nil {
			return false, fmt.Errorf("failed to decode response: %w", err)
		}

		for _, record := range historyResponse.Records {
			if record.EventType == "downloadFolderImported" {
				if droppedPath, ok := record.Data["droppedPath"].(string); ok && droppedPath == target {
					return true, nil
				}
			}

			inspected++
		}

		if historyResponse.TotalRecords > inspected {
			page++
		} else {
			return false, nil
		}
	}
}
