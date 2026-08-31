# How to execute

Bring up the secured stack and run a Claude-backed use case (incident-response shape). This is a laptop POC runbook, not production ops.

## Prerequisites

- Podman (default) with compose provider (`podman compose` — usually the Docker Compose plugin)
- `jq`, `curl`
- Either an Anthropic API key **or** GCP access for Claude on Vertex (see below)

Optional: `SYMAGIOS_CONTAINER_RUNTIME=docker` if you prefer Docker Engine instead of Podman.

Enable the rootless Podman API once per login session (install scripts also try this):

```bash
systemctl --user enable --now podman.socket
```

## 1. Bring up the mesh

Preferred (start / stop / wipe DB):

```bash
cd /path/to/symagios
# optional: export ANTHROPIC_API_KEY=…  and/or VERTEX_* for Claude
./scripts/stack-start.sh --build --watch   # auth if needed + workloads + watch
# …
./scripts/stack-stop.sh                    # stop containers, keep volumes
./scripts/stack-delete.sh                  # stop + wipe SQLite (symagios-data)
./scripts/stack-delete.sh --all --yes      # also wipe Keycloak volumes
./scripts/stack-start.sh --auth-force      # after --all, re-seed Vault/Keycloak
```

Make aliases: `make stack-start`, `make stack-stop`, `make stack-delete`
(`STACK_START_FLAGS=--watch`, `STACK_DELETE_FLAGS=--all --yes`).

Manual equivalent:

```bash
export ANTHROPIC_API_KEY='sk-ant-…'   # used by vault-install if LLM_API_KEY unset
make auth-install                     # secrets → vault → certs → keycloak → opa → envoy
./scripts/compose.sh --profile auth up -d --build
```

Default LLM is direct Anthropic (`provider: claude` in `deploy/llm/config.yaml`). For Claude on Vertex, switch that file before starting the engine (next section).

Re-seed a Vault Anthropic key if needed:

```bash
./scripts/compose.sh --profile auth exec -T vault \
  sh -c 'export VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=$VAULT_TOKEN; vault kv put secret/symagios/llm api_key="'"$ANTHROPIC_API_KEY"'"'
```

Optional HITL terminal (`watch` profile). Engine must already be running (auth
stack). Use `--no-recreate` so Compose still waits on `depends_on: engine` but
does **not** replace a running engine (env/image drift would otherwise recreate it
and drop in-flight TUS / LLM state):

```bash
./scripts/compose.sh --profile auth --profile watch up -d --no-recreate watch
# interactive one-shot:
./scripts/compose.sh --profile auth --profile watch run --rm --no-deps watch
# (--no-deps on run: engine already up; avoids dependency recreate path entirely)
```

## Claude on Vertex AI

Use this instead of `ANTHROPIC_API_KEY`. Vertex posts to `:rawPredict` with a Google Bearer token (not `x-api-key`).

1. Set `deploy/llm/config.yaml`:

```yaml
llm:
  provider: claude-vertex
  model: claude-sonnet-4@20250514
  project: YOUR_GCP_PROJECT
  location: us-east5   # or global / us / eu
  max_tokens: 4096
  vault_path: symagios/llm
  vault_field: access_token
```

2. Auth (preferred → fallback):
   - **ADC (recommended):** `gcloud auth application-default login` once, then
     mount `~/.config/gcloud/application_default_credentials.json` (compose default).
     The engine refreshes tokens itself — **no** `VERTEX_ACCESS_TOKEN` on every restart.
   - `VERTEX_ACCESS_TOKEN` / `GOOGLE_OAUTH_ACCESS_TOKEN` — short-lived (~1h); only for one-shot tests
     or set `SYMAGIOS_VERTEX_FORCE_TOKEN=1` to skip ADC.
   - Vault field `access_token` at `secret/symagios/llm` (also static unless you rotate it)

3. Start (ADC path — no token export):

```bash
gcloud auth application-default login   # once per laptop user
export VERTEX_PROJECT=YOUR_GCP_PROJECT
export VERTEX_LOCATION=us-east5         # or match deploy/llm/config.yaml
./scripts/stack-start.sh --build --watch
```

One-shot bearer (not recommended for long runs):

```bash
export VERTEX_ACCESS_TOKEN="$(gcloud auth print-access-token)"
export SYMAGIOS_VERTEX_FORCE_TOKEN=1
./scripts/compose.sh --profile auth up -d --no-deps --force-recreate engine
```

Egress RBAC/OPA allow `*-aiplatform.googleapis.com`, `aiplatform.googleapis.com`, and `aiplatform.{us,eu}.rep.googleapis.com`.

## 2. Get an operator token

All API calls go through Envoy ingress: `https://localhost:8443` (not Keycloak `:8080`). Example curls use `-sk` so you do not need the Vault CA in your trust store; `keycloak-admin.sh token` already uses `deploy/certs/ca.crt`.

```bash
source .env
TOKEN=$(./scripts/keycloak-admin.sh token "$SYMAGIOS_OPERATOR_USER" "$SYMAGIOS_OPERATOR_PASSWORD" | jq -r .access_token)
# Expect a long JWT string. If you see JSON with "error", fix Keycloak/users before continuing.
echo "token_len=${#TOKEN}"
```

