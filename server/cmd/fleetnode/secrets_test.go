package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretReader_PipedSequence(t *testing.T) {
	t.Parallel()

	// Arrange
	stdin := strings.NewReader("first-secret\nsecond-secret\n")
	var stderr bytes.Buffer
	sr := newSecretReader(stdin, &stderr)

	// Act
	first, err1 := sr.read("prompt one: ")
	second, err2 := sr.read("prompt two: ")

	// Assert
	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Equal(t, "first-secret", first)
	assert.Equal(t, "second-secret", second)
	assert.Contains(t, stderr.String(), "prompt one: ")
	assert.Contains(t, stderr.String(), "prompt two: ")
}

func TestSecretReader_EmptyStdinReturnsClearError(t *testing.T) {
	t.Parallel()

	// Arrange
	sr := newSecretReader(strings.NewReader(""), &bytes.Buffer{})

	// Act
	_, err := sr.read("prompt: ")

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no input on stdin")
}

func TestSecretReader_TrimsTrailingWhitespace(t *testing.T) {
	t.Parallel()

	// Arrange
	sr := newSecretReader(strings.NewReader("  padded   \n"), &bytes.Buffer{})

	// Act
	got, err := sr.read("prompt: ")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "padded", got)
}
