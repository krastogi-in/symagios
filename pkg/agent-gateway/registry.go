package agentgateway

import (
	"slices"
	"strings"
	"sync"

	"symagios/proto/a2a"
	"symagios/utils/types"
)

// peerSet is the seeded A2A peer list. Its mutex only covers this set.
type peerSet struct {
	mu    sync.RWMutex
	hosts []string
}

func newPeerSet() *peerSet {
	return &peerSet{}
}

func (p *peerSet) Add(host string) {
	host = strings.TrimSpace(host)
	host = strings.TrimRight(host, "/")
	if host == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if slices.Contains(p.hosts, host) {
		return
	}
	p.hosts = append(p.hosts, host)
}

func (p *peerSet) Remove(host string) {
	host = strings.TrimSpace(host)
	host = strings.TrimRight(host, "/")
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.hosts[:0]
	for _, h := range p.hosts {
		if h != host {
			out = append(out, h)
		}
	}
	p.hosts = out
}

func (p *peerSet) List() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]string, len(p.hosts))
	copy(out, p.hosts)
	return out
}

func (p *peerSet) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.hosts)
}

// hostRegistry maps SKILL|AGENT|SUB_AGENT name → peer host used to reach it.
// Built from AgentCard skills when a peer card is fetched; keyed by the seed host
// we already talk to (not card.Url rewriting).
type hostRegistry struct {
	mu    sync.RWMutex
	byKey map[string]string
}

func newHostRegistry() *hostRegistry {
	return &hostRegistry{byKey: make(map[string]string)}
}

func hostMappingKey(actionType types.ActionType, actionName string) string {
	return string(actionType) + ":" + actionName
}

func (r *hostRegistry) Set(actionType types.ActionType, actionName, host string) {
	host = strings.TrimSpace(host)
	host = strings.TrimRight(host, "/")
	if host == "" || actionName == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byKey[hostMappingKey(actionType, actionName)] = host
}

func (r *hostRegistry) Get(actionType types.ActionType, actionName string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	host, ok := r.byKey[hostMappingKey(actionType, actionName)]
	return host, ok && host != ""
}

// IndexCard registers skills/agents/sub-agents from a peer AgentCard onto host.
func (r *hostRegistry) IndexCard(host string, card *a2a.AgentCard) {
	if card == nil {
		return
	}
	host = strings.TrimSpace(host)
	host = strings.TrimRight(host, "/")
	if host == "" {
		return
	}
	for _, item := range card.Skills {
		if item == nil || item.Id == "" || len(item.Tags) == 0 {
			continue
		}
		if slices.Contains(item.Tags, string(types.Skill)) {
			r.Set(types.Skill, item.Id, host)
		}
		if slices.Contains(item.Tags, string(types.SubAgent)) {
			r.Set(types.SubAgent, item.Id, host)
		}
		if slices.Contains(item.Tags, string(types.Agent)) {
			r.Set(types.Agent, item.Id, host)
		}
	}
}

// ClearHost drops all capability mappings that pointed at host.
func (r *hostRegistry) ClearHost(host string) {
	host = strings.TrimSpace(host)
	host = strings.TrimRight(host, "/")
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, h := range r.byKey {
		if h == host {
			delete(r.byKey, k)
		}
	}
}
