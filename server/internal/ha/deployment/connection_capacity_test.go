package deployment

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestHAConnectionBudgetCoversBothRenderedAPIPools(t *testing.T) {
	contents, err := os.ReadFile("../../../../deployment-files/ha/fleet-compose.yaml")
	require.NoError(t, err)
	var compose struct {
		Services map[string]struct {
			Environment map[string]string `yaml:"environment"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(contents, &compose))
	environment := compose.Services["fleet-api"].Environment
	open, err := strconv.Atoi(environment["DB_MAX_OPEN_CONNS"])
	require.NoError(t, err)
	idle, err := strconv.Atoi(environment["DB_MAX_IDLE_CONNS"])
	require.NoError(t, err)
	require.Equal(t, open, idle, "polling bursts must not discard connections at the idle cap")

	contents, err = os.ReadFile("../../../../deployment-files/ha/patroni.yml.tmpl")
	require.NoError(t, err)
	var patroni struct {
		Bootstrap struct {
			DCS struct {
				PostgreSQL struct {
					Parameters struct {
						MaxConnections int `yaml:"max_connections"`
					} `yaml:"parameters"`
				} `yaml:"postgresql"`
			} `yaml:"dcs"`
		} `yaml:"bootstrap"`
	}
	require.NoError(t, yaml.Unmarshal(contents, &patroni))
	limit := patroni.Bootstrap.DCS.PostgreSQL.Parameters.MaxConnections
	require.Equal(t, haDatabaseConnectionCapacity, limit, "update gate must match the bootstrap capacity")
	require.GreaterOrEqual(t, limit-2*open, 200, "reserve slots during HA pool overlap")
}

func TestPatroniConnectionCapacityRequiresAppliedClusterLimit(t *testing.T) {
	for _, test := range []struct {
		name       string
		limit      int
		state      string
		pending    bool
		statusCode int
		malformed  bool
		want       string
	}{
		{name: "ready", limit: 1200, state: "running"},
		{name: "larger cluster", limit: 1500, state: "running"},
		{name: "old cluster", limit: 300, state: "running", want: "at least 1200 required"},
		{name: "restart required", limit: 1200, state: "running", pending: true, want: "pending_restart=true"},
		{name: "database stopped", limit: 1200, state: "stopped", want: "running databases"},
		{name: "unavailable", statusCode: http.StatusServiceUnavailable, want: "HTTP 503"},
		{name: "invalid response", malformed: true, want: "decode Patroni capacity"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				username, password, ok := r.BasicAuth()
				require.True(t, ok)
				require.Equal(t, "patroni-admin", username)
				require.Equal(t, "test-password", password)
				require.Equal(t, http.MethodGet, r.Method)
				if test.statusCode != 0 {
					w.WriteHeader(test.statusCode)
					return
				}
				if test.malformed {
					_, _ = w.Write([]byte("invalid"))
					return
				}
				if r.URL.Path == "/config" {
					_, _ = fmt.Fprintf(w, `{"postgresql":{"parameters":{"max_connections":%d}}}`, test.limit)
					return
				}
				require.Equal(t, "/patroni", r.URL.Path)
				_, _ = fmt.Fprintf(w, `{"state":%q,"pending_restart":%t}`, test.state, test.pending)
			}))
			defer server.Close()
			err := checkPatroniConnectionCapacity(t.Context(), server.Client(), server.URL, "test-password")
			if test.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.want)
			}
		})
	}
}
