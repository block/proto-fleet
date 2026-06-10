package main

import (
	"encoding/json"
	"testing"
)

func TestBuildPayload(t *testing.T) {
	body, err := buildPayload(publishOptions{Target: "OFF"})
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	var got struct {
		Target    int   `json:"target"`
		Timestamp int64 `json:"timestamp"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got.Target != wireTargetOff {
		t.Fatalf("target = %d, want %d", got.Target, wireTargetOff)
	}
	if got.Timestamp <= 0 {
		t.Fatalf("timestamp = %d, want positive", got.Timestamp)
	}
}

func TestBuildPayloadRejectsInvalidTarget(t *testing.T) {
	_, err := buildPayload(publishOptions{Target: "SLEEP"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildPayloadAllowsCustomPayload(t *testing.T) {
	got, err := buildPayload(publishOptions{CustomPayload: `{"target":50}`})
	if err != nil {
		t.Fatalf("build custom payload: %v", err)
	}
	if string(got) != `{"target":50}` {
		t.Fatalf("payload = %s", got)
	}
}

func TestParseBrokers(t *testing.T) {
	got, err := parseBrokers("primary=tcp://mqtt-a:1883,secondary=tcp://mqtt-b:1883")
	if err != nil {
		t.Fatalf("parse brokers: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "primary" || got[0].URL != "tcp://mqtt-a:1883" {
		t.Fatalf("first broker = %+v", got[0])
	}
}

func TestSelectedBrokersUsesBrokerNames(t *testing.T) {
	a := newApp(config{
		defaultTopic:    defaultTopic,
		defaultInterval: defaultInterval,
		brokers: []broker{
			{Name: "secondary", URL: "tcp://mqtt-secondary:1883"},
			{Name: "primary", URL: "tcp://mqtt-primary:1883"},
		},
	})

	got := a.selectedBrokers(true, false)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name != "primary" {
		t.Fatalf("selected broker = %q, want primary", got[0].Name)
	}
}

func TestSelectedBrokersSupportsUnnamedBrokerFallback(t *testing.T) {
	a := newApp(config{
		defaultTopic:    defaultTopic,
		defaultInterval: defaultInterval,
		brokers: []broker{
			{Name: "broker-1", URL: "tcp://mqtt-a:1883"},
			{Name: "broker-2", URL: "tcp://mqtt-b:1883"},
		},
	})

	got := a.selectedBrokers(false, true)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name != "broker-2" {
		t.Fatalf("selected broker = %q, want broker-2", got[0].Name)
	}
}

func TestBuildCurtailmentSettingsURL(t *testing.T) {
	got, err := buildCurtailmentSettingsURL("http://localhost:5173", "/settings/curtailment")
	if err != nil {
		t.Fatalf("build URL: %v", err)
	}
	if got != "http://localhost:5173/settings/curtailment" {
		t.Fatalf("URL = %q", got)
	}
}

func TestBuildCurtailmentSettingsURLAllowsEmptyBase(t *testing.T) {
	got, err := buildCurtailmentSettingsURL("", "/settings/curtailment")
	if err != nil {
		t.Fatalf("build URL: %v", err)
	}
	if got != "" {
		t.Fatalf("URL = %q, want empty", got)
	}
}

func TestBuildCurtailmentSettingsURLRejectsRelativeBase(t *testing.T) {
	_, err := buildCurtailmentSettingsURL("localhost:5173", "/settings/curtailment")
	if err == nil {
		t.Fatal("expected error")
	}
}
