package agentgateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"symagios/meshauth"
	"symagios/proto/a2a"
	"symagios/proto/manifest"
	"symagios/storage"
	"symagios/utils/types"
)

func (g *AgentGateway) getManifestSkills(ctx context.Context) []*a2a.AgentSkill {
	skills := make([]*a2a.AgentSkill, 0)
	agentAlreadyAdded := make(map[string]bool)
	skillAlreadyAdded := make(map[string]bool)
	subAgentAlreadyAdded := make(map[string]bool)
	allWorkflowManifests, err := g.db.GetAllWorkflowManifests(ctx)
	if err != nil {
		g.log.Error("failed to get all workflow manifests", "error", err)
		return nil
	}
	for _, workflowManifest := range allWorkflowManifests {
		capabilities := make([]*manifest.TargetDescriptor, 0)
		err := json.Unmarshal([]byte(workflowManifest.Capabilities), &capabilities)
		if err != nil {
			g.log.Error("failed to unmarshal capabilities", "error", err)
			continue
		}
		for _, capability := range capabilities {
			if capability.GetType() == manifest.TargetType_SKILL && capability.GetName() != "" {
				if !skillAlreadyAdded[capability.GetName()] {
					skillAlreadyAdded[capability.GetName()] = true
					skills = append(skills, &a2a.AgentSkill{
						Id:   capability.GetName(),
						Name: capability.GetName(),
						Tags: []string{string(types.Skill)},
					})
				}
				continue
			}
			if capability.GetType() == manifest.TargetType_SUB_AGENT && capability.GetName() != "" {
				if !subAgentAlreadyAdded[capability.GetName()] {
					subAgentAlreadyAdded[capability.GetName()] = true
					skills = append(skills, &a2a.AgentSkill{
						Id:   capability.GetName(),
						Name: capability.GetName(),
						Tags: []string{string(types.SubAgent)},
					})
				}
				continue
			}
			if capability.GetType() == manifest.TargetType_AGENT && capability.GetName() != "" {
				if !agentAlreadyAdded[capability.GetName()] {
					agentAlreadyAdded[capability.GetName()] = true
					skills = append(skills, &a2a.AgentSkill{
						Id:   capability.GetName(),
						Name: capability.GetName(),
						Tags: []string{string(types.Agent)},
					})
				}

			}
		}
	}
	return skills
}

func (g *AgentGateway) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Public discovery — Envoy skips JWT for /.well-known/; no app auth here.
	card := &a2a.AgentCard{
		Name:               g.config.AgentName,
		Description:        "Symagios A2A agent gateway",
		Version:            g.config.Version,
		Url:                g.config.URL,
		PreferredTransport: "JSONRPC",
		DefaultInputModes:  []string{"application/json"},
		DefaultOutputModes: []string{"application/json"},
		Capabilities: &a2a.AgentCapabilities{
			Streaming:         false,
			PushNotifications: false,
		},
		Skills: g.getManifestSkills(r.Context()),
	}
	writeJSON(w, card)
}

