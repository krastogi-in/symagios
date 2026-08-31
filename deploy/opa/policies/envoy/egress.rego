package envoy.egress

import input.attributes.request.http as http_request
import future.keywords.if
import future.keywords.in

default allow := false

source := header_value("x-symagios-source")

authority := destination_authority

destination_authority := v if {
	v := header_value("x-egress-authority")
	v != ""
}

destination_authority := v if {
	not header_value("x-egress-authority")
	v := header_value(":authority")
	v != ""
}

destination_authority := v if {
	not header_value("x-egress-authority")
	not header_value(":authority")
	v := header_value("host")
	v != ""
}

authority_host := host if {
	parts := split(authority, ":")
	count(parts) > 1
	host := parts[0]
}

authority_host := authority if {
	not contains(authority, ":")
}

allow if {
	source == "agent-gateway"
	a2a_route_allowed
}

allow if {
	source == "symagios-engine"
	search_route_allowed
}

allow if {
	source == "symagios-engine"
	llm_route_allowed
}

allow if {
	source == "mcp-gateway"
	mcp_upstream_allowed
}

a2a_route_allowed if {
	http_request.method == "POST"
	http_request.path == "/message/send"
}

# CONNECT only to mesh / allowlisted peer-style hosts (RBAC also enforces :authority).
a2a_route_allowed if {
	http_request.method == "CONNECT"
	a2a_connect_host_allowed
}

a2a_route_allowed if {
	http_request.method == "GET"
	glob.match("/tasks/**", ["/"], http_request.path)
}

a2a_route_allowed if {
	http_request.method == "GET"
	startswith(http_request.path, "/.well-known/")
}

a2a_route_allowed if {
	http_request.method == "POST"
	endswith(http_request.path, "/cancel")
}

a2a_connect_host_allowed if {
	authority_host == "localhost"
}

a2a_connect_host_allowed if {
	authority_host == "127.0.0.1"
}

a2a_connect_host_allowed if {
	authority_host == "agent-gateway"
}

a2a_connect_host_allowed if {
	endswith(authority_host, ".symagios.local")
}

search_route_allowed if {
	http_request.method == "POST"
	http_request.path == "/search"
	search_host_allowed
}

search_route_allowed if {
	http_request.method == "CONNECT"
	search_host_allowed
}

search_host_allowed if {
	authority_host == "api.tavily.com"
}

llm_route_allowed if {
	http_request.method == "POST"
	llm_host_allowed
}

llm_route_allowed if {
	http_request.method == "CONNECT"
	llm_host_allowed
}

llm_host_allowed if {
	authority_host == "api.anthropic.com"
}

llm_host_allowed if {
	authority_host == "api.openai.com"
}

# Claude on Vertex AI (regional, global, or multi-region REP).
llm_host_allowed if {
	endswith(authority_host, "-aiplatform.googleapis.com")
}

llm_host_allowed if {
	authority_host == "aiplatform.googleapis.com"
}

llm_host_allowed if {
	endswith(authority_host, ".rep.googleapis.com")
	startswith(authority_host, "aiplatform.")
}

# MCP upstream hosts must be explicitly allowlisted (extend as you attach servers).
mcp_upstream_allowed if {
	http_request.method == "POST"
	mcp_host_allowed
}

mcp_upstream_allowed if {
	http_request.method == "CONNECT"
	mcp_host_allowed
}

mcp_host_allowed if {
	authority_host == "localhost"
}

mcp_host_allowed if {
	authority_host == "127.0.0.1"
}

header_value(name) := value if {
	value := http_request.headers[name]
}

header_value(name) := value if {
	value := http_request.headers[lower(name)]
}
