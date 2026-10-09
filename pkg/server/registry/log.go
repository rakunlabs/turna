package registry

import "log/slog"

func slogError(msg string, err error) {
	slog.Error(msg, "err", err.Error())
}
