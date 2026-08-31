# Governed web search

The engine does the same restriction as `examples/governed-search/anthropic_example.py`. On every Claude or OpenCodex call from `CompleteLLM`, the request includes only Symagios's custom `web_search` tool. Anthropic's server tool (`type: web_search_20250305`) and OpenAI's `type: web_search` are never sent. If the provider still returns a server search result, the call fails.

When the model emits `tool_use` for `web_search`, or agents enqueue `WEB_SEARCH::web_search{"query":"..."}`, the engine parks *this* run (`WaitingForWebSearch`), calls the search broker, then stores the Tavily outcome as an encrypted **Result** and moves the run back to **Pending**. On the next scheduler fetch, `FoldResultIntoPayload` appends that outcome into Payload history and clears Result.

```text
WEB_SEARCH::web_search{"query":"..."}
```

The engine then:

1. Parses that action and parks the current run as `WaitingForWebSearch`.
2. The worker calls the search broker (`pkg/cmd/search-broker`) over **mTLS** (`SYMAGIOS_SEARCH_BROKER`, default `https://search-broker:8443/v1/search`). Only the **engine** workload client cert is accepted.
3. `pkg/search-gateway` loads the Tavily API key from Vault (`secret/symagios/tavily`, or `TAVILY_API_KEY` as a fallback).
4. The HTTP call goes out through **envoy-egress** with mTLS (`Host: api.tavily.com`); Lua sets `X-Symagios-Source: symagios-engine` from the search-broker / engine workload cert.
5. Egress RBAC + OPA decide whether that source may talk to Tavily’s `/search`.
6. On success: encrypted Result + `Pending` (fold into Payload on next fetch). On broker failure: `Failed` with an encrypted error Result (e.g. search unavailable).

## Compose (auth profile)

| Item | Value |
|------|--------|
| Service | `search-broker` |
| Listen | `:8443` (mTLS; requires engine client cert) |
| Engine env | `SYMAGIOS_SEARCH_BROKER=https://search-broker:8443/v1/search` |
| Vault path | `secret/symagios/tavily` field `api_key` |
| Egress | `https://envoy-egress:10001` |

Examples under `examples/governed-search/` use the same custom-tool JSON. Point `SYMAGIOS_SEARCH_BROKER` at the broker (and present the engine cert when calling mTLS).
