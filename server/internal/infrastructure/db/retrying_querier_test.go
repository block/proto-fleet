package db

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/block/proto-fleet/server/generated/sqlc"
)

func TestRetryingQuerier(t *testing.T) {
	t.Run("exec", func(t *testing.T) {
		fake := &fakeMQTTQuerier{upsertErrors: []error{errors.New("exec failed"), nil}}
		retrier := &recordingQueryRetrier{maxAttempts: 2}
		queries := sqlc.NewRetryingQuerier(fake, retrier)

		err := queries.UpsertMQTTSourceState(context.Background(), sqlc.UpsertMQTTSourceStateParams{})
		if err != nil {
			t.Fatalf("UpsertMQTTSourceState: %v", err)
		}
		assertRetryCall(t, retrier, "UpsertMQTTSourceState")
		if fake.upsertCalls != 2 {
			t.Fatalf("upsert calls = %d, want 2", fake.upsertCalls)
		}
	})

	t.Run("result method retries complete method error", func(t *testing.T) {
		want := sqlc.CurtailmentMqttSourceState{SourceConfigID: 42}
		fake := &fakeMQTTQuerier{
			stateErrors: []error{errors.New("deferred scan error"), nil},
			state:       want,
		}
		retrier := &recordingQueryRetrier{maxAttempts: 2}
		queries := sqlc.NewRetryingQuerier(fake, retrier)

		got, err := queries.GetMQTTSourceStateByID(context.Background(), 42)
		if err != nil {
			t.Fatalf("GetMQTTSourceStateByID: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("state = %#v, want %#v", got, want)
		}
		assertRetryCall(t, retrier, "GetMQTTSourceStateByID")
		if fake.stateCalls != 2 {
			t.Fatalf("state calls = %d, want 2", fake.stateCalls)
		}
	})

	t.Run("one discards partial result after retries exhaust", func(t *testing.T) {
		wantErr := errors.New("deferred scan error")
		fake := &fakeMQTTQuerier{
			stateErrors: []error{wantErr, wantErr},
			state:       sqlc.CurtailmentMqttSourceState{SourceConfigID: 42},
		}
		retrier := &recordingQueryRetrier{maxAttempts: 2}
		queries := sqlc.NewRetryingQuerier(fake, retrier)

		got, err := queries.GetMQTTSourceStateByID(context.Background(), 42)
		if !errors.Is(err, wantErr) {
			t.Fatalf("GetMQTTSourceStateByID error = %v, want %v", err, wantErr)
		}
		if !reflect.DeepEqual(got, sqlc.CurtailmentMqttSourceState{}) {
			t.Fatalf("state = %#v, want zero value", got)
		}
		if fake.stateCalls != 2 {
			t.Fatalf("state calls = %d, want 2", fake.stateCalls)
		}
	})
}

type retryCall struct {
	operationName string
}

type recordingQueryRetrier struct {
	maxAttempts int
	calls       []retryCall
}

func (r *recordingQueryRetrier) RetryQuery(_ context.Context, operationName string, fn func() error) error {
	r.calls = append(r.calls, retryCall{operationName: operationName})
	var lastErr error
	for range r.maxAttempts {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

func assertRetryCall(t *testing.T, retrier *recordingQueryRetrier, operationName string) {
	t.Helper()
	if len(retrier.calls) != 1 {
		t.Fatalf("retry calls = %#v, want one call", retrier.calls)
	}
	if retrier.calls[0].operationName != operationName {
		t.Fatalf("retry call = %#v, want operation=%q", retrier.calls[0], operationName)
	}
}

type fakeMQTTQuerier struct {
	sqlc.Querier
	upsertErrors []error
	upsertCalls  int
	stateErrors  []error
	stateCalls   int
	state        sqlc.CurtailmentMqttSourceState
}

func (f *fakeMQTTQuerier) UpsertMQTTSourceState(context.Context, sqlc.UpsertMQTTSourceStateParams) error {
	err := f.upsertErrors[f.upsertCalls]
	f.upsertCalls++
	return err
}

func (f *fakeMQTTQuerier) GetMQTTSourceStateByID(context.Context, int64) (sqlc.CurtailmentMqttSourceState, error) {
	err := f.stateErrors[f.stateCalls]
	f.stateCalls++
	if err != nil {
		return f.state, err
	}
	return f.state, nil
}
