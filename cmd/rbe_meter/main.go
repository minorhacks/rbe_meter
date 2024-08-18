package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	repb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	bbgrpc "github.com/buildbarn/bb-storage/pkg/grpc"
	"github.com/buildbarn/bb-storage/pkg/program"
	gpb "github.com/buildbarn/bb-storage/pkg/proto/configuration/grpc"
	"github.com/buildbarn/bb-storage/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/push"
	"github.com/prometheus/common/expfmt"
	bpb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/minorhacks/rbe_meter/pkg/metered"
	mpb "github.com/minorhacks/rbe_meter/pkg/proto/configuration/rbe_meter"
)

type meterApp struct{}

func registerServices(impl any) func(grpc.ServiceRegistrar) {
	return func(sr grpc.ServiceRegistrar) {
		repb.RegisterActionCacheServer(sr, impl.(repb.ActionCacheServer))
		repb.RegisterCapabilitiesServer(sr, impl.(repb.CapabilitiesServer))
		repb.RegisterContentAddressableStorageServer(sr, impl.(repb.ContentAddressableStorageServer))
		repb.RegisterExecutionServer(sr, impl.(repb.ExecutionServer))
		bpb.RegisterByteStreamServer(sr, impl.(bpb.ByteStreamServer))
	}
}

func (a *meterApp) Run(ctx context.Context, siblings program.Group, deps program.Group) error {
	if len(os.Args) != 2 {
		return status.Error(codes.InvalidArgument, "Usage: rbe_meter config.jsonnet")
	}
	var config mpb.ApplicationConfiguration
	if err := util.UnmarshalConfigurationFromFile(os.Args[1], &config); err != nil {
		return util.StatusWrapf(err, "failed to read config from file %q", os.Args[1])
	}

	reg := prometheus.NewRegistry()

	clientFactory := bbgrpc.NewBaseClientFactory(bbgrpc.BaseClientDialer, nil, nil)
	client, err := clientFactory.NewClientFromConfiguration(config.GetRbeBackend())
	if err != nil {
		return util.StatusWrapf(err, "failed to create new client from simple factory")
	}

	proxy := metered.NewServer(client, reg)
	pusher := push.New("http://localhost:8429/api/v1/import/prometheus", "rbe_meter").
		Collector(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})).
		Collector(collectors.NewGoCollector()).
		Gatherer(reg).
		Format(expfmt.NewFormat(expfmt.TypeTextPlain))

	go func() {
		tick := time.NewTicker(33 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return

			case <-tick.C:
				if err := pusher.PushContext(ctx); err != nil {
					slog.Error("metrics push failed", slog.Any("err", err))
				}
			}
		}
	}()

	if err := bbgrpc.NewServersFromConfigurationAndServe(
		[]*gpb.ServerConfiguration{config.GetGrpcServer()},
		registerServices(proxy),
		deps,
	); err != nil {
		return fmt.Errorf("while starting grpc servers: %w", err)
	}
	fmt.Println("rbe_meter")

	select {
	case <-ctx.Done():
	}
	return nil
}

func main() {
	app := &meterApp{}
	program.RunMain(app.Run)
}
