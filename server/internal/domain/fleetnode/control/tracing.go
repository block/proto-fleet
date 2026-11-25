package control

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	gatewaypb "github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1"
)

const (
	commandTracerName       = "github.com/block/proto-fleet/server/internal/domain/fleetnode/control"
	maxCommandTraceStateLen = 512 // Match TraceContext.tracestate's protobuf validation limit.
)

//nolint:spancheck // Span ownership is returned to the caller, which ends it after the command completes.
func startCommandDispatchSpan(ctx context.Context, fleetNodeID int64, commandKind string, cmd *gatewaypb.ControlCommand) (context.Context, trace.Span) {
	attrs := []attribute.KeyValue{
		attribute.Int64("fleet.node.id", fleetNodeID),
		attribute.String("fleet.command.id", cmd.GetCommandId()),
	}
	if commandKind != "" {
		attrs = append(attrs, attribute.String("fleet.command.kind", commandKind))
	}
	ctx, span := otel.Tracer(commandTracerName).Start(
		ctx,
		"fleetnode.command.dispatch",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(attrs...),
	)

	cmd.TraceContext = traceContextForCommand(ctx)
	return ctx, span
}

func traceContextForCommand(ctx context.Context) *gatewaypb.TraceContext {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	traceparent := carrier.Get("traceparent")
	if traceparent == "" {
		return nil
	}
	tracestate := carrier.Get("tracestate")
	if len(tracestate) > maxCommandTraceStateLen {
		tracestate = ""
	}
	return &gatewaypb.TraceContext{
		Traceparent: traceparent,
		Tracestate:  tracestate,
	}
}

func recordCommandAck(span trace.Span, ack *gatewaypb.ControlAck) {
	if ack == nil {
		return
	}
	span.SetAttributes(
		attribute.String("fleet.command.ack_code", ack.GetCode().String()),
		attribute.Bool("fleet.command.succeeded", ack.GetSucceeded()),
	)
	if ack.GetCode() != gatewaypb.AckCode_ACK_CODE_OK || !ack.GetSucceeded() {
		span.SetStatus(codes.Error, "fleet node command failed")
	}
}

func finishCommandDispatchSpan(span trace.Span, err error) {
	if err != nil {
		span.SetStatus(codes.Error, "fleet node command dispatch failed")
	}
	span.End()
}
