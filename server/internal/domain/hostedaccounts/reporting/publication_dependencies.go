package reporting

import (
	"context"
	"fmt"
)

// Publication records only prerequisite and source lookups for saved views.
// It does not construct a fact or metric provenance graph.
type publicationSourceKey struct{ id, metric string }
type publicationDependencies struct {
	prerequisites map[string]bool
	sources       map[publicationSourceKey]bool
}
type publicationDependencyContext struct {
	dependencies *publicationDependencies
	group        string
	prior        bool
}
type publicationDependencyContextKey struct{}

func dependencyGroup(ctx context.Context, group string) context.Context {
	state, ok := ctx.Value(publicationDependencyContextKey{}).(publicationDependencyContext)
	if !ok {
		return ctx
	}
	state.group = group
	return context.WithValue(ctx, publicationDependencyContextKey{}, state)
}

func priorDependencies(ctx context.Context) context.Context {
	state, ok := ctx.Value(publicationDependencyContextKey{}).(publicationDependencyContext)
	if !ok {
		return ctx
	}
	state.prior = true
	return context.WithValue(ctx, publicationDependencyContextKey{}, state)
}

func consultedDependencies(ctx context.Context) *publicationDependencies {
	state, ok := ctx.Value(publicationDependencyContextKey{}).(publicationDependencyContext)
	if !ok {
		return nil
	}
	// Annual comparisons discard prior defaults, fees, backlog, pool counts
	// and external results. Their lookups cannot authorize public fields.
	if state.prior && state.group != "energy" && state.group != "hashrate" && state.group != "maintenance" {
		return nil
	}
	return state.dependencies
}

func prerequisiteReady(ctx context.Context, c Configuration, key string) bool {
	if deps := consultedDependencies(ctx); deps != nil {
		deps.prerequisites[key] = true
	}
	return c.Ready(key)
}

func collectPublicationDependencies(ctx context.Context, r Result, in Inputs) (publicationDependencies, error) {
	deps := publicationDependencies{prerequisites: map[string]bool{}, sources: map[publicationSourceKey]bool{}}
	ctx = context.WithValue(ctx, publicationDependencyContextKey{}, publicationDependencyContext{dependencies: &deps})
	// Replay calculator seams for saved views. Issuance separately validates
	// full inputs and verifies full, selected and saved calculation equality.
	calculateScope(ctx, r.Period, in, scope{})
	for _, b := range r.Containers {
		calculateScope(ctx, r.Period, in, scope{site: b.SiteID, container: b.ContainerID})
	}
	for _, b := range r.Orders {
		calculateScope(ctx, r.Period, in, scope{order: b.OrderID})
	}
	deadlinesContext(ctx, r.Period, in.Configuration)
	if err := ctx.Err(); err != nil {
		return deps, fmt.Errorf("collect publication dependencies: %w", err)
	}
	return deps, nil
}
