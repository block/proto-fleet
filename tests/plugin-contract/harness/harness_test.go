package harness

import (
	"sync"
	"testing"
)

func TestTestWriterIgnoresLogsAfterTestCleanup(t *testing.T) {
	var writer *testWriter
	t.Run("plugin lifecycle", func(t *testing.T) {
		writer = &testWriter{t: t}
		t.Cleanup(writer.Close)
		if n, err := writer.Write([]byte("plugin running")); n != len("plugin running") || err != nil {
			t.Fatalf("Write returned (%d, %v)", n, err)
		}
	})

	// A go-plugin process watcher can log after the owning test has ended.
	var writers sync.WaitGroup
	for range 10 {
		writers.Go(func() {
			if n, err := writer.Write([]byte("plugin exited")); n != len("plugin exited") || err != nil {
				t.Errorf("late Write returned (%d, %v)", n, err)
			}
		})
	}
	writer.Close()
	writers.Wait()
}
