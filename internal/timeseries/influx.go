package timeseries

import (
	"context"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"

	"vpp-lab/internal/model"
)

type Writer struct {
	client influxdb2.Client
	org    string
	bucket string
}

func NewWriter(url, token, org, bucket string) *Writer {
	return &Writer{
		client: influxdb2.NewClient(url, token),
		org:    org,
		bucket: bucket,
	}
}

func (w *Writer) Close() {
	w.client.Close()
}

func (w *Writer) WriteTelemetry(ctx context.Context, t model.Telemetry) error {
	ts := time.Unix(t.Timestamp, 0)
	if t.Timestamp == 0 {
		ts = time.Now()
	}
	measurement := string(t.Type)
	if measurement == "" {
		measurement = "device"
	}
	p := influxdb2.NewPoint(measurement,
		map[string]string{
			"site_id":   t.SiteID,
			"device_id": t.DeviceID,
		},
		map[string]interface{}{
			"state": t.State,
			"seq":   t.Seq,
		},
		ts,
	)
	for k, v := range t.Metrics {
		p.AddField(k, v)
	}
	return w.client.WriteAPIBlocking(w.org, w.bucket).WritePoint(ctx, p)
}
