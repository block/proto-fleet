package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
)

// pinFleetAuthEnv pins the string-valued FLEET_* connection and auth env vars
// so values from the developer's shell cannot leak into root flag resolution;
// vars then sets the per-case values.
func pinFleetAuthEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	for _, key := range []string{envFleetServer, envFleetAPIKey, envFleetUsername, envFleetPassword} {
		t.Setenv(key, "")
	}
	for key, value := range vars {
		t.Setenv(key, value)
	}
}

func findSubcommand(t *testing.T, parent *cli.Command, name string) *cli.Command {
	t.Helper()
	for _, sub := range parent.Commands {
		if sub.Name == name {
			return sub
		}
	}
	t.Fatalf("subcommand %q not found under %q", name, parent.Name)
	return nil
}

// probeAuthInputs runs the full root command with argv and captures what
// resolvedAuthInputs returns inside the leaf command's action, exercising the
// real flag parsing including subcommand-local flags and env sources.
func probeAuthInputs(t *testing.T, path []string, argv ...string) (string, string, string) {
	t.Helper()

	root := newRootCommand()
	leaf := root
	for _, name := range path {
		leaf = findSubcommand(t, leaf, name)
	}

	var apiKey, username, password string
	captured := false
	leaf.Action = func(_ context.Context, cmd *cli.Command) error {
		apiKey, username, password = resolvedAuthInputs(cmd)
		captured = true
		return nil
	}
	if err := root.Run(context.Background(), append([]string{"fleetcli"}, argv...)); err != nil {
		t.Fatalf("run fleetcli %s: %v", strings.Join(argv, " "), err)
	}
	if !captured {
		t.Fatalf("probe action never ran for: fleetcli %s", strings.Join(argv, " "))
	}
	return apiKey, username, password
}

