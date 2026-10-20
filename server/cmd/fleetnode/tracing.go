package main

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	pb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

const fleetNodeCommandTracerName = "github.com/block/proto-fleet/server/cmd/fleetnode"

// startFleetNodeCommandSpan returns a span that the caller must end.
//
//nolint:spancheck // Span ownership is transferred to the caller.
func startFleetNodeCommandSpan(parent context.Context, fleetNodeID int64, cmd *pb.ControlCommand, commandKind string, admissionClass controlCommandAdmissionClass) (context.Context, trace.Span) {
	if remote := cmd.GetTraceContext(); remote != nil {
		parent = propagation.TraceContext{}.Extract(parent, propagation.MapCarrier{
			"traceparent": remote.GetTraceparent(),
			"tracestate":  remote.GetTracestate(),
		})
	}
	if !trace.SpanContextFromContext(parent).IsRemote() {
		// Command spans belong to the per-command carrier, never to the
		// long-lived control session. Missing or invalid context starts a root.
		parent = trace.ContextWithSpanContext(parent, trace.SpanContext{})
	}
	return otel.Tracer(fleetNodeCommandTracerName).Start(
		parent,
		"fleetnode.command",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.Int64("fleet.node.id", fleetNodeID),
			attribute.String("fleet.command.id", cmd.GetCommandId()),
			attribute.String("fleet.command.kind", commandKind),
			attribute.String("fleet.command.admission_class", string(admissionClass)),
		),
	)
}

func recordFleetNodeCommandResult(span trace.Span, ack *pb.ControlAck, sendErr error) {
	if ack == nil {
		span.SetStatus(codes.Error, "fleet node command completed without acknowledgement")
	} else {
		span.SetAttributes(
			attribute.String("fleet.command.ack_code", ack.GetCode().String()),
			attribute.Bool("fleet.command.succeeded", ack.GetSucceeded()),
		)
		if ack.GetCode() != pb.AckCode_ACK_CODE_OK || !ack.GetSucceeded() {
			span.SetStatus(codes.Error, "fleet node command failed")
		}
	}
	if sendErr != nil {
		span.SetStatus(codes.Error, "fleet node acknowledgement send failed")
	}
}
