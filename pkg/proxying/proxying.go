package proxying

import (
	"context"
	"io"
	"log/slog"

	repb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/buildbarn/bb-storage/pkg/util"
	bpb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

type Server struct {
	ProxyTarget               grpc.ClientConnInterface
	ActionCache               repb.ActionCacheClient
	Capabilities              repb.CapabilitiesClient
	ContentAddressableStorage repb.ContentAddressableStorageClient
	Execution                 repb.ExecutionClient
	ByteStream                bpb.ByteStreamClient

	repb.UnimplementedActionCacheServer
	repb.UnimplementedCapabilitiesServer
	repb.UnimplementedContentAddressableStorageServer
	repb.UnimplementedExecutionServer
	bpb.UnimplementedByteStreamServer
}

func (s *Server) GetCapabilities(ctx context.Context, req *repb.GetCapabilitiesRequest) (res *repb.ServerCapabilities, err error) {
	defer func() {
		if err != nil {
			slog.Error("GetCapabilities error", slog.Any("err", err))
		}
	}()
	return s.Capabilities.GetCapabilities(ctx, req)
}

func (s *Server) GetActionResult(ctx context.Context, req *repb.GetActionResultRequest) (res *repb.ActionResult, err error) {
	defer func() {
		if err != nil {
			slog.Error("GetActionResult error", slog.Any("err", err))
		}
	}()
	return s.ActionCache.GetActionResult(ctx, req)
}

func (s *Server) UpdateActionResult(ctx context.Context, req *repb.UpdateActionResultRequest) (res *repb.ActionResult, err error) {
	defer func() {
		if err != nil {
			slog.Error("UpdateActionResult error", slog.Any("err", err))
		}
	}()
	return s.ActionCache.UpdateActionResult(ctx, req)
}

func (s *Server) FindMissingBlobs(ctx context.Context, req *repb.FindMissingBlobsRequest) (res *repb.FindMissingBlobsResponse, err error) {
	defer func() {
		if err != nil {
			slog.Error("FindMissingBlobs error", slog.Any("err", err))
		}
	}()
	return s.ContentAddressableStorage.FindMissingBlobs(ctx, req)
}

func (s *Server) BatchUpdateBlobs(ctx context.Context, req *repb.BatchUpdateBlobsRequest) (res *repb.BatchUpdateBlobsResponse, err error) {
	defer func() {
		if err != nil {
			slog.Error("BatchUpdateBlobs error", slog.Any("err", err))
		}
	}()
	return s.ContentAddressableStorage.BatchUpdateBlobs(ctx, req)
}

func (s *Server) BatchReadBlobs(ctx context.Context, req *repb.BatchReadBlobsRequest) (res *repb.BatchReadBlobsResponse, err error) {
	defer func() {
		if err != nil {
			slog.Error("BatchReadBlobs error", slog.Any("err", err))
		}
	}()
	return s.BatchReadBlobs(ctx, req)
}

func (s *Server) GetTree(req *repb.GetTreeRequest, srv repb.ContentAddressableStorage_GetTreeServer) error {
	client, err := s.ContentAddressableStorage.GetTree(srv.Context(), req)
	if err != nil {
		slog.Error("failed to create client", slog.String("method", "ContentAddressableStorage.GetTree"), slog.Any("err", err))
		return util.StatusWrapWithCode(err, codes.FailedPrecondition, "failed to create GetTree client")
	}

	for {
		res, err := client.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}

			slog.Error("failed to receive from backend", slog.String("method", "ContentAddressableStorage.GetTree"), slog.Any("err", err))
			return err
		}

		if err := srv.Send(res); err != nil {
			slog.Error("failed to send to client", slog.String("method", "ContentAddressableStorage.GetTree"), slog.Any("err", err))
			return err
		}
	}
}

func (s *Server) Execute(req *repb.ExecuteRequest, srv repb.Execution_ExecuteServer) error {
	client, err := s.Execution.Execute(srv.Context(), req)
	if err != nil {
		slog.Error("failed to create client", slog.String("method", "Execution.Execute"), slog.Any("err", err))
		return util.StatusWrapWithCode(err, codes.FailedPrecondition, "failed to create Execute client")
	}
	for {
		res, err := client.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}

			slog.Error("failed to receive from backend", slog.String("method", "Execution.Execute"), slog.Any("err", err))
			return err
		}

		if err := srv.Send(res); err != nil {
			slog.Error("failed to send to client", slog.String("method", "Execution.Execute"), slog.Any("err", err))
			return err
		}
	}
}

func (s *Server) WaitExecution(req *repb.WaitExecutionRequest, srv repb.Execution_WaitExecutionServer) error {
	client, err := s.Execution.WaitExecution(srv.Context(), req)
	if err != nil {
		slog.Error("failed to create client", slog.String("method", "Execution.WaitExecution"), slog.Any("err", err))
		return util.StatusWrapWithCode(err, codes.FailedPrecondition, "failed to create WaitExecution client")
	}
	for {
		res, err := client.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}

			slog.Error("failed to receive from backend", slog.String("method", "Execution.WaitExecution"), slog.Any("err", err))
			return err
		}

		if err := srv.Send(res); err != nil {
			slog.Error("failed to send to client", slog.String("method", "Execution.WaitExecution"), slog.Any("err", err))
			return err
		}
	}
}

func (s *Server) Read(req *bpb.ReadRequest, srv bpb.ByteStream_ReadServer) error {
	client, err := s.ByteStream.Read(srv.Context(), req)
	if err != nil {
		slog.Error("failed to create client", slog.String("method", "ByteStream.Read"), slog.Any("err", err))
		return util.StatusWrapWithCode(err, codes.FailedPrecondition, "failed to create Read client")
	}
	for {
		res, err := client.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}

			slog.Error("failed to receive from backend", slog.String("method", "ByteStream.Read"), slog.Any("err", err))
			return err
		}

		if err := srv.Send(res); err != nil {
			slog.Error("failed to send to client", slog.String("method", "ByteStream.Read"), slog.Any("err", err))
			return err
		}
	}
}

func (s *Server) Write(srv bpb.ByteStream_WriteServer) error {
	stream, err := s.ByteStream.Write(srv.Context())
	if err != nil {
		slog.Error("failed to create client", slog.String("method", "ByteStream.Write"), slog.Any("err", err))
		return util.StatusWrapWithCode(err, codes.FailedPrecondition, "failed to open Write stream")
	}
	for {
		req, err := srv.Recv()
		if err == io.EOF {
			res, err := stream.CloseAndRecv()
			if err != nil {
				slog.Error("CloseAndRecv from client failed", slog.String("method", "ByteStream.Write"), slog.Any("err", err))
				return err
			}
			if err := srv.SendAndClose(res); err != nil {
				slog.Error("SendAndClose to backend failed", slog.String("method", "ByteStream.Write"), slog.Any("err", err))
				return err
			}
			return nil
		} else if err != nil {
			slog.Error("Recv from client failed", slog.String("method", "ByteStream.Write"), slog.Any("err", err))
			return err
		}
		if err := stream.Send(req); err != nil {
			slog.Error("Send to backend failed", slog.String("method", "ByteStream.Write"), slog.Any("err", err))
			return err
		}
	}
}

func (s *Server) QueryWriteStatus(ctx context.Context, req *bpb.QueryWriteStatusRequest) (res *bpb.QueryWriteStatusResponse, err error) {
	defer func() {
		if err != nil {
			slog.Error("QueryWriteStatus error", slog.Any("err", err))
		}
	}()
	return s.ByteStream.QueryWriteStatus(ctx, req)
}
