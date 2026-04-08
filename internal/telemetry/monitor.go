package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type MonitoredChannel struct {
	Name string
	Len  func() int
	Cap  func() int
}

func (t *Telemetry) MonitorChannels(ctx context.Context, channels []MonitoredChannel, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			for _, ch := range channels {
				c := ch.Cap()
				if c == 0 {
					continue
				}
				utilization := float64(ch.Len()) / float64(c)
				t.Metrics.ChannelUtilization.Record(ctx, utilization,
					metric.WithAttributes(attribute.String("channel", ch.Name)))
			}
		case <-ctx.Done():
			return
		}
	}
}
