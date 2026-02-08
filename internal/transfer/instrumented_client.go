package transfer

import (
	"context"
	"io"

	"github.com/italolelis/seedbox_downloader/internal/telemetry"
)

// InstrumentedDownloadClient wraps DownloadClient with telemetry.
type InstrumentedDownloadClient struct {
	client     DownloadClient
	telemetry  *telemetry.Telemetry
	clientType string
}

// NewInstrumentedDownloadClient creates a new instrumented download client.
func NewInstrumentedDownloadClient(client DownloadClient, tel *telemetry.Telemetry, clientType string) *InstrumentedDownloadClient {
	return &InstrumentedDownloadClient{
		client:     client,
		telemetry:  tel,
		clientType: clientType,
	}
}

// Authenticate authenticates with the download client with telemetry.
func (c *InstrumentedDownloadClient) Authenticate(ctx context.Context) error {
	return c.telemetry.InstrumentClientOperation(ctx, c.clientType, "authenticate", func(ctx context.Context) error {
		return c.client.Authenticate(ctx)
	})
}

// GetTaggedTorrents retrieves tagged torrents with telemetry.
func (c *InstrumentedDownloadClient) GetTaggedTorrents(ctx context.Context, label string) ([]*Transfer, error) {
	var result []*Transfer

	var err error

	instrumentedErr := c.telemetry.InstrumentClientOperation(ctx, c.clientType, "get_tagged_torrents", func(ctx context.Context) error {
		result, err = c.client.GetTaggedTorrents(ctx, label)

		return err
	})

	if instrumentedErr != nil {
		return nil, instrumentedErr
	}

	return result, nil
}

// GrabFile grabs a file with telemetry.
func (c *InstrumentedDownloadClient) GrabFile(ctx context.Context, file *File) (io.ReadCloser, error) {
	var result io.ReadCloser

	var err error

	instrumentedErr := c.telemetry.InstrumentClientOperation(ctx, c.clientType, "grab_file", func(ctx context.Context) error {
		result, err = c.client.GrabFile(ctx, file)

		return err
	})

	if instrumentedErr != nil {
		return nil, instrumentedErr
	}

	return result, nil
}

// InstrumentedTransferClient wraps TransferClient with telemetry.
type InstrumentedTransferClient struct {
	client     TransferClient
	telemetry  *telemetry.Telemetry
	clientType string
}

// NewInstrumentedTransferClient creates a new instrumented transfer client.
func NewInstrumentedTransferClient(client TransferClient, tel *telemetry.Telemetry, clientType string) *InstrumentedTransferClient {
	return &InstrumentedTransferClient{
		client:     client,
		telemetry:  tel,
		clientType: clientType,
	}
}

// AddTransfer adds a transfer with telemetry.
func (c *InstrumentedTransferClient) AddTransfer(ctx context.Context, url string, downloadDir string) (*Transfer, error) {
	var result *Transfer

	var err error

	instrumentedErr := c.telemetry.InstrumentClientOperation(ctx, c.clientType, "add_transfer", func(ctx context.Context) error {
		result, err = c.client.AddTransfer(ctx, url, downloadDir)

		return err
	})

	if instrumentedErr != nil {
		return nil, instrumentedErr
	}

	c.telemetry.RecordTransfer(ctx, "add", "success")

	return result, nil
}

// AddTransferByBytes adds a transfer from .torrent file bytes with telemetry.
func (c *InstrumentedTransferClient) AddTransferByBytes(
	ctx context.Context, torrentBytes []byte, filename string, downloadDir string,
) (*Transfer, error) {
	var result *Transfer

	var err error

	instrumentedErr := c.telemetry.InstrumentClientOperation(ctx, c.clientType, "add_transfer_by_bytes", func(ctx context.Context) error {
		result, err = c.client.AddTransferByBytes(ctx, torrentBytes, filename, downloadDir)

		return err
	})

	if instrumentedErr != nil {
		return nil, instrumentedErr
	}

	c.telemetry.RecordTransfer(ctx, "add", "success")

	return result, nil
}

// RemoveTransfers removes transfers with telemetry.
func (c *InstrumentedTransferClient) RemoveTransfers(ctx context.Context, transferIDs []string, deleteLocalData bool) error {
	err := c.telemetry.InstrumentClientOperation(ctx, c.clientType, "remove_transfers", func(ctx context.Context) error {
		return c.client.RemoveTransfers(ctx, transferIDs, deleteLocalData)
	})

	status := "success"
	if err != nil {
		status = "error"
	}

	for range transferIDs {
		c.telemetry.RecordTransfer(ctx, "remove", status)
	}

	return err
}
