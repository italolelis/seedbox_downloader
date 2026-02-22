package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// Embed represents a Discord embed message with structured fields.
type Embed struct {
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Color       int          `json:"color"`
	Fields      []EmbedField `json:"fields"`
	Timestamp   string       `json:"timestamp,omitempty"`
}

// EmbedField represents a single field in a Discord embed.
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type Notifier interface {
	Notify(content string) error
	NotifyEmbed(embed Embed) error
}

type DiscordNotifier struct {
	WebhookURL string
}

func (d *DiscordNotifier) Notify(content string) error {
	if d.WebhookURL == "" {
		return fmt.Errorf("webhook URL is not set")
	}

	payload := map[string]string{"content": content}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := http.Post(d.WebhookURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook failed with status %d", resp.StatusCode)
	}

	return nil
}

// NotifyEmbed sends a Discord embed notification to the configured webhook URL.
func (d *DiscordNotifier) NotifyEmbed(embed Embed) error {
	if d.WebhookURL == "" {
		return fmt.Errorf("webhook URL is not set")
	}

	payload := map[string][]Embed{"embeds": {embed}}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal embed payload: %w", err)
	}

	resp, err := http.Post(d.WebhookURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to send embed request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("embed webhook failed with status %d", resp.StatusCode)
	}

	return nil
}