func (g *AgentGateway) handleMessageSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !g.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var message a2a.Message
	if err := json.Unmarshal(body, &message); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if message.ContextId == "" {
		http.Error(w, "context_id is required", http.StatusBadRequest)
		return
	}
	if message.Metadata == nil {
		http.Error(w, "metadata is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	sessionID := message.ContextId

	// Session/manifest lifecycle is agent-entry only — A2A never creates manifests.
	if _, err := g.db.GetWorkflowManifest(ctx, sessionID); err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	if err := g.onServerTasks.PurgeTerminalForContext(ctx, sessionID); err != nil {
		g.log.Error("failed to purge terminal tasks", "context_id", sessionID, "error", err)
	}

	// Peer work body: Parts carry stored ciphertext (shared Vault key). No manifest create.
	cipherPayload := partsCiphertext(&message)

	enqueue := func(actionName string, actionType types.ActionType) (*a2a.Task, error) {
		if cipherPayload != "" {
			now := time.Now().UTC()
			if err := g.db.InsertActionRun(ctx, storage.ActionRun{
				SessionID:  sessionID,
				ActionName: actionName,
				ActionType: actionType,
				Status:     types.Pending,
				Payload:    cipherPayload,
				CreatedAt:  now,
				UpdatedAt:  now,
			}); err != nil {
				return nil, err
			}
		}
		task := pendingOnServerTask(sessionID, actionName, actionType)
		if err := g.onServerTasks.Save(ctx, NewTaskRecord(task, g.config.Host, 0)); err != nil {
			return nil, err
		}
		return task, nil
	}

	if taskAsked, ok := message.GetMetadata()[string(types.Agent)]; ok {
		task, err := enqueue(string(taskAsked), types.Agent)
		if err != nil {
			g.log.Error("enqueue peer/on-server agent failed", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, task)
		return
	}
	if taskAsked, ok := message.GetMetadata()[string(types.Skill)]; ok {
		task, err := enqueue(string(taskAsked), types.Skill)
		if err != nil {
			g.log.Error("enqueue peer/on-server skill failed", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, task)
		return
	}
	if taskAsked, ok := message.GetMetadata()[string(types.SubAgent)]; ok {
		task, err := enqueue(string(taskAsked), types.SubAgent)
		if err != nil {
			g.log.Error("enqueue peer/on-server sub-agent failed", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, task)
		return
	}

	http.Error(w, "no task asked", http.StatusBadRequest)
}

func (g *AgentGateway) handleTasksGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !g.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	taskID := r.PathValue("task_id")
	if taskID == "" {
		http.Error(w, "task_id is required", http.StatusBadRequest)
		return
	}

	if err := g.syncOnServerTaskFromID(r.Context(), taskID); err != nil {
		g.log.Error("failed to sync on-server task", "task_id", taskID, "error", err)
	}

	record, err := g.onServerTasks.Get(r.Context(), taskID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, record.Task)
}

func (g *AgentGateway) handleTasksList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !g.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	contextID := r.URL.Query().Get("contextId")
	if contextID == "" {
		http.Error(w, "contextId query param is required", http.StatusBadRequest)
		return
	}

	if err := g.syncOnServerTasksForSession(r.Context(), contextID); err != nil {
		g.log.Error("failed to sync on-server tasks", "context_id", contextID, "error", err)
	}

	records, err := g.onServerTasks.ListByContext(r.Context(), contextID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tasks := make([]*a2a.Task, 0, len(records))
	for _, record := range records {
		tasks = append(tasks, record.Task)
	}

	writeJSON(w, tasks)
}

func (g *AgentGateway) handleTasksCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !g.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	taskID := r.PathValue("task_id")
	if taskID == "" {
		http.Error(w, "task_id is required", http.StatusBadRequest)
		return
	}

	record, err := g.onServerTasks.Get(r.Context(), taskID)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	if isTerminalState(record.Task.Status) {
		http.Error(w, "task already in terminal state, cannot cancel", http.StatusConflict)
		return
	}

	if err := g.onServerTasks.Cancel(r.Context(), taskID); err != nil {
		http.Error(w, "failed to update task", http.StatusInternalServerError)
		return
	}

	writeJSON(w, record.Task)
}

func (g *AgentGateway) handlePeerAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !g.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Host string `json:"host"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	host := strings.TrimSpace(body.Host)
	host = strings.TrimRight(host, "/")
	if host == "" {
		http.Error(w, "host is required", http.StatusBadRequest)
		return
	}
	g.AddHost(host)
	// Index card now so the next refreshPeersAndDispatch can resolve waits.
	if err := g.retrieveExternalAgentCard(r.Context(), host, true); err != nil {
		g.log.Error("peer added but agent card refresh failed", "host", host, "error", err)
		writeJSON(w, map[string]any{"status": "added", "host": host, "card_error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"status": "added", "host": host})
}

func (g *AgentGateway) handlePeerRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !g.authorize(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body struct {
		Host string `json:"host"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	host := strings.TrimSpace(body.Host)
	host = strings.TrimRight(host, "/")
	if host == "" {
		http.Error(w, "host is required", http.StatusBadRequest)
		return
	}
	g.RemoveHost(host)
	writeJSON(w, map[string]any{"status": "removed", "host": host})
}

func (g *AgentGateway) authorize(r *http.Request) bool {
	return meshauth.Authorize(r)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func pendingOnServerTask(sessionID, actionName string, actionType types.ActionType) *a2a.Task {
	return &a2a.Task{
		Id:        onServerTaskID(sessionID, actionName, actionType),
		ContextId: sessionID,
		Status:    a2a.TaskState_TASK_STATE_SUBMITTED,
		Artifacts: []*a2a.Artifact{
			{
				Name: actionName,
			},
		},
	}
}