func TestNormalizeEnum(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "set-power-target", want: "set_power_target"},
		{in: "SET-POWER-TARGET", want: "set_power_target"},
		{in: "set_power_target", want: "set_power_target"},
		{in: " one-time ", want: "one_time"},
		{in: "reboot", want: "reboot"},
	}
	for _, tt := range tests {
		if got := normalizeEnum(tt.in); got != tt.want {
			t.Errorf("normalizeEnum(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResolvedAuthInputs(t *testing.T) {
	authLogin := []string{"auth", "login"}
	tests := []struct {
		name     string
		env      map[string]string
		path     []string
		argv     []string
		wantKey  string
		wantUser string
		wantPass string
	}{
		{
			name:     "env api key and creds all pass through",
			env:      map[string]string{envFleetAPIKey: "k", envFleetUsername: "u", envFleetPassword: "p"},
			path:     authLogin,
			argv:     []string{"auth", "login"},
			wantKey:  "k",
			wantUser: "u",
			wantPass: "p",
		},
		{
			name:    "cli api key only",
			path:    authLogin,
			argv:    []string{"--api-key", "clik", "auth", "login"},
			wantKey: "clik",
		},
		{
			name:     "cli credentials before subcommand",
			path:     authLogin,
			argv:     []string{"--username", "u", "--password", "p", "auth", "login"},
			wantUser: "u",
			wantPass: "p",
		},
		{
			name:     "auth flags after subcommand bind to root",
			path:     authLogin,
			argv:     []string{"auth", "login", "--username", "u", "--password", "p"},
			wantUser: "u",
			wantPass: "p",
		},
		{
			name:     "cli username overrides env username",
			env:      map[string]string{envFleetUsername: "envu"},
			path:     authLogin,
			argv:     []string{"--username", "cliu", "auth", "login"},
			wantUser: "cliu",
		},
		{
			name:     "env api key kept alongside cli credentials",
			env:      map[string]string{envFleetAPIKey: "k"},
			path:     authLogin,
			argv:     []string{"--username", "u", "--password", "p", "auth", "login"},
			wantKey:  "k",
			wantUser: "u",
			wantPass: "p",
		},
		{
			name:    "pools update local username does not leak into auth",
			env:     map[string]string{envFleetAPIKey: "k"},
			path:    []string{"pools", "update"},
			argv:    []string{"pools", "update", "--username", "pooluser"},
			wantKey: "k",
		},
		{
			name:    "pools validate local username does not leak into auth",
			env:     map[string]string{envFleetAPIKey: "k"},
			path:    []string{"pools", "validate"},
			argv:    []string{"pools", "validate", "--username", "pooluser"},
			wantKey: "k",
		},
		{
			name: "onboarding create-admin credentials do not leak into auth",
			path: []string{"onboarding", "create-admin"},
			argv: []string{"onboarding", "create-admin", "--username", "au", "--password", "ap"},
		},
		{
			name: "nothing set resolves empty",
			path: authLogin,
			argv: []string{"auth", "login"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinFleetAuthEnv(t, tt.env)
			apiKey, username, password := probeAuthInputs(t, tt.path, tt.argv...)
			if apiKey != tt.wantKey || username != tt.wantUser || password != tt.wantPass {
				t.Errorf("resolvedAuthInputs = (%q, %q, %q), want (%q, %q, %q)",
					apiKey, username, password, tt.wantKey, tt.wantUser, tt.wantPass)
			}
		})
	}
}

func TestScheduleCreateEnumInputs(t *testing.T) {
	tests := []struct {
		name         string
		action       string
		scheduleType string
	}{
		{name: "hyphenated values", action: "set-power-target", scheduleType: "one-time"},
		{name: "underscored values", action: "set_power_target", scheduleType: "one_time"},
		{name: "uppercase values", action: "SET-POWER-TARGET", scheduleType: "ONE-TIME"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinFleetAuthEnv(t, nil)

			var gotAuth string
			var gotBody map[string]any
			mux := http.NewServeMux()
			mux.HandleFunc("POST /schedule.v1.ScheduleService/CreateSchedule", func(w http.ResponseWriter, r *http.Request) {
				gotAuth = r.Header.Get("Authorization")
				if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
					t.Errorf("decode create request: %v", err)
				}
				w.Header().Set("Content-Type", contentTypeJSON)
				_, _ = w.Write([]byte("{}"))
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			err := newRootCommand().Run(context.Background(), []string{
				"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
				"schedule", "create", "--name", "n", "--action", tt.action, "--schedule-type", tt.scheduleType,
			})
			if err != nil {
				t.Fatalf("schedule create --action %q: %v", tt.action, err)
			}
			if gotAuth != "Bearer test-key" {
				t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-key")
			}
			if gotBody["action"] != "SCHEDULE_ACTION_SET_POWER_TARGET" {
				t.Errorf("request action = %v, want SCHEDULE_ACTION_SET_POWER_TARGET", gotBody["action"])
			}
			if gotBody["schedule_type"] != "SCHEDULE_TYPE_ONE_TIME" {
				t.Errorf("request schedule_type = %v, want SCHEDULE_TYPE_ONE_TIME", gotBody["schedule_type"])
			}
		})
	}

	t.Run("invalid value rejected before any request", func(t *testing.T) {
		pinFleetAuthEnv(t, nil)

		mux := http.NewServeMux()
		forbidFirmwareEndpoint(t, mux, "POST /schedule.v1.ScheduleService/CreateSchedule")
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)

		err := newRootCommand().Run(context.Background(), []string{
			"fleetcli", "--server", srv.URL + "/", "--api-key", "test-key",
			"schedule", "create", "--name", "n", "--action", "bogus",
		})
		if err == nil || !strings.Contains(err.Error(), "invalid value for action") {
			t.Fatalf("schedule create --action bogus error = %v, want invalid-value error", err)
		}
		if !strings.Contains(err.Error(), "set-power-target") {
			t.Errorf("error should list hyphenated valid options, got: %v", err)
		}
	})
}

// TestApiKeyListAuthenticatesWithEnvCreds covers the bug where an env
// FLEET_API_KEY blanked env FLEET_USERNAME/FLEET_PASSWORD, breaking
// session-only commands even though credentials were available.
func TestApiKeyListAuthenticatesWithEnvCreds(t *testing.T) {
	pinFleetAuthEnv(t, map[string]string{
		envFleetAPIKey:   "env-key",
		envFleetUsername: "admin",
		envFleetPassword: "proto",
	})

	var authBody map[string]any
	var listCookie string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth.v1.AuthService/Authenticate", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&authBody); err != nil {
			t.Errorf("decode authenticate request: %v", err)
		}
		http.SetCookie(w, &http.Cookie{Name: "fleet_session", Value: "sess", Path: "/", Secure: true})
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte("{}"))
	})
	mux.HandleFunc("POST /apikey.v1.ApiKeyService/ListApiKeys", func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("fleet_session"); err == nil {
			listCookie = cookie.Value
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte(`{"api_keys":[]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/", "apikey", "list",
	})
	if err != nil {
		t.Fatalf("apikey list with env key and creds should succeed, got: %v", err)
	}
	if authBody["username"] != "admin" || authBody["password"] != "proto" {
		t.Errorf("authenticate body = %v, want env username/password", authBody)
	}
	if listCookie != "sess" {
		t.Errorf("ListApiKeys session cookie = %q, want %q", listCookie, "sess")
	}
}

// TestPoolsValidateBearerWithLocalUsername covers the bug where the
// subcommand-local --username flag hijacked Fleet auth and discarded the API
// key: the pool username must reach the request body while auth stays bearer.
func TestPoolsValidateBearerWithLocalUsername(t *testing.T) {
	pinFleetAuthEnv(t, map[string]string{envFleetAPIKey: "k"})

	var gotAuth string
	var gotBody map[string]any
	mux := http.NewServeMux()
	forbidFirmwareEndpoint(t, mux, "POST /auth.v1.AuthService/Authenticate")
	mux.HandleFunc("POST /pools.v1.PoolsService/ValidatePool", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode validate request: %v", err)
		}
		w.Header().Set("Content-Type", contentTypeJSON)
		_, _ = w.Write([]byte("{}"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := newRootCommand().Run(context.Background(), []string{
		"fleetcli", "--server", srv.URL + "/",
		"pools", "validate", "--url", "stratum+tcp://pool:3333", "--username", "pooluser",
	})
	if err != nil {
		t.Fatalf("pools validate with env api key should succeed, got: %v", err)
	}
	if gotAuth != "Bearer k" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer k")
	}
	if gotBody["username"] != "pooluser" {
		t.Errorf("request username = %v, want pooluser", gotBody["username"])
	}
	if gotBody["url"] != "stratum+tcp://pool:3333" {
		t.Errorf("request url = %v, want the pool url", gotBody["url"])
	}
}
