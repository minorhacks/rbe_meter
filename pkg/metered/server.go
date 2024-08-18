// Package metered provides (an) implementation
package metered

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	repb "github.com/bazelbuild/remote-apis/build/bazel/remote/execution/v2"
	"github.com/buildbarn/bb-storage/pkg/util"
	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	bpb "google.golang.org/genproto/googleapis/bytestream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Server struct {
	ProxyTarget               grpc.ClientConnInterface
	ActionCache               repb.ActionCacheClient
	Capabilities              repb.CapabilitiesClient
	ContentAddressableStorage repb.ContentAddressableStorageClient
	Execution                 repb.ExecutionClient
	ByteStream                bpb.ByteStreamClient

	fmbBlobSizeBytes      *prometheus.HistogramVec
	fmbBatchSizeBlobCount *prometheus.HistogramVec
	fmbBatchSizeBytes     *prometheus.HistogramVec
	bsrStartedBytes       *prometheus.CounterVec
	bsrCompletedBytes     *prometheus.CounterVec
	bsrBlobReadTput       *prometheus.HistogramVec

	repb.UnimplementedActionCacheServer
	repb.UnimplementedCapabilitiesServer
	repb.UnimplementedContentAddressableStorageServer
	repb.UnimplementedExecutionServer
	bpb.UnimplementedByteStreamServer
}

func NewServer(client grpc.ClientConnInterface, reg prometheus.Registerer) *Server {
	srv := &Server{
		ProxyTarget:               client,
		ActionCache:               repb.NewActionCacheClient(client),
		Capabilities:              repb.NewCapabilitiesClient(client),
		ContentAddressableStorage: repb.NewContentAddressableStorageClient(client),
		Execution:                 repb.NewExecutionClient(client),
		ByteStream:                bpb.NewByteStreamClient(client),

		fmbBlobSizeBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "rbemeter",
			Subsystem: "find_missing_blobs",
			Name:      "blob_size_bytes",
			Buckets:   prometheus.ExponentialBuckets(1, 4, 18),
		}, []string{"inv_id", "result"}),
		fmbBatchSizeBlobCount: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "rbemeter",
			Subsystem: "find_missing_blobs",
			Name:      "batch_size_blob_count",
			Buckets:   prometheus.ExponentialBucketsRange(1, 10000, 18),
		}, []string{"inv_id", "result"}),
		fmbBatchSizeBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "rbemeter",
			Subsystem: "find_missing_blobs",
			Name:      "batch_size_bytes",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 14),
		}, []string{"inv_id", "result"}),
		bsrStartedBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "rbemeter",
			Subsystem: "bytestream_read",
			Name:      "started_bytes",
		}, []string{"inv_id"}),
		bsrCompletedBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "rbemeter",
			Subsystem: "bytestream_read",
			Name:      "completed_bytes",
		}, []string{"inv_id", "result"}),
		bsrBlobReadTput: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "rbemeter",
			Subsystem: "bytestream_read",
			Name:      "blob_tput_bytes_per_second",
			Buckets:   prometheus.ExponentialBuckets(1024 /* 1 KBps */, 2, 20),
		}, []string{"inv_id"}),
	}

	reg.MustRegister(srv.fmbBlobSizeBytes)
	reg.MustRegister(srv.fmbBatchSizeBlobCount)
	reg.MustRegister(srv.fmbBatchSizeBytes)
	reg.MustRegister(srv.bsrStartedBytes)
	reg.MustRegister(srv.bsrCompletedBytes)
	reg.MustRegister(srv.bsrBlobReadTput)

	return srv
}

func invocationId(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Errorf(codes.InvalidArgument, "can't get call metadata")
	}
	data := []byte(md.Get(`build.bazel.remote.execution.v2.requestmetadata-bin`)[0])
	if len(data) == 0 {
		return "", status.Errorf(codes.InvalidArgument, "empty or missing repb.REquestMetadata header")
	}
	reqMeta := &repb.RequestMetadata{}
	if err := proto.Unmarshal(data, reqMeta); err != nil {
		return "", util.StatusWrapWithCode(err, codes.InvalidArgument, "can't parse header value as repb.RequestMetadata message")
	}
	return reqMeta.ToolInvocationId, nil
}

