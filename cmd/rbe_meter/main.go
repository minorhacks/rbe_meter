package main

import (
	"context"
	"fmt"
	"os"

	repb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	bbgrpc "github.com/buildbarn/bb-storage/pkg/grpc"
	"github.com/buildbarn/bb-storage/pkg/program"
	gpb "github.com/buildbarn/bb-storage/pkg/proto/configuration/grpc"
	"github.com/buildbarn/bb-storage/pkg/util"
	bpb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	mpb "github.com/minorhacks/rbe_meter/pkg/proto/configuration/rbe_meter"
	"github.com/minorhacks/rbe_meter/pkg/proxying"
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

	clientFactory := bbgrpc.NewBaseClientFactory(bbgrpc.BaseClientDialer, nil, nil)
	client, err := clientFactory.NewClientFromConfiguration(config.GetRbeBackend())
	if err != nil {
		return util.StatusWrapf(err, "failed to create new client from simple factory")
	}

	proxy := &proxying.Server{
		ProxyTarget:               client,
		ActionCache:               repb.NewActionCacheClient(client),
		Capabilities:              repb.NewCapabilitiesClient(client),
		ContentAddressableStorage: repb.NewContentAddressableStorageClient(client),
		Execution:                 repb.NewExecutionClient(client),
		ByteStream: bpb.NewByteStreamClient(client),
	}

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
