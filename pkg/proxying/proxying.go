package proxying

import (
	"context"

	repb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	bpb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	repb.UnimplementedActionCacheServer
	repb.UnimplementedCapabilitiesServer
	repb.UnimplementedContentAddressableStorageServer
	repb.UnimplementedExecutionServer
	bpb.UnimplementedByteStreamServer
}

func (s *Server) GetActionResult(ctx context.Context, req *repb.GetActionResultRequest) (*repb.ActionResult, error) {
	return nil, status.Error(codes.Unimplemented, "GetActionResult not implemented")
}

func (s *Server) UpdateActionResult(ctx context.Context, req *repb.UpdateActionResultRequest) (*repb.ActionResult, error) {
	return nil, status.Error(codes.Unimplemented, "UpdateActionResult not implemented")
}

func (s *Server) FindMissingBlobs(ctx context.Context, req *repb.FindMissingBlobsRequest) (*repb.FindMissingBlobsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "FindMissingBlobs not implemented")
}

func (s *Server) BatchUpdateBlobs(ctx context.Context, req *repb.BatchUpdateBlobsRequest) (*repb.BatchUpdateBlobsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "BatchUpdateBlobs not implemented")
}

func (s *Server) BatchReadBlobs(ctx context.Context, req *repb.BatchReadBlobsRequest) (*repb.BatchReadBlobsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "BatchReadBlobs not implemented")
}

func (s *Server) GetTree(req *repb.GetTreeRequest, srv repb.ContentAddressableStorage_GetTreeServer) error {
	return status.Error(codes.Unimplemented, "GetTree not implemented")
}

func (s *Server) Execute(req *repb.ExecuteRequest, srv repb.Execution_ExecuteServer) error {
	return status.Error(codes.Unimplemented, "Execute not implemented")
}

func (s *Server) WaitExecution(req *repb.WaitExecutionRequest, srv repb.Execution_WaitExecutionServer) error {
	return status.Error(codes.Unimplemented, "WaitExecution not implemented")
}

func (s *Server) Read(req *bpb.ReadRequest, srv bpb.ByteStream_ReadServer) error {
	return status.Error(codes.Unimplemented, "Read not implemented")
}

func (s *Server) Write(srv bpb.ByteStream_WriteServer) error {
	return status.Error(codes.Unimplemented, "Write not implemented")
}

func (s *Server) QueryWriteStatus(ctx context.Context, req *bpb.QueryWriteStatusRequest) (*bpb.QueryWriteStatusResponse, error) {
	return nil, status.Error(codes.Unimplemented, "QueryWriteStatus not implemented")
}
