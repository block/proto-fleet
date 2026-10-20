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

const commandTracerName = "github.com/block/proto-fleet/server/internal/domain/fleetnode/control"

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

	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	cmd.TraceContext = nil
	if traceparent := carrier.Get("traceparent"); traceparent != "" {
		cmd.TraceContext = &gatewaypb.TraceContext{
			Traceparent: traceparent,
			Tracestate:  carrier.Get("tracestate"),
		}
	}
	return ctx, span
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