Keep this shell open — later steps need `$TOKEN`.

**Next:** upload one agent so the engine calls Claude (section 3), then watch logs (section 4).

## 3. Upload an agent (smoke test)

Goal of this section: one TUS upload of the incident-response orchestrator + alert fixture,
with child unit bodies packed into `capabilities[].data_source` when those files exist.
That proves auth → entry → engine → LLM egress, and lets `SKILL::triage` (etc.) run locally
with the real skill/sub-agent prompts — without separate TUS uploads per unit.

Packaging is still manual. Body shape:

```json
{
  "name": "orchestrator",
  "data": "<orchestrator prompt + case JSON>",
  "capabilities": [
    {"name":"triage","type":"SKILL","data_source":"<skills/triage.md>"},
    {"name":"investigator","type":"SUB_AGENT","data_source":"<subagents/investigator.md>"},
    {"name":"reporter","type":"AGENT","data_source":"<agents/reporter.md>"}
  ],
  "workflows": []
}
```

`name`+`type` → local routing. `data_source` → child worker prompt (prepended to the ACTION JSON).
Omit `data_source` only if you intentionally want name-only stubs.

Run all of this in the same shell as section 2 (`$TOKEN` must be set):

```bash
SESSION=demo-$(date +%s)
BODY=$(jq -n \
  --arg data "$(cat examples/incident-response/agents/orchestrator.md)

ALERT:
$(cat examples/incident-response/case/alert.json)" \
  --arg triage "$(cat examples/incident-response/skills/triage.md)" \
  --arg investigator "$(cat examples/incident-response/subagents/investigator.md)" \
  --arg reporter "$(cat examples/incident-response/agents/reporter.md)" \
  '{name:"orchestrator", data:$data, capabilities:[
     {"name":"triage","type":"SKILL","data_source":$triage},
     {"name":"investigator","type":"SUB_AGENT","data_source":$investigator},
     {"name":"reporter","type":"AGENT","data_source":$reporter}
   ], workflows:[]}')

LEN=$(printf '%s' "$BODY" | wc -c)
META=$(printf 'session_id %s,agent_name %s' \
  "$(printf '%s' "$SESSION" | base64 -w0)" \
  "$(printf 'orchestrator' | base64 -w0)")

# 1) Create upload — expect a Location header path (e.g. /agents/…)
LOC=$(curl -sk -D - -o /dev/null \
  -H "Authorization: Bearer $TOKEN" \
  -H "Upload-Length: $LEN" \
  -H "Upload-Metadata: $META" \
  -H "Tus-Resumable: 1.0.0" \
  -X POST "https://localhost:8443/agents" | awk -F': ' 'tolower($1)=="location"{print $2}' | tr -d '\r')
echo "LOC=$LOC"
# If LOC is empty: dump the status line — 401 "Jwt issuer is not configured" means an old
# token iss (https://localhost/… without :8443). Re-source .env, get a fresh TOKEN, retry.

# 2) Finalize (single PATCH with full body) — expect HTTP 204
curl -sk -o /dev/null -w "finalize_http=%{http_code}\n" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Upload-Offset: 0" \
  -H "Content-Type: application/offset+octet-stream" \
  -H "Tus-Resumable: 1.0.0" \
  --data-binary "$BODY" \
  -X PATCH "https://localhost:8443${LOC}"
```

What the stack does after a successful finalize:

1. agent-entry encrypts the payload and enqueues a `Pending` action  
2. engine scheduler claims it and calls Claude (Anthropic or Vertex via egress)  
3. `ParseNextAction` may emit `SKILL::triage{…}` etc.

With `capabilities` populated as above, children stay **local** (`Pending`). Name-only caps
without `data_source` still route locally but the child only sees the ACTION JSON.
Missing from `capabilities` entirely → `WaitingForExternal*` (peer / later package).

**Next:** follow engine logs (section 4). Do not jump to MCP/HITL until you see an LLM call succeed.

## 4. Watch it run

**Action table (best observability):** poll every status/result change on `action_runs`:

```bash
# Detached observer (logs only — typing in `logs -f` does NOT answer HITL)
./scripts/compose.sh --profile auth --profile watch up -d --no-recreate watch
./scripts/compose.sh --profile auth --profile watch logs -f watch

# Answer HITL (interactive TTY — do not use `logs -f` for typing):
SYMAGIOS_WATCH_HITL=1 ./scripts/compose.sh --profile auth --profile watch run --rm --no-deps watch
# type reply at the `>` prompt, Enter
```

You should see lines like `[action HH:MM:SS] NEW … status=PENDING` then `PENDING → RUNNING` → `COMPLETED` (and child skills). Filter with `--session $SESSION` after upload.

**Engine / mesh logs** (LLM errors, egress):

```bash
./scripts/compose.sh --profile auth logs -f engine agent-gateway agent-entry
```

