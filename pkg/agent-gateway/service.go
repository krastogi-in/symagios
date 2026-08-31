package agentgateway

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"symagios/egress"
	"symagios/encrypt"
	"symagios/engine"
	applog "symagios/log"
	"symagios/proto/a2a"
	"symagios/storage/sqlite"
	"symagios/utils"
	"symagios/utils/types"
)

// AgentGateway exposes A2A HTTP endpoints backed by the shared SQLite store.
type AgentGateway struct {
	agentCard     *a2a.AgentCard
	server        *http.Server
	client        *http.Client
	db            *sqlite.Client
	config        GatewayConfig
	onServerTasks *taskStore
	externalTasks *taskStore
	peers         *peerSet
	hostRegistry  *hostRegistry
	agentCards    *utils.LRUCache[*a2a.AgentCard]
	encryptKey    *encrypt.EncryptKey
	cancel        context.CancelFunc
	log           *applog.Logger
}

const (
	MaxTaskAttempts = 3
)

// New creates a gateway bound to the given store client.
func New(db *sqlite.Client, encryptKey *encrypt.EncryptKey, cfg GatewayConfig) *AgentGateway {
	return &AgentGateway{
		db:            db,
		config:        cfg,
		onServerTasks: newTaskStore(db, utils.NewLRUCache[*TaskRecord](256)),
		externalTasks: newTaskStore(db, utils.NewLRUCache[*TaskRecord](256)),
		peers:         newPeerSet(),
		hostRegistry:  newHostRegistry(),
		agentCards:    utils.NewLRUCache[*a2a.AgentCard](64),
		encryptKey:    encryptKey,
		log:           applog.Default().With("component", "agent-gateway"),
	}
}

func (g *AgentGateway) AddHost(host string) {
	g.peers.Add(host)
}

func (g *AgentGateway) RemoveHost(host string) {
	host = strings.TrimSpace(host)
	host = strings.TrimRight(host, "/")
	g.peers.Remove(host)
	g.hostRegistry.ClearHost(host)
	if g.agentCards != nil {
		g.agentCards.Remove(host)
	}
}

// Start configures routes and begins serving HTTP. It returns once the listener is up.
// Peer hosts are seeded from g.config.ExternalHosts and can also be added/removed via HTTP.
func (g *AgentGateway) Start(ctx context.Context, host string, port int) error {
	if host == "" {
		host = "0.0.0.0"
	}
	if port <= 0 {
		if g.config.tlsEnabled() {
			port = 8443
		} else {
			port = 8080
		}
	}

	baseURL := fmt.Sprintf("http://%s:%d", host, port)
	if g.config.tlsEnabled() {
		baseURL = fmt.Sprintf("https://%s:%d", host, port)
	}
	if host == "0.0.0.0" {
		if g.config.tlsEnabled() {
			baseURL = fmt.Sprintf("https://127.0.0.1:%d", port)
		} else {
			baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)
		}
	}
	if g.config.URL == "" {
		// A2A: AgentCard.url is the preferred service endpoint (not the well-known path).
		g.config.URL = baseURL
	} else {
		g.config.URL = strings.TrimRight(strings.TrimSpace(g.config.URL), "/")
	}
	if g.config.Host == "" {
		g.config.Host = hostAuthority(hostBaseURL(g.config.URL))
	}
	if g.config.AgentName == "" {
		g.config.AgentName = "Agent Gateway"
	}
	if g.config.Version == "" {
		g.config.Version = "1.0.0"
	}

	g.agentCard = &a2a.AgentCard{
		Name:               g.config.AgentName,
		Description:        "Agent Gateway",
		Version:            g.config.Version,
		Url:                g.config.URL,
		PreferredTransport: "JSONRPC",
		Skills:             []*a2a.AgentSkill{},
		DefaultInputModes: []string{
			"application/json",
		},
		DefaultOutputModes: []string{
			"application/json",
		},
		Capabilities: &a2a.AgentCapabilities{
			Streaming:         false,
			PushNotifications: false,
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/agent-card.json", g.handleAgentCard)
	mux.HandleFunc("/message/send", g.handleMessageSend)
	mux.HandleFunc("/tasks/{task_id}", g.handleTasksGet)
	mux.HandleFunc("/tasks/list", g.handleTasksList)
	mux.HandleFunc("/tasks/{task_id}/cancel", g.handleTasksCancel)
	mux.HandleFunc("POST /peers", g.handlePeerAdd)
	mux.HandleFunc("DELETE /peers", g.handlePeerRemove)

	ctx, cancel := context.WithCancel(ctx)
	g.cancel = cancel

	g.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", host, port),
		Handler: mux,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}
	if g.config.tlsEnabled() {
		tlsCfg, err := loadServerTLS(g.config.TLSCertFile, g.config.TLSKeyFile, g.config.TLSCAFile)
		if err != nil {
			return fmt.Errorf("agent-gateway tls: %w", err)
		}
		g.server.TLSConfig = tlsCfg
	}

	egClient, err := egress.Config{
		Proxy:      g.config.EgressProxy,
		Source:     "agent-gateway",
		CACert:     firstNonEmpty(os.Getenv("VAULT_CACERT"), g.config.TLSCAFile),
		ClientCert: firstNonEmpty(os.Getenv("VAULT_CLIENT_CERT"), g.config.TLSCertFile),
		ClientKey:  firstNonEmpty(os.Getenv("VAULT_CLIENT_KEY"), g.config.TLSKeyFile),
	}.HTTPClient(30 * time.Second)
	if err != nil {
		return fmt.Errorf("agent-gateway egress client: %w", err)
	}
	g.client = egClient

	if len(g.config.ExternalHosts) > 0 {
		for _, h := range g.config.ExternalHosts {
			g.AddHost(h)
		}
	}

	go func() {
		var err error
		if g.config.tlsEnabled() {
			err = g.server.ListenAndServeTLS("", "")
		} else {
			err = g.server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			g.log.Error("http server stopped", "error", err)
		}
	}()

	go g.pollExternalActions(ctx)
	go g.pollOnServerTasks(ctx)

	g.log.Info("agent gateway started", "addr", g.server.Addr, "tls", g.config.tlsEnabled(), "seed_hosts", g.peers.Len())
	return nil
}

