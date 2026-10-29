package test

import (
	"context"
	"testing"
	"time"

	"github.com/italolelis/seedbox_downloader/internal/downloader"
	"github.com/italolelis/seedbox_downloader/internal/logctx"
	"github.com/italolelis/seedbox_downloader/internal/telemetry"
	"github.com/italolelis/seedbox_downloader/internal/transfer"
	"github.com/italolelis/seedbox_downloader/test/seedbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// The dashboard's download panels were empty in production: the instrumentation
// existed, but nothing on the download path ever called it. Each downloaded or
// failed transfer must now be counted, with its duration.
func TestDownloads_AreCountedByOutcome(t *testing.T) {
	sb := seedbox.New(t, "itv",
		seedbox.Transfer{Name: "Good", Root: seedbox.Entry{Name: "good.mkv", Content: "arrives fine"}},
		seedbox.Transfer{Name: "Bad", Root: seedbox.Entry{Name: "bad.mkv", Content: "never arrives", Status: 500}},
	)

	reader := sdkmetric.NewManualReader()
	tel, err := telemetry.New(context.Background(), telemetry.Config{ServiceName: "test", Reader: reader})
	require.NoError(t, err)

	client := sb.Client()
	dl := downloader.NewDownloader(t.TempDir(), 5, client, client, nil, tel)

	ctx, cancel := context.WithCancel(logctx.WithLogger(context.Background(), testLogger()))
	defer cancel()

	queue := make(chan *transfer.Transfer)
	dl.WatchDownloads(ctx, queue)

	finished, failed := collect(dl.OnTransferDownloadFinished), collect(dl.OnTransferDownloadError)

	transfers := fetch(t, sb)
	queue <- byName(t, transfers, "Good")
	queue <- byName(t, transfers, "Bad")

	for _, ch := range []<-chan *transfer.Transfer{finished, failed} {
		select {
		case <-ch:
		case <-time.After(wedgeTimeout):
			t.Fatal("a transfer never reported its outcome")
		}
	}

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	assert.Equal(t, map[string]int64{"success": 1, "error": 1}, countsByStatus(rm, "downloads.total"))
	assert.EqualValues(t, 2, histogramCount(rm, "downloads.duration"))
}

func countsByStatus(rm metricdata.ResourceMetrics, name string) map[string]int64 {
	counts := map[string]int64{}

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if m.Name != name || !ok {
				continue
			}

			for _, dp := range sum.DataPoints {
				status, _ := dp.Attributes.Value(attribute.Key("status"))
				counts[status.AsString()] += dp.Value
			}
		}
	}

	return counts
}

func histogramCount(rm metricdata.ResourceMetrics, name string) uint64 {
	var total uint64

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if m.Name != name || !ok {
				continue
			}

			for _, dp := range hist.DataPoints {
				total += dp.Count
			}
		}
	}

	return total
}
