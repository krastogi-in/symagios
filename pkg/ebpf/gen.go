package ebpf

// Requires: clang, llvm-strip, and libbpf headers (bpf/bpf_helpers.h etc.)
// available at build time. On RHEL/Fedora: dnf install clang llvm libbpf-devel.
// vmlinux.h can be generated once via:
//   bpftool btf dump file /sys/kernel/btf/vmlinux format c > headers/vmlinux.h

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -target amd64,arm64 -cc clang -cflags "-I./headers -mllvm -bpf-stack-size=1024" PreHook prehook.c
