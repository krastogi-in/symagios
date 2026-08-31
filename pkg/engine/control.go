package engine

import (
	"context"
	"fmt"
	"runtime"

	"symagios/ebpf/cgroup"
	"symagios/encrypt"
	"symagios/engine/hooks"
	applog "symagios/log"
	"symagios/storage/sqlite"
	"symagios/utils"
)

var (
	store        *sqlite.Client
	storeEncrypt *encrypt.EncryptKey
	engineLogger = applog.Default().With("component", "engine")
)

func startControl(ctx context.Context, dbClient *sqlite.Client, encryptKey *encrypt.EncryptKey, numWorkers int) {
	store = dbClient
	storeEncrypt = encryptKey

	if err := hooks.InitLSM(); err != nil {
		engineLogger.Error("ebpf LSM unavailable, workers will run without syscall policy", "error", err)
	}

	engineLogger.Info("starting control", "workers", numWorkers)

	buffer := utils.NewBuffer(100)
	for i := 0; i < numWorkers; i++ {
		newWorker(ctx, i, buffer, encryptKey)
	}
	startScheduler(ctx, buffer)
}

func newWorker(ctx context.Context, workerID int, buffer *utils.Buffer, encryptKey *encrypt.EncryptKey) {
	logger := engineLogger.With("worker_id", workerID)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		var cgroupID uint64
		cg, err := cgroup.Create(ctx, fmt.Sprintf("symagios-worker-%d", workerID))
		if err != nil {
			// Rootless Podman/Docker often mounts /sys/fs/cgroup read-only; LSM policy
			// also needs CAP_SYS_RESOURCE. Continue without isolation so the POC still runs.
			logger.Error("failed to create cgroup, worker continuing without isolation", "error", err)
		} else {
			defer func() {
				if err := cg.Teardown(ctx); err != nil {
					logger.Error("failed to teardown cgroup", "error", err)
				}
			}()
			cgroupID = cg.ID()
			logger.Info("worker cgroup attached", "cgroup_id", cgroupID)
		}

		for {
			raw := buffer.Get()
			item, ok := raw.(*Item)
			if !ok || item == nil {
				continue
			}
			if !item.CanExecute() {
				logger.Debug(
					"skipping item that cannot execute",
					applog.ActionFields(item.sessionID, item.actionName, item.actionType)...,
				)
				continue
			}

			execute(ctx, encryptKey, item, cgroupID)
			logger.Info(
				"item executed",
				applog.ActionFields(item.sessionID, item.actionName, item.actionType)...,
			)
		}
	}()
}
