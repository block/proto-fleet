package deployment

import (
	"context"
	"strings"
	"testing"
)

func TestRunComposeRejectsParentOverrides(t *testing.T) {
	for _, key := range []string{"AUTH_CLIENT_SECRET_KEY", "HA_NODE_IP"} {
		t.Run(key, func(t *testing.T) {
			// Arrange
			const value = "must-not-appear-in-errors"
			t.Setenv(key, value)

			// Act
			err := RunCompose(context.Background(), []string{"config"})

			// Assert
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("RunCompose() error = %v", err)
			}
			if strings.Contains(err.Error(), value) {
				t.Fatal("RunCompose() exposed the parent value")
			}
		})
	}
}
