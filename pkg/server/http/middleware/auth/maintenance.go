package auth

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

const (
	flowCleanupInterval = time.Hour
	flowCleanupBatch    = 1000
)

func (m *Auth) purgeExpiredFlowCodes(ctx context.Context) (int64, error) {
	var total int64
	for {
		deleted, err := m.store.PruneExpiredFlowCodes(ctx, flowCleanupBatch)
		if err != nil {
			return total, err
		}
		total += deleted
		if deleted < flowCleanupBatch {
			return total, nil
		}
	}
}

func (m *Auth) watchFlowCleanup(ctx context.Context) {
	run := func() {
		deleted, err := m.purgeExpiredFlowCodes(ctx)
		if err != nil {
			slog.Error("auth flow cleanup failed", "error", err.Error())
			return
		}
		if deleted > 0 {
			slog.Info("auth flow cleanup completed", "deleted", deleted)
		}
	}

	run()
	ticker := time.NewTicker(flowCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func (m *Auth) PurgeFlowCodesAPI(w http.ResponseWriter, r *http.Request) {
	deleted, err := m.purgeExpiredFlowCodes(r.Context())
	if err != nil {
		httputil.HandleError(w, httputil.NewError("cannot purge expired auth records", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, Response[map[string]any]{
		Payload: map[string]any{"deleted": deleted},
	})
}
