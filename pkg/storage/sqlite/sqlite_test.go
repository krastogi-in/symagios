package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"symagios/storage"
	"symagios/storage/sqlite"
	"symagios/utils/types"
)

func TestActionRunLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sym.db")
	db, err := sqlite.New(sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	err = db.InsertActionRun(ctx, storage.ActionRun{
		SessionID:  "sess",
		ActionName: "a1",
		ActionType: types.Agent,
		Status:     types.Pending,
		Payload:    "p",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.UpdateActionRunStatus(ctx, "sess", "a1", types.Agent, types.Running, "p", "")
	if err != nil {
		t.Fatal(err)
	}

	run, err := db.GetActionRun(ctx, "sess", "a1", types.Agent)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != types.Running {
		t.Fatalf("status=%s", run.Status)
	}

	pending, err := db.ListActionRunsByStatus(ctx, types.Running)
	if err != nil || len(pending) != 1 {
		t.Fatalf("%v %d", err, len(pending))
	}

	all, err := db.ListAllActionRuns(ctx)
	if err != nil || len(all) != 1 || all[0].Status != types.Running {
		t.Fatalf("list all: %v %#v", err, all)
	}
}

func TestClaimPendingActionRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claim.db")
	db, err := sqlite.New(sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.InsertActionRun(ctx, storage.ActionRun{
		SessionID: "s", ActionName: "a", ActionType: types.Agent,
		Status: types.Pending, Payload: "p",
	}); err != nil {
		t.Fatal(err)
	}

	ok, err := db.ClaimPendingActionRun(ctx, "s", "a", types.Agent)
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	ok, err = db.ClaimPendingActionRun(ctx, "s", "a", types.Agent)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("second claim should fail; already Running")
	}
	run, err := db.GetActionRun(ctx, "s", "a", types.Agent)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != types.Running {
		t.Fatalf("status=%s", run.Status)
	}
}

func TestWorkflowManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wf.db")
	db, err := sqlite.New(sqlite.Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	err = db.InsertWorkflowManifest(ctx, storage.WorkflowManifest{
		SessionID:     "s",
		Capabilities:  `["a"]`,
		WorkflowSteps: `[]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := db.GetWorkflowManifest(ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	if m.Capabilities != `["a"]` {
		t.Fatalf("%+v", m)
	}
}
