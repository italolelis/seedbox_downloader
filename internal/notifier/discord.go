package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type Notifier interface {
	Notify(content string) error
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
