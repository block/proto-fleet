package promqlshim

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
)

// writePromVector renders rows as a Prometheus API instant-vector response.
func writePromVector(w http.ResponseWriter, rows []resultRow) {
	type sample struct {
		Metric map[string]string `json:"metric"`
		Value  [2]any            `json:"value"`
	}
	out := struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string   `json:"resultType"`
			Result     []sample `json:"result"`
		} `json:"data"`
	}{Status: "success"}
	out.Data.ResultType = "vector"
	out.Data.Result = make([]sample, 0, len(rows))
	for _, r := range rows {
		out.Data.Result = append(out.Data.Result, sample{
			Metric: r.labels,
			Value: [2]any{
				float64(r.ts.Unix()) + float64(r.ts.Nanosecond())/1e9,
				strconv.FormatFloat(r.value, 'f', -1, 64),
			},
		})
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		slog.Error("promqlshim: failed to encode response", "error", err)
	}
}

func writePromError(w http.ResponseWriter, status int, errorType, msg string) {
	out := struct {
		Status    string `json:"status"`
		ErrorType string `json:"errorType"`
		Error     string `json:"error"`
	}{Status: "error", ErrorType: errorType, Error: msg}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(out); err != nil {
		slog.Error("promqlshim: failed to encode error", "error", err)
	}
}
