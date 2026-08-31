package engine_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"symagios/encrypt"
	"symagios/engine"
	"symagios/storage"
	"symagios/storage/sqlite"
	"symagios/utils/types"
)

func TestLocalHasCapabilityRouting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.db")
	db, err := sqlite.New(sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	engine.InitStore(db, nil)
	ctx := context.Background()

	// No local manifest → not local.
	if engine.LocalHasCapability(ctx, types.Skill, "triage") {
		t.Fatal("expected missing capability")
	}

	if err := db.InsertWorkflowManifest(ctx, storage.WorkflowManifest{
		SessionID:     "s",
		Capabilities:  `[{"name":"triage","type":1}]`,
		WorkflowSteps: "[]",
	}); err != nil {
		t.Fatal(err)
	}
	if !engine.LocalHasCapability(ctx, types.Skill, "triage") {
		t.Fatal("expected local skill")
	}
	if engine.LocalHasCapability(ctx, types.Agent, "triage") {
		t.Fatal("skill should not match agent type")
	}
}

func TestPromoteLocalExternalWait(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	db, err := sqlite.New(sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	engine.InitStore(db, nil)
	ctx := context.Background()

	_ = db.InsertActionRun(ctx, storage.ActionRun{
		SessionID: "s", ActionName: "triage", ActionType: types.Skill,
		Status: types.WaitingForExternalSkillAction, Payload: "x",
	})
	// Capability not present yet — stays external.
	if err := engine.PromoteLocalExternalWaits(ctx); err != nil {
		t.Fatal(err)
	}
	still, err := db.ListActionRunsByStatus(ctx, types.WaitingForExternalSkillAction)
	if err != nil || len(still) != 1 {
		t.Fatalf("want still waiting: %v %+v", err, still)
	}

	_ = db.InsertWorkflowManifest(ctx, storage.WorkflowManifest{
		SessionID: "other", Capabilities: `[{"name":"triage","type":1}]`, WorkflowSteps: "[]",
	})
	if err := engine.PromoteLocalExternalWaits(ctx); err != nil {
		t.Fatal(err)
	}
	pending, err := db.ListActionRunsByStatus(ctx, types.Pending)
	if err != nil || len(pending) != 1 || pending[0].ActionName != "triage" {
		t.Fatalf("want promoted pending: %v %+v", err, pending)
	}
}

func TestCapabilityPromptFromDataSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cap.db")
	db, err := sqlite.New(sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	engine.InitStore(db, nil)
	ctx := context.Background()

	caps := `[{"name":"triage","type":1,"data_source":"# Skill: triage\nClassify."}]`
	if err := db.InsertWorkflowManifest(ctx, storage.WorkflowManifest{
		SessionID: "s", Capabilities: caps, WorkflowSteps: "[]",
	}); err != nil {
		t.Fatal(err)
	}
	got := engine.CapabilityPrompt(ctx, types.Skill, "triage")
	if !strings.Contains(got, "Classify") {
		t.Fatalf("prompt=%q", got)
	}
	if engine.CapabilityPrompt(ctx, types.Skill, "missing") != "" {
		t.Fatal("expected empty for missing")
	}
}

func TestExternalWaitStatus(t *testing.T) {
	if engine.ExternalWaitStatus(types.Skill) != types.WaitingForExternalSkillAction {
		t.Fatal(types.Skill)
	}
	if engine.ExternalWaitStatus(types.Agent) != types.WaitingForExternalAgentAction {
		t.Fatal(types.Agent)
	}
	if engine.ExternalWaitStatus(types.SubAgent) != types.WaitingForExternalSubAgentAction {
		t.Fatal(types.SubAgent)
	}
}

func TestDispatchEnqueuesChild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.db")
	db, err := sqlite.New(sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	engine.InitStore(db, nil)

	key, err := encrypt.NewEncryptKey(strings.Repeat("d", 32))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = db.InsertWorkflowManifest(ctx, storage.WorkflowManifest{
		SessionID: "s", Capabilities: `[{"name":"triage","type":1}]`, WorkflowSteps: "[]",
	})
	_ = db.InsertActionRun(ctx, storage.ActionRun{
		SessionID: "s", ActionName: "orch", ActionType: types.Agent, Status: types.Running, Payload: "x",
	})

	act, ok := engine.ParseNextAction(`SKILL::triage{"case":"1"}`)
	if !ok || act.Kind != engine.NextSkill {
		t.Fatalf("%+v", act)
	}
	childType, ok := act.ChildActionType()
	if !ok || childType != types.Skill {
		t.Fatalf("child type %+v", act)
	}
	status := types.Pending
	if !engine.LocalHasCapability(ctx, childType, act.Name) {
		status = engine.ExternalWaitStatus(childType)
	}
	enc, _ := key.Encrypt(act.Payload)
	if err := db.InsertActionRun(ctx, storage.ActionRun{
		SessionID: "s", ActionName: act.Name, ActionType: childType, Status: status, Payload: enc,
	}); err != nil {
		t.Fatal(err)
	}
	runs, err := db.ListActionRunsByStatus(ctx, types.Pending)
	if err != nil || len(runs) != 1 || runs[0].ActionName != "triage" {
		t.Fatalf("%v %+v", err, runs)
	}
}
