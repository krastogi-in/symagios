package engine_test

import (
	"strings"
	"testing"

	"symagios/engine"
)

func TestParseNextAction(t *testing.T) {
	cases := []struct {
		in   string
		kind engine.NextActionKind
		name string
		hitl bool
	}{
		{`SKILL::triage{"case_id":"1"}`, engine.NextSkill, "triage", false},
		{`please proceed
NEED_HUMAN_INPUT::approve
{"reason":"high sev"}
thanks`, engine.NextHumanInput, "approve", true},
		{`MCP::lookup_docs{"query":"x"}`, engine.NextMCP, "lookup_docs", false},
		{`WEB_SEARCH::web_search{"query":"cve"}`, engine.NextWebSearch, "web_search", false},
		{`SUB_AGENT::investigator{"host":"ws-1"}`, engine.NextSubAgent, "investigator", false},
		{`AGENT::reporter{"findings":"ok"}`, engine.NextAgent, "reporter", false},
		{`no structured action here`, "", "", false},
	}
	for _, tc := range cases {
		act, ok := engine.ParseNextAction(tc.in)
		if tc.kind == "" {
			if ok {
				t.Fatalf("expected no action for %q", tc.in)
			}
			continue
		}
		if !ok || act.Kind != tc.kind || act.Name != tc.name || act.HITL != tc.hitl {
			t.Fatalf("%q → %+v ok=%v", tc.in, act, ok)
		}
		if act.Payload == "" {
			t.Fatalf("empty payload for %q", tc.in)
		}
	}
}

func TestStripNextActions(t *testing.T) {
	in := "Looking into the case.\nSKILL::triage{\"case_id\":\"1\"}\nThanks."
	got := engine.StripNextActions(in)
	if strings.Contains(got, "SKILL::") || strings.Contains(got, `{"case_id"`) {
		t.Fatalf("still has action: %q", got)
	}
	if !strings.Contains(got, "Looking into the case.") {
		t.Fatalf("lost prose: %q", got)
	}
}

func TestStripSummary(t *testing.T) {
	in := "Resume notes.\nSUMMARY::checkpoint{\"target_objective\":\"x\"}\nDone."
	got := engine.StripNextActions(in)
	if strings.Contains(got, "SUMMARY::") || strings.Contains(got, "target_objective") {
		t.Fatalf("still has summary: %q", got)
	}
	if !strings.Contains(got, "Resume notes.") {
		t.Fatalf("lost prose: %q", got)
	}
}

func TestPublicFacingTextHITL(t *testing.T) {
	got := engine.PublicFacingText(`NEED_HUMAN_INPUT::approve{"reason":"high sev"}`)
	if strings.Contains(got, "NEED_HUMAN_INPUT") {
		t.Fatalf("leaked action: %q", got)
	}
	if !strings.Contains(got, "high sev") && !strings.Contains(got, "approve") {
		t.Fatalf("expected human question: %q", got)
	}
}

func TestParseNextActionPrefersHITL(t *testing.T) {
	in := `SKILL::triage{"a":1}
NEED_HUMAN_INPUT::stop{"reason":"x"}`
	act, ok := engine.ParseNextAction(in)
	if !ok || !act.HITL || act.Name != "stop" {
		t.Fatalf("%+v", act)
	}
}
