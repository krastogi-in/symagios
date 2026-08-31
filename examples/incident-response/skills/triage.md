# Skill: triage

Classify the alert and extract a compact IOC list.

## Input
JSON alert (`case_id`, `summary`, `host`, `user`, `indicators`, …).

## Output JSON
```json
{
  "case_id": "...",
  "severity": "low|medium|high|critical",
  "ioc": ["..."],
  "needs_investigation": true,
  "rationale": "one short paragraph"
}
```

## Hard rules
- Do not call MCP or email from this skill.
- Do not escalate severity without evidence from the alert fields.
