package metered

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Interceptor struct {
	grpcCallsStartedCount *prometheus.CounterVec
	grpcCallDuration *prometheus.HistogramVec
	grpcStreamRxFirstMsgLatency *prometheus.HistogramVec
	grpcStreamRxInterMsgLatency *prometheus.HistogramVec
}

func NewInterceptor(reg prometheus.Registerer) *Interceptor {
	i := &Interceptor{
		grpcCallsStartedCount: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "rbemeter",
			Subsystem: "grpc",
			Name: "calls_started_count",
		}, []string{"inv_id", "grpc_method"}),
		grpcCallDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "rbemeter",
			Subsystem: "grpc",
			Name: "call_duration_seconds",
			Buckets: prometheus.ExponentialBucketsRange(0.000001, 600, 18),
		}, []string{"inv_id", "grpc_method", "response_code"}),
		grpcStreamRxFirstMsgLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "rbemeter",
			Subsystem: "grpc",
			Name: "stream_rx_first_message_latency_seconds",
			Buckets: prometheus.ExponentialBucketsRange(0.000001, 10, 18),
		}, []string{"inv_id", "grpc_method"}),
		grpcStreamRxInterMsgLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "rbemeter",
			Subsystem: "grpc",
			Name: "stream_rx_inter_message_latency_seconds",
			Buckets: prometheus.ExponentialBucketsRange(0.000001, 10, 18),
		}, []string{"inv_id", "grpc_method"}),
	}

	reg.MustRegister(i.grpcCallsStartedCount)
	reg.MustRegister(i.grpcCallDuration)
	reg.MustRegister(i.grpcStreamRxFirstMsgLatency)
	reg.MustRegister(i.grpcStreamRxInterMsgLatency)

	return i
}

func (i *Interceptor) Intercept(
	ctx context.Context,
	method string,
	req any,
	reply any,
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
	) (rpcErr error) {
		iid, err := invocationId(ctx)
		if err != nil {
			return err
		}

		i.grpcCallsStartedCount.WithLabelValues(iid, method).Inc()

		start := time.Now()
		defer func() {
			elapsed := time.Now().Sub(start)
			code := status.Code(rpcErr).String()
			i.grpcCallDuration.WithLabelValues(iid, method, code).Observe(elapsed.Seconds())
		}()
		return invoker(ctx, method, req, reply, cc, opts...)
}

type singleStreamMetering struct {
	iid string
	method string
	impl grpc.ClientStream
	parent *Interceptor
	ttfbFlag sync.Once
	streamStart time.Time
	lastMsgTime time.Time
}

func (s *singleStreamMetering) SendMsg(msg any) error {
	return s.impl.SendMsg(msg)
}

func (s *singleStreamMetering) RecvMsg(msg any) (rpcErr error) {
	defer func() {
		if rpcErr == nil {
			return
		}
		// Got an error; the stream from the proxied server is ending
		duration := time.Now().Sub(s.streamStart)
		code := codes.OK
		if !errors.Is(rpcErr, io.EOF) {
			code = status.Code(rpcErr)
		}
		s.parent.grpcCallDuration.WithLabelValues(s.iid, s.method, code.String()).Observe(duration.Seconds())
	}()

	current := time.Now()
	s.ttfbFlag.Do(func() { s.parent.grpcStreamRxFirstMsgLatency.WithLabelValues(s.iid, s.method).Observe(current.Sub(s.streamStart).Seconds()) } )
	s.parent.grpcStreamRxInterMsgLatency.WithLabelValues(s.iid, s.method).Observe(current.Sub(s.lastMsgTime).Seconds())

	s.lastMsgTime = current

	return s.impl.RecvMsg(msg)
}

func (s *singleStreamMetering) CloseSend() error {
	return s.impl.CloseSend()
}

func (s *singleStreamMetering) Context() context.Context {
	return s.impl.Context()
}

func (s *singleStreamMetering) Header() (metadata.MD, error) {
	return s.impl.Header()
}

func (s *singleStreamMetering) Trailer() metadata.MD {
	return s.impl.Trailer()
}

func (i *Interceptor) StreamIntercept(
	ctx context.Context,
	desc *grpc.StreamDesc,
	cc *grpc.ClientConn,
	method string,
	streamer grpc.Streamer,
	opts ...grpc.CallOption) (grpc.ClientStream, error) {
		iid, err := invocationId(ctx)
		if err != nil {
			return nil, err
		}

		i.grpcCallsStartedCount.WithLabelValues(iid, method).Inc()

		stream, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			return nil, err
		}

		return &singleStreamMetering{
			iid: iid,
			method: method,
			impl: stream,
			parent: i,
			ttfbFlag: sync.Once{},
			streamStart: time.Now(),
			lastMsgTime: time.Now(),
		}, nil
	}