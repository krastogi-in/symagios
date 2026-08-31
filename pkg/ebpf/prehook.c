//go:build ignore

#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define EPERM 1

#define MAX_PATH_LEN 256

char __license[] SEC("license") = "GPL";

/* Permission bitmask values. A worker's cgroup is authorized for a
 * combination of these before it's allowed to do the corresponding action. */
#define PERM_EXEC    (1 << 0)  /* start a new process (execve/execveat) */
#define PERM_FILE    (1 << 1)  /* open a file */
#define PERM_NET     (1 << 2)  /* open an outbound network connection */

/* Policy map: one entry per WORKER cgroup, lives for the worker's lifetime.
 * Value is a bitmask of what that worker is currently allowed to do -
 * updated by Go before each task, and narrowed again right after. */
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024); /* one entry per concurrent worker, not per task */
    __type(key, __u64);        /* cgroup id */
    __type(value, __u8);       /* permission bitmask */
} policy_map SEC(".maps");

struct denied_path_key {
    __u32 prefixlen;
    char data[MAX_PATH_LEN];
};

struct {
    __uint(type, BPF_MAP_TYPE_LPM_TRIE);
    __uint(max_entries, 1024);
    __uint(map_flags, BPF_F_NO_PREALLOC);
    __type(key, struct denied_path_key);
    __type(value, __u8);
} denied_paths SEC(".maps");

/* Ring buffer: every check (allowed or denied) is pushed here so userspace
 * can build an audit trail / feed the agent's memory & anomaly detection. */
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} audit_events SEC(".maps");

struct audit_event {
    __u64 cgroup_id;
    __u32 pid;
    __u8  action;   /* PERM_EXEC / PERM_FILE / PERM_NET - which check fired */
    __u8  allowed;  /* 1 = permitted, 0 = denied */
    char  comm[16]; /* process name, for quick human-readable logs */
};

static __always_inline __u8 lookup_perm(__u64 cgid) {
    __u8 *v = bpf_map_lookup_elem(&policy_map, &cgid);
    return v ? *v : 0;
}

/* LPM trie lookup: prefixlen is the path length in bits (LPM trie convention). */
static __always_inline __u8 lookup_denied_path(const char *path, __u32 path_len) {
    struct denied_path_key key = {};

    if (!path || path_len == 0)
        return 0;

    if (path_len >= MAX_PATH_LEN)
        path_len = MAX_PATH_LEN - 1;

    key.prefixlen = path_len * 8;
    bpf_probe_read_kernel(key.data, path_len, path);

    __u8 *v = bpf_map_lookup_elem(&denied_paths, &key);
    return v ? *v : 0;
}

static __always_inline void emit_audit(__u64 cgid, __u8 action, __u8 allowed) {
    struct audit_event *ev = bpf_ringbuf_reserve(&audit_events, sizeof(*ev), 0);
    if (!ev)
        return; /* buffer full - drop the audit record, never block on it */
    ev->cgroup_id = cgid;
    ev->pid = bpf_get_current_pid_tgid() >> 32;
    ev->action = action;
    ev->allowed = allowed;
    bpf_get_current_comm(&ev->comm, sizeof(ev->comm));
    bpf_ringbuf_submit(ev, 0);
}

/* Fires before a new program is allowed to start (covers exec.Command(...)
 * and anything it in turn shells out to). */
SEC("lsm/bprm_check_security")
int BPF_PROG(check_exec, struct linux_binprm *bprm) {
    __u64 cgid = bpf_get_current_cgroup_id();
    __u8 perm = lookup_perm(cgid);
    __u8 allowed = (perm & PERM_EXEC) ? 1 : 0;

    emit_audit(cgid, PERM_EXEC, allowed);
    return allowed ? 0 : -EPERM;
}

/* Fires before a file is opened (covers os.ReadFile, os.Open, etc. called
 * directly in Go, with no subprocess involved). */
SEC("lsm/file_open")
int BPF_PROG(check_file_open, struct file *file) {
    __u64 cgid = bpf_get_current_cgroup_id();
    char buf[MAX_PATH_LEN];
    long path_len = bpf_d_path(&file->f_path, buf, sizeof(buf));

    if (path_len > 0) {
        __u8 denied = lookup_denied_path(buf, (__u32)path_len);
        if (denied) {
            emit_audit(cgid, PERM_FILE, 0);
            return -EPERM;
        }
    }

    __u8 perm = lookup_perm(cgid);
    __u8 allowed = (perm & PERM_FILE) ? 1 : 0;

    emit_audit(cgid, PERM_FILE, allowed);
    return allowed ? 0 : -EPERM;
}

/* Fires before an outbound connection is made (covers http.Get and other
 * direct Go network calls, no subprocess involved). */
SEC("lsm/socket_connect")
int BPF_PROG(check_socket_connect, struct socket *sock, struct sockaddr *address, int addrlen) {
    __u64 cgid = bpf_get_current_cgroup_id();
    __u8 perm = lookup_perm(cgid);
    __u8 allowed = (perm & PERM_NET) ? 1 : 0;

    emit_audit(cgid, PERM_NET, allowed);
    return allowed ? 0 : -EPERM;
}
