package sdk

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateHashboardJSON(t *testing.T) {
	for _, value := range []string{`{"text":"board","uptime":"9007199254740993"}`, `{"text":"\\u0000"}`, `{"text":"\ud83d\ude80"}`, `{"温度":42}`, `{"value":1e131071}`, `{"value":1e-16383}`} {
		t.Run(value, func(t *testing.T) { require.NoError(t, ValidateHashboardJSON([]byte(value))) })
	}
	for _, value := range []string{`{"text":"\u0000"}`, `{"\u0000":"key"}`, `{"text":"\ud800"}`, `{"text":"\udfff"}`, `{"text":"\ud800\u0041"}`, `{"value":1e131072}`, `{"value":1e-16384}`, `{"value":1e999999999999999999999}`, `{"value":`, string([]byte{'"', 0xff, '"'})} {
		t.Run(value, func(t *testing.T) { require.Error(t, ValidateHashboardJSON([]byte(value))) })
	}
	require.Error(t, ValidateHashboardJSON([]byte(strings.Repeat("[", 65)+"0"+strings.Repeat("]", 65))))
	require.NoError(t, ValidateHashboardJSON([]byte(`{"value":`+strings.Repeat("9", 131072)+`}`)))
	require.Error(t, ValidateHashboardJSON([]byte(`{"value":`+strings.Repeat("9", 131073)+`}`)))
}

func TestSafeHashboardIdentity(t *testing.T) {
	require.True(t, SafeHashboardIdentity(strings.Repeat("a", 256)))
	require.False(t, SafeHashboardIdentity(strings.Repeat("a", 257)))
	require.False(t, SafeHashboardIdentity("serial\x00"))
	require.False(t, SafeHashboardIdentity(string([]byte{0xff})))
}