Success signals: agent-entry accepts the PATCH; watch shows orchestrator → triage (etc.); engine logs an LLM request (no 401 from Vertex / Anthropic, no egress deny). Failures after upload are almost always LLM config/token or egress — not the Keycloak JWT.

On startup the engine should log `worker cgroup attached` (four times). That needs the compose `security_opt` / `cap_add` on `engine` (already in `docker-compose.yml`). Under rootless Podman you may still see `ebpf LSM unavailable` — syscall policy needs a rootful runtime; workers still run with cgroup isolation.

## 5. After the smoke test works

| Order | Do this | Why |
|------|---------|-----|
| 1 | Orchestrator + alert + unit `data_source` (sections 3–4) | Claude + local triage/investigator/reporter |
| 2 | MCP attach from `examples/incident-response/mcp/registry.snippet.json` | Tools / HITL |
| 3 | `watch` profile → answer `NEED_HUMAN_INPUT` | Human-in-the-loop |
| 4 | Parallel `guard-hitl` agent + `SYMAGIOS_CONTENT_MODE=block` | Content guard → HITL |
| 5 | Real `TAVILY_API_KEY` in Vault (when ready) | Billed governed search |

### Parallel demo: content guard → HITL

Default guards **redact** and continue. For HITL from guards, use **block** mode and
upload `guard-hitl` (does not replace the IR orchestrator path).

```bash
# 0) Engine in block mode (recreates engine once — export Vertex token if you use it)
source .env
export SYMAGIOS_CONTENT_MODE=block
export VERTEX_ACCESS_TOKEN="$(gcloud auth print-access-token)"   # if needed
./scripts/compose.sh --profile auth up -d --force-recreate engine

# 1) HITL watcher (must not recreate engine)
./scripts/compose.sh --profile auth --profile watch up -d --no-recreate watch
# When watch shows [human-input WAITING], answer in an interactive shell:
SYMAGIOS_WATCH_HITL=1 ./scripts/compose.sh --profile auth --profile watch run --rm --no-deps watch

# 2) Fresh operator JWT
TOKEN=$(./scripts/keycloak-admin.sh token "$SYMAGIOS_OPERATOR_USER" "$SYMAGIOS_OPERATOR_PASSWORD" | jq -r .access_token)

# 3) TUS upload guard-hitl + synthetic PII fixture
SESSION=guard-$(date +%s)
BODY=$(jq -n \
  --arg data "$(cat examples/incident-response/agents/guard-hitl.md)

PII_FIXTURE:
$(cat examples/incident-response/case/pii-fixture.json)" \
  '{name:"guard-hitl", data:$data, capabilities:[], workflows:[]}')
LEN=$(printf '%s' "$BODY" | wc -c)
META=$(printf 'session_id %s,agent_name %s' \
  "$(printf '%s' "$SESSION" | base64 -w0)" \
  "$(printf 'guard-hitl' | base64 -w0)")

LOC=$(curl -sk -D - -o /dev/null \
  -H "Authorization: Bearer $TOKEN" \
  -H "Upload-Length: $LEN" \
  -H "Upload-Metadata: $META" \
  -H "Tus-Resumable: 1.0.0" \
  -X POST "https://localhost:8443/agents" | awk -F': ' 'tolower($1)=="location"{print $2}' | tr -d '\r')
echo "LOC=$LOC"

curl -sk -o /dev/null -w "finalize_http=%{http_code}\n" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Upload-Offset: 0" \
  -H "Content-Type: application/offset+octet-stream" \
  -H "Tus-Resumable: 1.0.0" \
  --data-binary "$BODY" \
  -X PATCH "https://localhost:8443${LOC}"
```

Expect: engine log `pre-LLM content guard blocked payload`; status `WAITING_FOR_HUMAN_INPUT`;
`watch` prompts you. When done, switch engine back to **redact** before IR uploads again
(block mode also trips on defanged-looking policy text / IOCs unless fixtures avoid
`exfiltrat*` and bare IPv4):

```bash
export SYMAGIOS_CONTENT_MODE=redact
export VERTEX_ACCESS_TOKEN="$(gcloud auth print-access-token)"   # if needed
./scripts/compose.sh --profile auth up -d --no-deps --force-recreate engine
```


More detail:

- [examples/incident-response](examples/incident-response) — cast, prompts, alert fixture  
- [examples/incident-response/runbook.md](examples/incident-response/runbook.md) — shorter exercise notes  
- [deploy/docs/README.md](deploy/docs/README.md) — auth, mTLS, search, MCP docs  

Governed search: `examples/governed-search/` is a provider-side contrast demo. Live Symagios search is engine `WEB_SEARCH::…` → search-broker → Tavily through egress — not Anthropic’s server search tool.

## Useful make targets

```bash
make auth-install    # full auth stack install
make docker-build    # image
make test            # unit tests
make mcp-gateway     # local MCP gateway (host), registry = deploy/mcp/registry.json
```

Compose profiles:

- `auth` — Vault, Keycloak, Envoy, OPA, engine, gateways, search, mcp  
- `watch` — CLI HITL watcher (depends on engine + Vault; start with `--no-recreate` / `run --no-deps`)
