package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	repb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	bbgrpc "github.com/buildbarn/bb-storage/pkg/grpc"
	"github.com/buildbarn/bb-storage/pkg/program"
	gpb "github.com/buildbarn/bb-storage/pkg/proto/configuration/grpc"
	"github.com/buildbarn/bb-storage/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	bpb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/minorhacks/rbe_meter/pkg/metered"
	"github.com/minorhacks/rbe_meter/pkg/metrics"
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

	interceptor := metered.NewInterceptor(reg)

	clientFactory := bbgrpc.NewBaseClientFactory(
		bbgrpc.BaseClientDialer,
		[]grpc.UnaryClientInterceptor{interceptor.Intercept},
		[]grpc.StreamClientInterceptor{interceptor.StreamIntercept},
	)
	client, err := clientFactory.NewClientFromConfiguration(config.GetRbeBackend())
	if err != nil {
		return util.StatusWrapf(err, "failed to create new client from simple factory")
	}

	proxy := metered.NewServer(client, reg)

	pusher, err := metrics.FromConfig(config.PushMetrics, reg)
	if err != nil {
		return fmt.Errorf("creating metrics pusher: %w", err)
	}
	go pusher.PushLoop(ctx)

	if err := bbgrpc.NewServersFromConfigurationAndServe(
		[]*gpb.ServerConfiguration{config.GetGrpcServer()},
		registerServices(proxy),
		deps,
	); err != nil {
		return fmt.Errorf("while starting grpc servers: %w", err)
	}
	slog.Info("proxying REAPI requests to backend")

	select {
	case <-ctx.Done():
	}
	return nil
}

func main() {
	app := &meterApp{}
	program.RunMain(app.Run)
}
