package main

import "testing"

func TestParseDecision_Clean(t *testing.T) {
	e, err := parseDecision(`{"decision":"ask","reason":"kubectl mutates cluster state"}`)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if e.Decision != "ask" || e.Reason != "kubectl mutates cluster state" {
		t.Fatalf("bad parse: %+v", e)
	}
}

func TestParseDecision_EmbeddedInPreamble(t *testing.T) {
	// The LLM sometimes prefixes output. The fallback regex must still extract it.
	e, err := parseDecision("Here you go: {\"decision\":\"deny\",\"reason\":\"prod\"}\n")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if e.Decision != "deny" {
		t.Fatalf("bad decision: %+v", e)
	}
}

func TestParseDecision_Invalid(t *testing.T) {
	if _, err := parseDecision(`{"decision":"wat"}`); err == nil {
		t.Errorf("expected error on invalid decision value")
	}
	if _, err := parseDecision(`not json`); err == nil {
		t.Errorf("expected error on non-JSON")
	}
}
