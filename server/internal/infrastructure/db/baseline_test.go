package db

import (
	"encoding/json"
	"testing"
)

func TestBaselineCatalogRefusesDrift(t *testing.T) {
	expected := map[string]json.RawMessage{"column/device/id": json.RawMessage(`["bigint",true]`), "function/f()": json.RawMessage(`["body",false]`)}
	for _, tt := range []struct {
		name   string
		actual map[string]json.RawMessage
		want   string
	}{
		{"equal", expected, ""},
		{"missing", map[string]json.RawMessage{"column/device/id": json.RawMessage(`["bigint",true]`)}, "function/f()"},
		{"changed function", map[string]json.RawMessage{"column/device/id": json.RawMessage(`["bigint",true]`), "function/f()": json.RawMessage(`["other",false]`)}, "function/f()"},
		{"extra private column", map[string]json.RawMessage{"column/device/id": json.RawMessage(`["bigint",true]`), "function/f()": json.RawMessage(`["body",false]`), "column/device/extra": json.RawMessage(`["integer",true]`)}, "column/device/extra"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			equal := baselineCatalogDigest(expected) == baselineCatalogDigest(tt.actual)
			if equal != (tt.want == "") {
				t.Fatalf("unexpected catalog equality for %s", tt.name)
			}
		})
	}
}
