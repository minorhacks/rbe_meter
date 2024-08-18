package metrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/push"
	"github.com/prometheus/common/expfmt"

	mpb "github.com/minorhacks/rbe_meter/pkg/proto/configuration/rbe_meter"
)

type IntervalPush struct {
	pusher         *push.Pusher
	scrapeInterval time.Duration
}

func (p *IntervalPush) PushLoop(ctx context.Context) {
	tick := time.NewTicker(p.scrapeInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return

		case <-tick.C:
			if err := p.pusher.PushContext(ctx); err != nil {
				slog.Error("metrics push failed", slog.Any("err", err))
			}
		}
	}
}

func FromConfig(config *mpb.PushMetricsConfiguration, gatherer prometheus.Gatherer) (*IntervalPush, error) {
	p := push.New(config.GetPushUrl(), config.GetJobName()).
		Collector(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})).
		Collector(collectors.NewGoCollector()).
		Gatherer(gatherer).
		Format(expfmt.NewFormat(expfmt.TypeTextPlain))

	return &IntervalPush{
		pusher:         p,
		scrapeInterval: time.Duration(config.GetScrapeIntervalMs()) * time.Millisecond,
	}, nil
}
