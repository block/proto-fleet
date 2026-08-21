package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestListenAddress(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv(listenAddressEnv, "")
		assert.Equal(t, defaultListenAddress, listenAddress())
	})

	t.Run("environment override", func(t *testing.T) {
		t.Setenv(listenAddressEnv, "127.0.0.1:15502")
		assert.Equal(t, "127.0.0.1:15502", listenAddress())
	})

	t.Run("blank environment override", func(t *testing.T) {
		t.Setenv(listenAddressEnv, " \t ")
		assert.Equal(t, defaultListenAddress, listenAddress())
	})
}
