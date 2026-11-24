package middleware

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/block/proto-fleet/server/generated/grpc/fleetnodegateway/v1/fleetnodegatewayv1connect"
)

// ClassifyUnaryRequest marks the existing HTTP span using a registered RPC
// procedure. REST adapters call it before decoding; the Connect interceptor
// calls it before admission/authentication. Calling both updates the same span.
func ClassifyUnaryRequest(ctx context.Context, procedure string) {
	resource := "fleet.api.unary"
	if strings.HasPrefix(procedure, "/"+fleetnodegatewayv1connect.FleetNodeGatewayServiceName+"/") {
		resource = "fleet.node.unary"
	}
	// otelhttp formats the HTTP span name again after dispatch. Datadog's
	// explicit resource override survives that normal naming behavior.
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("resource.name", resource),
		attribute.String("http.route", procedure),
	)
}
