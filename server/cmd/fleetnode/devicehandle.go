package main

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	sdk "github.com/block/proto-fleet/server/sdk/v1"
)

type deviceHandleCleaner interface {
	CloseDevice(ctx context.Context, deviceID string) error
}

func cleanupUncertainDeviceCreation(ctx context.Context, driver sdk.Driver, handleID string, err error) {
	code := grpcstatus.Code(err)
	uncertain := ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		code == codes.Canceled || code == codes.DeadlineExceeded || code == codes.Unavailable
	if cleaner, ok := driver.(deviceHandleCleaner); ok && uncertain {
		// The registration may exist even though its response was canceled or lost.
		closeUncertainDevice(ctx, cleaner, handleID)
	}
}

func closeUncertainDevice(ctx context.Context, cleaner deviceHandleCleaner, deviceID string) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	for {
		if cleaner.CloseDevice(closeCtx, deviceID) == nil {
			return
		}
		// Keep retrying even after NotFound: NewDevice may still be registering.
		select {
		case <-closeCtx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}
