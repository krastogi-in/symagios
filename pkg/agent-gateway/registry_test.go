package agentgateway

import (
	"testing"

	"symagios/proto/a2a"
	"symagios/utils/types"
)

func TestPeerSetAddDedupesExactHost(t *testing.T) {
	p := newPeerSet()
	p.Add("https://peer.example:9000")
	p.Add("https://peer.example:9000/")
	p.Add("https://peer.example:9000")
	if p.Len() != 1 {
		t.Fatalf("len=%d hosts=%v", p.Len(), p.List())
	}
}

func TestHostRegistryIndexesSkillsAgentsSubAgents(t *testing.T) {
	r := newHostRegistry()
	host := "https://peer.example:9000"
	r.IndexCard(host, &a2a.AgentCard{
		Skills: []*a2a.AgentSkill{
			{Id: "triage", Tags: []string{string(types.Skill)}},
			{Id: "investigator", Tags: []string{string(types.SubAgent)}},
			{Id: "reporter", Tags: []string{string(types.Agent)}},
		},
	})

	got, ok := r.Get(types.Skill, "triage")
	if !ok || got != host {
		t.Fatalf("skill=%q ok=%v", got, ok)
	}
	got, ok = r.Get(types.SubAgent, "investigator")
	if !ok || got != host {
		t.Fatalf("subagent=%q ok=%v", got, ok)
	}
	got, ok = r.Get(types.Agent, "reporter")
	if !ok || got != host {
		t.Fatalf("agent=%q ok=%v", got, ok)
	}

	r.ClearHost(host)
	if _, ok := r.Get(types.Skill, "triage"); ok {
		t.Fatal("expected clear")
	}
}
