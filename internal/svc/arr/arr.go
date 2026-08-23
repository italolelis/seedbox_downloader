package arr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

const (
	// downloadFolderImportedEvent is history event type 3 in both Sonarr
	// (EpisodeHistoryEventType) and Radarr (MovieHistoryEventType).
	downloadFolderImportedEvent = "3"

	// downloadFolderImportedName is the same event as it appears in the JSON.
	// The response is checked against this rather than trusting the numeric
	// filter alone: if either app ever renumbers its enum, matching on the name
	// fails closed (a transfer is never considered imported, so its files linger)
	// instead of failing open (files deleted before the import actually happened).
	downloadFolderImportedName = "downloadFolderImported"
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

// Configured reports whether this client has somewhere to talk to. Both a Sonarr
// and a Radarr client are always constructed, so one of them is typically unset.
func (c *Client) Configured() bool {
	return c.baseURL != "" && c.apiKey != ""
}

type HistoryRecord struct {
	EventType  string                 `json:"eventType"`
	DownloadID string                 `json:"downloadId"`
	Data       map[string]interface{} `json:"data"`
}

type HistoryResponse struct {
	Records      []HistoryRecord `json:"records"`
	TotalRecords int             `json:"totalRecords"`
}

// CheckImported reports whether the *arr application has imported the download
// with the given download client id.
//
// The history query is filtered server-side by download id and event type, so the
// app returns at most one record however large its history has grown. This used to
// page through the entire history 1,000 records at a time and match on a file path.
// That cost the *arr app a release-title parse for every record it returned, so the
// price scaled with total history rather than with how many transfers are being
// tracked -- on a real instance with ~2,700 records it was roughly 3.5s of Sonarr
// CPU per check, several times a minute, forever.
//
// downloadID is upper-cased because both apps store the torrent hash that way
// (DownloadId = torrent.HashString.ToUpper()) and the API filter is an exact,
// case-sensitive comparison.
func (c *Client) CheckImported(ctx context.Context, downloadID string) (bool, error) {
	if !c.Configured() || downloadID == "" {
		return false, nil
	}

	query := url.Values{
		"downloadId": {strings.ToUpper(downloadID)},
		"eventType":  {downloadFolderImportedEvent},
		"pageSize":   {"1"},
		// Sonarr's and Radarr's names for the same idea. Each ignores the other's.
		"includeSeries":  {"false"},
		"includeEpisode": {"false"},
		"includeMovie":   {"false"},
	}

	endpoint := fmt.Sprintf("%s/api/v3/history?%s", c.baseURL, query.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Api-Key", c.apiKey)

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := c.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to send request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("url: %s, status: %d", endpoint, resp.StatusCode)
	}

	var historyResponse HistoryResponse
	if err := json.NewDecoder(resp.Body).Decode(&historyResponse); err != nil {
		return false, fmt.Errorf("failed to decode response: %w", err)
	}

	for _, record := range historyResponse.Records {
		if record.EventType == downloadFolderImportedName {
			return true, nil
		}
	}

	return false, nil
}
