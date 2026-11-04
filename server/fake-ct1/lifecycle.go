package main

import (
	"context"
	"strings"
	"time"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// serve runs one API request at a time against the simulation clock: state
// advances before the request, and a write is recorded into history after it.
// REST, Connect, and native gRPC requests all enter through it.
func (s *ContainerState) serve(ctx context.Context, write bool, handle func()) error {
	if delay := s.Config.ResponseDelay; delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.requests.Lock()
	defer s.requests.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	now := s.now()
	s.recordUntil(now)
	s.advance(now)
	s.mu.Unlock()
	handle()
	if write {
		s.mu.Lock()
		s.advance(now)
		s.recordTransition()
		s.mu.Unlock()
	}
	return nil
}

// boundToGET reports whether an RPC's REST binding is a GET. RPC transports
// treat every other procedure as a write, matching the REST routes' rule.
func boundToGET(procedure string) bool {
	name := protoreflect.FullName(strings.ReplaceAll(strings.TrimPrefix(procedure, "/"), "/", "."))
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(name)
	if err != nil {
		return false
	}
	method, ok := descriptor.(protoreflect.MethodDescriptor)
	if !ok {
		return false
	}
	rule, _ := proto.GetExtension(method.Options(), annotations.E_Http).(*annotations.HttpRule)
	return rule.GetGet() != ""
}

func serveUnary(state *ContainerState) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (res any, err error) {
		started := time.Now()
		defer func() { state.metrics.record(info.FullMethod, started, err != nil) }()
		if serveErr := state.serve(ctx, !boundToGET(info.FullMethod), func() { res, err = handler(ctx, req) }); serveErr != nil {
			err = status.FromContextError(serveErr).Err()
		}
		return res, err
	}
}
