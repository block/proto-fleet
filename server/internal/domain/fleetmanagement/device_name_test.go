package fleetmanagement

import "testing"

func TestComposeDeviceName(t *testing.T) {
	tests := []struct {
		name         string
		customName   string
		manufacturer string
		model        string
		want         string
	}{
		{
			name:         "custom name wins over manufacturer+model",
			customName:   "Rack 3 / Slot 7",
			manufacturer: "Bitmain",
			model:        "S21",
			want:         "Rack 3 / Slot 7",
		},
		{
			name:         "falls back to manufacturer + model when custom name empty",
			customName:   "",
			manufacturer: "Bitmain",
			model:        "S21",
			want:         "Bitmain S21",
		},
		{
			name:         "only manufacturer, trims trailing space",
			customName:   "",
			manufacturer: "Bitmain",
			model:        "",
			want:         "Bitmain",
		},
		{
			name:         "only model, trims leading space",
			customName:   "",
			manufacturer: "",
			model:        "S21",
			want:         "S21",
		},
		{
			name:         "all empty returns empty string, not a lone space",
			customName:   "",
			manufacturer: "",
			model:        "",
			want:         "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ComposeDeviceName(tc.customName, tc.manufacturer, tc.model)
			if got != tc.want {
				t.Errorf("ComposeDeviceName(%q, %q, %q) = %q, want %q",
					tc.customName, tc.manufacturer, tc.model, got, tc.want)
			}
		})
	}
}
