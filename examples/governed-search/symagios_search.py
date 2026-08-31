"""Shared governed search client for provider examples."""

import json
import os
import ssl
import urllib.error
import urllib.request

# Compose (auth): https://search-broker:8443/v1/search with mTLS.
# Local plaintext: SYMAGIOS_SEARCH_BROKER=http://127.0.0.1:8091/v1/search
BROKER_URL = os.environ.get(
    "SYMAGIOS_SEARCH_BROKER",
    "https://search-broker:8443/v1/search",
)


def _env(*keys):
    for key in keys:
        value = os.environ.get(key)
        if value:
            return value
    return None


def _ssl_context():
    cert = _env("SYMAGIOS_SEARCH_TLS_CERT", "VAULT_CLIENT_CERT")
    key = _env("SYMAGIOS_SEARCH_TLS_KEY", "VAULT_CLIENT_KEY")
    ca = _env("SYMAGIOS_SEARCH_TLS_CA", "VAULT_CACERT")
    if not cert or not key:
        return None
    ctx = ssl.create_default_context(cafile=ca if ca else None)
    ctx.load_cert_chain(certfile=cert, keyfile=key)
    return ctx


def symagios_egress_search(query: str) -> str:
    """Single choke point: broker -> egress Envoy -> OPA -> Tavily."""
    payload = json.dumps({"query": query}).encode("utf-8")
    req = urllib.request.Request(
        BROKER_URL,
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(
            req, timeout=60, context=_ssl_context()
        ) as resp:
            body = json.loads(resp.read().decode("utf-8"))
            results = body.get("results", [])
            return body.get("text") or json.dumps(results)
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(
            f"governed search failed: {exc.code} {detail}"
        ) from exc
