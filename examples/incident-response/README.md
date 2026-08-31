# Example: multi-agent incident response

This folder is a **reviewable use case**, not production config. It shows how one orchestrator agent fans out into skills and sub-agents under Symagios secured orchestration (content guards, MCP gateway, HITL, process guards).

You will want to edit names, prompts, and data to match your environment.

## Cast

| Unit | Type | Role |
|------|------|------|
| `orchestrator` | agent | Owns the case; decides next ACTION (skill / subagent / MCP / human) |
| `triage` | skill | Classifies severity and extracts IOCs from the alert |
| `investigator` | sub-agent | Deepens on one host/user; may call MCP tools |
| `reporter` | agent | Writes the final handoff for humans |
| `guard-hitl` | agent | Parallel demo: synthetic PII + `CONTENT_MODE=block` → HITL |

Flow (happy path):

```text
alert → orchestrator
          ├─ SKILL::triage {…}
          ├─ SUB_AGENT::investigator {…}   (maybe MCP::lookup_docs / echo)
          ├─ NEED_HUMAN_INPUT::approve {…} if MCP tool missing or high severity
          └─ AGENT::reporter {…}

# Parallel (optional):
pii-fixture → guard-hitl  → pre-LLM block → WAITING_FOR_HUMAN_INPUT (watch)
```

## Layout

```text
examples/incident-response/
  README.md                 # this file
  case/alert.json           # sample IR input
  case/pii-fixture.json     # synthetic PII for guard-hitl
  agents/
    orchestrator.md         # role + goals (ACTION grammar comes from engine system prompt)
    reporter.md
    guard-hitl.md           # content-guard → HITL demo agent
  skills/
    triage.md
  subagents/
    investigator.md
  mcp/
    registry.snippet.json   # tools this case expects to be attached
  runbook.md                # how to exercise on a laptop
```

## Security touchpoints exercised

1. **System prompt markers** — wrap each unit’s instructions with Symagios markers before the LLM.
2. **Content guards** — pre/post LLM; store/retrieve if you persist case notes.
3. **MCP gateway** — only attached tools; missing tool → `WAITING_FOR_HUMAN_INPUT`.
4. **Egress** — `send_email` is egress-marked; PII in args blocks the tool.
5. **OPA roles** — operator / agent-runner / admin (see `deploy/docs/mesh-hardening.md`).
6. **Landlock/seccomp** — investigator/script work under process guards when the engine runs them.

## Packaging (TUS upload)

One orchestrator upload carries the case plus child unit bodies:

| Field | Content |
|-------|---------|
| `data` | `agents/orchestrator.md` + `case/alert.json` |
| `capabilities[].name` / `type` | Local routing (`triage` SKILL, `investigator` SUB_AGENT, `reporter` AGENT) |
| `capabilities[].data_source` | Full markdown of that unit (`skills/triage.md`, …) |

Do **not** TUS-upload each skill as its own Pending agent — that would run them immediately with no case context. Pack them into `data_source` on the orchestrator manifest (see `HOW_TO_EXECUTE.md` §3). The engine prepends `data_source` to the child ACTION JSON when dispatching.

## Editing tips

- Keep next-action *names* consistent with capabilities you upload (`triage`, `investigator`, …). The `KIND::name{json}` grammar is injected by the engine (`pkg/utils.NextActionPrompt`) — do not re-teach it in every agent file.
- Do not put API keys in these markdown files — use Vault (`symagios/llm`, `symagios/mcp/...`).
