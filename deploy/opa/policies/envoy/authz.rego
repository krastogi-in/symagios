package envoy.authz

import input.attributes.request.http as http_request
import data.envoy.egress
import data.envoy.ingress
import future.keywords.if

default allow := false

allow if {
	traffic_class == "egress"
	egress.allow
}

allow if {
	traffic_class != "egress"
	ingress.allow
}

traffic_class := header_value("x-symagios-traffic")

header_value(name) := value if {
	value := http_request.headers[name]
}

header_value(name) := value if {
	value := http_request.headers[lower(name)]
}
