package envoy.ingress

import input.attributes.request.http as http_request
import future.keywords.contains
import future.keywords.if
import future.keywords.in

default allow := false

allow if {
	startswith(http_request.path, "/.well-known/")
}

allow if {
	http_request.method == "POST"
	http_request.path == "/realms/symagios/protocol/openid-connect/token"
}

allow if {
	http_request.method == "GET"
	http_request.path == "/realms/symagios/protocol/openid-connect/auth"
}

allow if {
	startswith(http_request.path, "/realms/symagios/.well-known/")
}

allow if {
	http_request.method == "GET"
	startswith(http_request.path, "/tasks/")
	is_authenticated
}

allow if {
	http_request.method == "GET"
	http_request.path == "/tasks/list"
	is_authenticated
}

allow if {
	http_request.method == "POST"
	http_request.path == "/message/send"
	is_authenticated
	can_send_messages
}

allow if {
	http_request.method == "POST"
	http_request.path == "/peers"
	is_authenticated
	can_manage_peers
}

allow if {
	http_request.method == "DELETE"
	http_request.path == "/peers"
	is_authenticated
	can_manage_peers
}

allow if {
	http_request.method == "POST"
	endswith(http_request.path, "/cancel")
	startswith(http_request.path, "/tasks/")
	is_authenticated
}

allow if {
	http_request.method == "OPTIONS"
	http_request.path == "/agents"
}

allow if {
	http_request.method == "POST"
	http_request.path == "/agents"
	is_authenticated
	can_manage_agents
}

allow if {
	http_request.method == "GET"
	http_request.path == "/agents"
	is_authenticated
}

allow if {
	http_request.method == "HEAD"
	startswith(http_request.path, "/agents/")
	is_authenticated
}

allow if {
	http_request.method == "PATCH"
	startswith(http_request.path, "/agents/")
	is_authenticated
	can_manage_agents
}

allow if {
	http_request.method == "DELETE"
	startswith(http_request.path, "/agents/")
	is_authenticated
	can_manage_agents
}

allow if {
	http_request.method == "GET"
	endswith(http_request.path, "/status")
	startswith(http_request.path, "/agents/")
	is_authenticated
}

allow if {
	http_request.method == "GET"
	http_request.path == "/v1/mcp/tools"
	is_authenticated
}

allow if {
	http_request.method == "POST"
	http_request.path == "/v1/mcp/tools/call"
	is_authenticated
	can_call_mcp
}

allow if {
	http_request.method == "POST"
	http_request.path == "/v1/mcp/attach"
	is_authenticated
	is_admin
}

allow if {
	http_request.method == "POST"
	http_request.path == "/v1/mcp/detach"
	is_authenticated
	is_admin
}

is_authenticated if {
	header_value("authorization") != ""
}

is_authenticated if {
	header_value("x-user-id") != ""
}

# Comma-separated realm roles from Envoy (JWT → x-symagios-roles). Fail closed:
# missing/empty header means has_role never matches.
role_header := header_value("x-symagios-roles")

roles := {trim(raw, " \t") |
	some raw in split(role_header, ",")
	trim(raw, " \t") != ""
}

has_role(want) if {
	want in roles
}

can_send_messages if { has_role("operator") }
can_send_messages if { has_role("agent-runner") }
can_send_messages if { has_role("admin") }

can_manage_peers if { has_role("admin") }
can_manage_peers if { has_role("operator") }

can_manage_agents if { has_role("admin") }
can_manage_agents if { has_role("operator") }

can_call_mcp if { has_role("agent-runner") }
can_call_mcp if { has_role("operator") }
can_call_mcp if { has_role("admin") }

is_admin if { has_role("admin") }

header_value(name) := value if {
	value := http_request.headers[name]
}

header_value(name) := value if {
	value := http_request.headers[lower(name)]
}