func byteStreamBlobSize(name string) (uint64, error) {
	fields := strings.Split(name, "/")
	sizeStr := fields[len(fields)-1]
	return strconv.ParseUint(sizeStr, 10, 64)
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

	iid, err := invocationId(ctx)
	if err != nil {
		return nil, err
	}

	sizeBytes := int64(0)
	sizeSet := map[string]int64{}
	for _, digest := range req.BlobDigests {
		sizeSet[digest.GetHash()] = digest.GetSizeBytes()
		sizeBytes += digest.GetSizeBytes()
		s.fmbBlobSizeBytes.WithLabelValues(iid, "all").Observe(float64(digest.GetSizeBytes()))
	}
	s.fmbBatchSizeBlobCount.WithLabelValues(iid, "all").Observe(float64(len(req.BlobDigests)))
	s.fmbBatchSizeBytes.WithLabelValues(iid, "all").Observe(float64(sizeBytes))

	res, err = s.ContentAddressableStorage.FindMissingBlobs(ctx, req)
	if err != nil {
		return res, err
	}

	sizeHitBytes := int64(0)
	sizeMissBytes := int64(0)
	for _, digest := range res.MissingBlobDigests {
		sizeMissBytes += digest.GetSizeBytes()
		s.fmbBlobSizeBytes.WithLabelValues(iid, "miss").Observe(float64(digest.GetSizeBytes()))
		delete(sizeSet, digest.GetHash())
	}
	for _, size := range sizeSet {
		sizeHitBytes += size
		s.fmbBlobSizeBytes.WithLabelValues(iid, "hit").Observe(float64(size))
	}
	s.fmbBatchSizeBlobCount.WithLabelValues(iid, "miss").Observe(float64(len(res.MissingBlobDigests)))
	s.fmbBatchSizeBytes.WithLabelValues(iid, "miss").Observe(float64(sizeMissBytes))
	s.fmbBatchSizeBlobCount.WithLabelValues(iid, "hit").Observe(float64(len(sizeSet)))
	s.fmbBatchSizeBytes.WithLabelValues(iid, "hit").Observe(float64(sizeHitBytes))

	return res, nil
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

func (s *Server) Read(req *bpb.ReadRequest, srv bpb.ByteStream_ReadServer) (rpcErr error) {
	iid, err := invocationId(srv.Context())
	if err != nil {
		return err
	}

	streamStart := time.Now()

	client, err := s.ByteStream.Read(srv.Context(), req)
	if err != nil {
		slog.Error("failed to create client", slog.String("method", "ByteStream.Read"), slog.Any("err", err))
		return util.StatusWrapWithCode(err, codes.FailedPrecondition, "failed to create Read client")
	}

	size, err := byteStreamBlobSize(req.GetResourceName())
	if err != nil {
		return util.StatusWrapWithCode(err, codes.Internal, "failed to parse size from resource name")
	}
	remaining := size
	s.bsrStartedBytes.WithLabelValues(iid).Add(float64(remaining))
	defer func() {
		code := codes.OK
		if rpcErr != nil && !errors.Is(rpcErr, io.EOF) {
			code = status.Code(rpcErr)
		} else {
			// Only record throughputs for successfully-downloaded blobs
			d := time.Now().Sub(streamStart).Seconds()
			s.bsrBlobReadTput.WithLabelValues(iid).Observe(float64(size) / d)
		}
		s.bsrCompletedBytes.WithLabelValues(iid, code.String())
	}()

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

		remaining -= uint64(len(res.Data))
		s.bsrCompletedBytes.WithLabelValues(iid, codes.OK.String()).Add(float64(len(res.Data)))
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
