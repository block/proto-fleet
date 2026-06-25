package gateway

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunCommandArtifactDownloadSendTimesOutBlockedSend(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	err := runCommandArtifactDownloadSend(context.Background(), 10*time.Millisecond, func() error {
		close(started)
		defer close(done)
		<-release
		return nil
	})

	require.Error(t, err)
	assert.Equal(t, connect.CodeDeadlineExceeded, connect.CodeOf(err))
	close(release)

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("send function did not start")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("send function did not exit")
	}
}
