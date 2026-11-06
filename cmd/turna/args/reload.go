package args

import (
	"log/slog"
	"reflect"
	"sync"

	"github.com/rakunlabs/turna/internal/config"
	"github.com/rakunlabs/turna/pkg/server"
)

var (
	reloadMu      sync.Mutex
	lastLoadValue any
	// lastData is the latest loaded data, used by file and signal reloads
	lastData map[string]any
)

// reloadServer applies the server configuration of server.load_value when
// dynamic data changes after the server started.
func reloadServer(data map[string]any) {
	reloadMu.Lock()
	defer reloadMu.Unlock()

	lastData = data

	key := config.Application.Server.LoadValue
	if key == "" || server.GlobalManager == nil {
		return
	}

	value := data[key]
	if reflect.DeepEqual(value, lastLoadValue) {
		return
	}

	next := server.Server{LoadValue: key}
	if err := config.Decode(value, &next); err != nil {
		slog.Error("server reload: cannot decode load_value", "key", key, "err", err.Error())

		return
	}

	fp := serverFingerprint(&next)

	if err := server.GlobalManager.SetLayer(server.LayerStatic, &next); err != nil {
		slog.Error("server reload failed, previous configuration is kept", "err", err.Error())

		return
	}

	lastLoadValue = value
	appliedServer = fp

	slog.Info("server configuration reloaded", "key", key)
}

// rememberLoadValue stores the value used at startup so the first dynamic
// callback does not re-apply an unchanged configuration.
func rememberLoadValue(data map[string]any) {
	reloadMu.Lock()
	defer reloadMu.Unlock()

	lastData = data
	lastLoadValue = data[config.Application.Server.LoadValue]
}