func (g *AgentGateway) pollExternalActions(ctx context.Context) {
	// Drain any WaitingForExternal* that were parked before peers were up.
	g.refreshPeersAndDispatch(ctx)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.refreshPeersAndDispatch(ctx)
		}
	}
}

// refreshPeersAndDispatch re-indexes peer AgentCards then retries WaitingForExternal*.
// Late joiners (new peers or new skills on a card) are picked up on the next cycle.
func (g *AgentGateway) refreshPeersAndDispatch(ctx context.Context) {
	for _, host := range g.peers.List() {
		if err := g.retrieveExternalAgentCard(ctx, host, true); err != nil {
			g.log.Debug("peer card refresh skipped (peer may still be joining)",
				"host", host, "error", err)
		}
	}
	g.processExternalActionRuns(ctx, types.WaitingForExternalAgentAction)
	g.processExternalActionRuns(ctx, types.WaitingForExternalSkillAction)
	g.processExternalActionRuns(ctx, types.WaitingForExternalSubAgentAction)
}

func (g *AgentGateway) processExternalActionRuns(ctx context.Context, status types.ActionRunStatus) {
	actionRuns, err := g.db.ListActionRunsByStatus(ctx, status)
	if err != nil {
		g.log.Error("failed to list action runs by status", "status", status, "error", err)
		return
	}

	for _, actionRun := range actionRuns {
		// Fetch: fold Result into Payload history (keep prior summaries), clear Result.
		folded, cleared, changed, err := encrypt.FoldResultIntoPayload(g.encryptKey, actionRun.Payload, actionRun.Result)
		if err != nil {
			g.log.Error("failed to fold result into payload", "session_id", actionRun.SessionID, "error", err)
			continue
		}
		if changed {
			actionRun.Payload, actionRun.Result = folded, cleared
			if err := g.db.UpdateActionRunStatus(ctx,
				actionRun.SessionID, actionRun.ActionName, actionRun.ActionType,
				actionRun.Status, actionRun.Payload, actionRun.Result); err != nil {
				g.log.Error("failed to persist folded payload", "session_id", actionRun.SessionID, "error", err)
				continue
			}
		}

		actionName := actionRun.ActionName
		actionType := actionRun.ActionType
		if actionName == "" || actionType == "" {
			g.log.Error("external action run missing name/type", "session_id", actionRun.SessionID)
			continue
		}

		taskKey := fmt.Sprintf("%s:%s:%s", actionRun.SessionID, actionName, actionType)

		if taskRecord, err := g.externalTasks.Get(ctx, taskKey); err == nil {
			if taskRecord.Attempts >= MaxTaskAttempts {
				g.log.Error("task has reached maximum attempts", "task_id", taskKey, "attempts", taskRecord.Attempts)
				result := fmt.Sprintf("Task has reached maximum attempts: %d", MaxTaskAttempts)
				encResult, err := g.encryptForStore(result)
				if err != nil {
					// Keep tracker — do not redispatch; retry encrypt next cycle.
					g.log.Error("failed to encrypt action result", "session_id", actionRun.SessionID, "error", err)
					continue
				}
				if err := g.db.UpdateActionRunStatus(ctx,
					actionRun.SessionID, actionRun.ActionName, actionRun.ActionType,
					types.WaitingForHumanInput, actionRun.Payload, encResult); err != nil {
					g.log.Error("failed to update action run", "session_id", actionRun.SessionID, "error", err)
					continue
				}
				g.externalTasks.Remove(ctx, taskKey)
				continue
			}
			task := taskRecord.Task
			host := taskRecord.Host
			if isTerminalState(task.Status) {
				encResult, err := g.encryptForStore(task.StatusMessage)
				if err != nil {
					// Keep tracker so we do not SendMessage again; retry encrypt next poll.
					g.log.Error("failed to encrypt action result", "session_id", actionRun.SessionID, "error", err)
					continue
				}
				var status types.ActionRunStatus
				if task.Status == a2a.TaskState_TASK_STATE_FAILED ||
					task.Status == a2a.TaskState_TASK_STATE_CANCELED {
					status = types.Failed
				} else {
					status = types.Pending
				}
				if err := g.db.UpdateActionRunStatus(ctx, actionRun.SessionID, actionRun.ActionName, actionRun.ActionType, status, actionRun.Payload, encResult); err != nil {
					g.log.Error("failed to update action run", "session_id", actionRun.SessionID, "error", err)
					continue
				}
				g.externalTasks.Remove(ctx, taskKey)
				continue
			}

			modifiedTask, err := g.remoteClient(host).GetTask(ctx, task.Id)
			if err != nil {
				taskRecord.Attempts++
				g.log.Error("failed to get task", "host", host, "task_id", taskKey, "error", err)
				if err := g.externalTasks.Save(ctx, taskRecord); err != nil {
					g.log.Error("failed to save task", "task_id", task.Id, "error", err)
				}
				continue
			}
			if taskRecord.Task != modifiedTask {
				taskRecord.Task = modifiedTask
				taskRecord.Attempts = 0
				if err := g.externalTasks.UpdateStatus(ctx, taskKey, modifiedTask.Status, modifiedTask.StatusMessage); err != nil {
					g.log.Error("failed to update task status", "task_id", taskKey, "error", err)
				}
			} else {
				taskRecord.Attempts++
				if err := g.externalTasks.Save(ctx, taskRecord); err != nil {
					g.log.Error("failed to save task", "task_id", modifiedTask.Id, "error", err)
				}
			}
			continue
		}

		// Resolve peer from AgentCard skills indexed into hostRegistry.
		// Same-worker-group late uploads are promoted to Pending by the engine scheduler.
		host, ok := g.hostRegistry.Get(actionType, actionName)
		if !ok {
			g.log.Debug("no peer advertises capability yet; will retry",
				"action_type", actionType, "action_name", actionName)
			continue
		}

		// Send stored ciphertext in Parts (shared Vault key on peer); no plaintext on wire.
		task, err := g.remoteClient(host).SendMessage(ctx, &a2a.Message{
			ContextId: actionRun.SessionID,
			Metadata: map[string]string{
				string(actionType): actionName,
			},
			Parts: payloadParts(actionRun.Payload),
		})
		if err != nil {
			g.log.Error("failed to send message", "host", host, "error", err)
			continue
		}

		if err := g.externalTasks.Save(ctx, NewTaskRecord(task, host, 0)); err != nil {
			g.log.Error("failed to save task", "task_id", task.Id, "error", err)
		}

		g.log.Debug(
			"dispatched external action",
			"host", host,
			"session_id", actionRun.SessionID,
			"action_name", actionName,
			"task_id", task.Id,
		)
	}
}

