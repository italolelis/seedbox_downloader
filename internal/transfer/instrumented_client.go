package transfer

import (
	"context"
	"io"

	"github.com/italolelis/seedbox_downloader/internal/telemetry"
)

// SeedingState is how a transfer is seeding on the seedbox right now.
type SeedingState struct {
	// Ratio is uploaded over downloaded; 0 when nothing was downloaded.
	Ratio float64
	// SecondsSeeding is how long the seedbox has seeded it.
	SecondsSeeding int64
	// Seeding is false once the seedbox has stopped seeding it, after which
	// neither number above will grow.
	Seeding bool
}

// TransferInfoer is an optional capability for download clients that support
// querying a transfer's live seeding state from the remote service.
type TransferInfoer interface {
	GetTransferInfo(ctx context.Context, transferID string) (state SeedingState, found bool, err error)
}

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

// GetTransferInfo retrieves a transfer's live seeding state if the underlying client supports it.
// Returns a zero state and false if the underlying client does not implement TransferInfoer.
func (c *InstrumentedDownloadClient) GetTransferInfo(ctx context.Context, transferID string) (SeedingState, bool, error) {
	infoer, ok := c.client.(TransferInfoer)
	if !ok {
		return SeedingState{}, false, nil
	}

	return infoer.GetTransferInfo(ctx, transferID)
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
func (c *InstrumentedTransferClient) AddTransfer(ctx context.Context, url, label, category string) (*Transfer, error) {
	var result *Transfer

	var err error

	instrumentedErr := c.telemetry.InstrumentClientOperation(ctx, c.clientType, "add_transfer", func(ctx context.Context) error {
		result, err = c.client.AddTransfer(ctx, url, label, category)

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
	ctx context.Context, torrentBytes []byte, filename, label, category string,
) (*Transfer, error) {
	var result *Transfer

	var err error

	instrumentedErr := c.telemetry.InstrumentClientOperation(ctx, c.clientType, "add_transfer_by_bytes", func(ctx context.Context) error {
		result, err = c.client.AddTransferByBytes(ctx, torrentBytes, filename, label, category)

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
