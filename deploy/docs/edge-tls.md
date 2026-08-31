# Edge TLS (the public door)

We do not put a load balancer in front of Symagios for the local compose setup. Envoy *is* the edge. That means the only listener you should hit from outside is HTTPS, and Envoy is the process that terminates TLS.

If we left a plaintext listener on the host, people would send Bearer tokens in the clear up to the trust boundary. That is exactly what we are trying not to do for a secured orchestrator. So host port `8081`-style plaintext ingress was removed. The published ingress port is **8443 → container 10000**.

Certs for that listener come from Vault PKI (`make mtls-issue`) and land under `deploy/certs/envoy-ingress/`. The listener uses a normal server certificate. We do **not** require a client certificate from browsers or the CLI at the edge — mutual TLS is for container-to-container hops behind Envoy, not for every human client.

Envoy’s admin interface binds to `127.0.0.1:9901` *inside* the container and is not published. Install scripts check readiness with `./scripts/compose.sh exec` and a `/dev/tcp` probe, not with a curl from the host to an open admin port.

The same `deploy/envoy/envoy.yaml` file also configures how Envoy talks *outbound* to agent-gateway over mTLS. That is a different hop (UpstreamTlsContext), not a second Envoy binary. More on that in [mtls.md](mtls.md) and the overview in [auth-stack.md](auth-stack.md).