// Stop shuts down the HTTP server and cancels the gateway context.
func (g *AgentGateway) Stop() {
	if g.cancel != nil {
		g.cancel()
	}
	if g.server == nil {
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := g.server.Shutdown(shutdownCtx); err != nil {
		g.log.Error("http server shutdown failed", "error", err)
		return
	}
	if g.client != nil {
		g.client.CloseIdleConnections()
	}
	g.log.Info("agent gateway stopped")
}

// DB returns the underlying store client.
func (g *AgentGateway) DB() *sqlite.Client {
	return g.db
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// payloadParts wraps ciphertext (or empty) for A2A Message.Parts.
func payloadParts(payload string) []*a2a.Part {
	if payload == "" {
		return nil
	}
	return []*a2a.Part{{Content: &a2a.Part_Text{Text: payload}}}
}

// partsCiphertext joins inbound Message.Parts text (peer sends stored ciphertext).
func partsCiphertext(m *a2a.Message) string {
	if m == nil {
		return ""
	}
	var b strings.Builder
	for _, p := range m.GetParts() {
		if t := p.GetText(); t != "" {
			b.WriteString(t)
		}
	}
	return b.String()
}

// decryptStoredPayload reads SQLite payload plaintext (ciphertext or plaintext fallback).
func (g *AgentGateway) decryptStoredPayload(stored string) string {
	if stored == "" {
		return ""
	}
	if g.encryptKey == nil {
		return engine.PublicFacingText(stored)
	}
	return engine.PublicFacingText(g.encryptKey.DecryptStored(stored))
}

// encryptForStore encrypts plaintext for SQLite writes. Store updates must be ciphertext.
func (g *AgentGateway) encryptForStore(plain string) (string, error) {
	if g.encryptKey == nil {
		return "", fmt.Errorf("encrypt key missing")
	}
	return g.encryptKey.Encrypt(engine.PublicFacingText(plain))
}

