package web_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/block/proto-fleet/plugin/antminer/pkg/antminer/web"
	sdk "github.com/block/proto-fleet/server/sdk/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoredPoolPasswordPresenceFromActualHTTPResponse(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		present     bool
		password    string
	}{
		{"present", "\"pass\":\"stored-secret\",", true, "stored-secret"},
		{"explicit empty", "\"pass\":\"\",", true, ""},
		{"omitted", "", false, ""},
		{"null", "\"pass\":null,", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/cgi-bin/get_miner_conf.cgi", r.URL.Path)
				_, _ = fmt.Fprintf(w, `{"pools":[{%s"url":"stratum+tcp://pool.example:3333","user":"worker"},{"url":"","user":"","pass":""}]}`, tc.field)
			}))
			defer server.Close()
			cfg, err := web.NewService().GetMinerConfig(t.Context(), newTestAntminerConnectionInfo(t, server.URL, sdk.UsernamePassword{Username: "root", Password: "root"}))
			require.NoError(t, err)
			require.Len(t, cfg.Pools, 2)
			require.Equal(t, tc.present, cfg.Pools[0].PasswordPresent)
			require.Equal(t, tc.password, cfg.Pools[0].Password)
			require.True(t, cfg.Pools[1].PasswordPresent, "disabled slots also need actual credential presence")
		})
	}
}
