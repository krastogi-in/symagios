# Process guards (Landlock + seccomp)

Landlock and seccomp are not “script-only.” Agents, skills, and subagents can run or spawn work that should not see the whole machine. Scripts are just the most obvious case.

Stack for sandboxed work:

1. **eBPF LSM** on the worker cgroup — coarse exec/net/file classes.
2. **Landlock** — only the path trees you allow (workspace, maybe interpreters).
3. **seccomp** — kill dangerous syscalls (`mount`, `umount2`, `reboot`, …).

Compose (`engine` service) unmasks `/sys/fs/cgroup` and sets `label=disable` so rootless Podman on Fedora can create per-worker cgroups. Loading the eBPF LSM still requires a **rootful** runtime (`kernel.unprivileged_bpf_disabled`); under rootless the engine logs `ebpf LSM unavailable` and continues with cgroups only.

`hooks.NeededForAction` is true for **agent**, **skill**, **subagent**, and **script**. MCP / WEB_SEARCH / HITL stay on eBPF-only (net grant for MCP and search) for now.

Landlock and seccomp stick for the life of the process. Call `hooks.ApplyConfiguredGuards()` in the child or OS thread that actually runs the sandboxed work — not on a shared worker that will later handle a different action.

## Config

```bash
SYMAGIOS_GUARD_LANDLOCK=true
SYMAGIOS_GUARD_SECCOMP=true
SYMAGIOS_GUARD_ALLOW_PATHS=/tmp/symagios-workspace,/usr/bin
```

```bash
symagios engine --guard-landlock --guard-seccomp
```

Default allow path is `/tmp/symagios-workspace`. Widen it when an agent/skill/script needs more — do not set it to `/`.
