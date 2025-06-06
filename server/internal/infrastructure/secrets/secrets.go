// Package secrets provides a way to handle sensitive information
// such as passwords, API keys, or any other sensitive data.
// It ensures that the original value is not exposed in logs or
// string representations, while still allowing access to the value when needed.
// Example of using Text:
//
//	t := NewText("my-secret")
//	fmt.Println(t)         // Output: ***********
//	fmt.Println(t.Value()) // Output: my-secret
package secrets

import "log/slog"

const (
	defaultRedacted = "***********"
)

// Text is a type that holds a sensitive string value.
//
//nolint:recvcheck // This struct needs to have a mix of pointer and value receivers
type Text struct {
	value *string
}

// NewText creates a new Text instance with the provided value.
func NewText(value string) *Text {
	return &Text{value: &value}
}

// Value returns the original value of the Text instance.
func (t Text) Value() string {
	return *t.value
}

// String returns a redacted version of the Text instance.
// This is used to prevent sensitive information from being logged or printed.
func (t Text) String() string {
	return defaultRedacted
}

// MarshalJSON implements the encoding.TextMarshaler interface.
// It returns a redacted version of the Text instance.
func (t Text) MarshalJSON() ([]byte, error) {
	return []byte(`"` + defaultRedacted + `"`), nil
}

// UnmarshalJSON implements the json.Unmarshaler interface.
// It does not retrieve the original value, only the redacted text is used.
func (t *Text) UnmarshalJSON(_ []byte) error {
	slog.Warn("Unmarshalling Text type does not retrieve the original value, only the redacted text is used")
	tmp := defaultRedacted
	t.value = &tmp
	return nil
}
