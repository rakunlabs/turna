package args

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rakunlabs/chu"
	"github.com/rakunlabs/chu/loader"

	"github.com/rakunlabs/turna/internal/config"
	"github.com/rakunlabs/turna/pkg/server"
	"github.com/rakunlabs/turna/pkg/server/reloader"
)

// reloadRunMu runs one configuration reload at a time.
var reloadRunMu sync.Mutex

// appliedServer is the fingerprint of the last applied server
// configuration, guarded by reloadMu.
var appliedServer string

// serverFingerprint returns a stable representation of s, empty when it
// cannot be built.
func serverFingerprint(s *server.Server) string {
	v, err := json.Marshal(s)
	if err != nil {
		return ""
	}

	return string(v)
}

// rememberServer stores the fingerprint of the configuration started with.
func rememberServer(s *server.Server) {
	reloadMu.Lock()
	defer reloadMu.Unlock()

	appliedServer = serverFingerprint(s)
}

// reloadConfig loads the configuration again and applies its server
// section. On error the running configuration is kept.
func reloadConfig(ctx context.Context, reason string) error {
	// a slow source must not let an older load be applied after a newer one
	reloadRunMu.Lock()
	defer reloadRunMu.Unlock()

	changed, err := applyConfig(ctx)
	if err != nil {
		slog.Error("config reload failed, previous configuration is kept", "reason", reason, "err", err.Error())

		return err
	}

	if !changed {
		slog.Debug("server configuration is unchanged", "reason", reason)

		return nil
	}

	slog.Info("server configuration reloaded", "reason", reason)

	return nil
}

// applyConfig reports whether the server configuration changed.
func applyConfig(ctx context.Context) (bool, error) {
	next := config.NewApp()
	if err := chu.Load(ctx, AppName, &next, chu.WithReport(reportSource)); err != nil {
		return false, fmt.Errorf("cannot load configuration: %w", err)
	}

	reloadMu.Lock()
	defer reloadMu.Unlock()

	srv := next.Server
	var value any

	if key := srv.LoadValue; key != "" {
		// the server comes from loaded data, keep following it
		value = lastData[key]
		if value == nil {
			return false, fmt.Errorf("load_value %s is missing", key)
		}

		srv = server.Server{LoadValue: key}
		if err := config.Decode(value, &srv); err != nil {
			return false, fmt.Errorf("cannot decode load_value %s: %w", key, err)
		}
	}

	if server.GlobalManager == nil {
		return false, errors.New("server is not running")
	}

	// compare decoded configurations: the running layer may have been
	// changed while building it
	fp := serverFingerprint(&srv)
	if fp != "" && fp == appliedServer {
		return false, nil
	}

	if err := server.GlobalManager.SetLayer(server.LayerStatic, &srv); err != nil {
		return false, err
	}

	appliedServer = fp

	config.Application.Server.LoadValue = next.Server.LoadValue
	lastLoadValue = value

	return true, nil
}

// startReload reloads on SIGHUP and on configuration file changes, as
// configured, until ctx is done.
func startReload(ctx context.Context, wg *sync.WaitGroup, cfg config.Reload) error {
	if cfg.Interval < 0 {
		return errors.New("reload interval must not be negative")
	}

	debounce := cfg.Debounce
	if debounce <= 0 {
		debounce = 500 * time.Millisecond
	}

	trigger := make(chan string, 1)
	send := func(reason string) {
		select {
		case trigger <- reason:
		default:
		}
	}

	if cfg.Signal == nil || *cfg.Signal {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGHUP)

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer signal.Stop(sig)

			for {
				select {
				case <-ctx.Done():
					return
				case <-sig:
					send("signal")
				}
			}
		}()
	}

	if cfg.Watch {
		file := loadedFile()
		if file == "" {
			return fmt.Errorf("reload watch: no configuration file was loaded")
		}

		if err := watchFile(ctx, wg, file, debounce, send); err != nil {
			return fmt.Errorf("reload watch: %w", err)
		}

		slog.Info("watching configuration file", "file", file)
	}

	if cfg.Interval > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ticker := time.NewTicker(cfg.Interval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					send("interval")
				}
			}
		}()
	}

	reloader.Set(reloadConfig)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer reloader.Set(nil)

		for {
			select {
			case <-ctx.Done():
				return
			case reason := <-trigger:
				_ = reloadConfig(ctx, reason)
			}
		}
	}()

	return nil
}

// watchFile watches the directory of file, so editors replacing the file
// and Kubernetes ConfigMap symlink swaps are seen.
func watchFile(ctx context.Context, wg *sync.WaitGroup, file string, debounce time.Duration, send func(string)) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	abs, err := filepath.Abs(file)
	if err != nil {
		_ = w.Close()

		return err
	}

	dir := filepath.Dir(abs)
	if err := w.Add(dir); err != nil {
		_ = w.Close()

		return err
	}

	type state struct {
		real string
		mod  time.Time
		size int64
	}

	current := func() (state, bool) {
		fi, err := os.Stat(abs)
		if err != nil {
			return state{}, false
		}

		real, _ := filepath.EvalSymlinks(abs)

		return state{real: real, mod: fi.ModTime(), size: fi.Size()}, true
	}

	last, _ := current()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer w.Close()

		var timer <-chan time.Time

		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}

				if ev.Has(fsnotify.Chmod) && !ev.Has(fsnotify.Write) {
					continue
				}

				name := filepath.Clean(ev.Name)
				// ConfigMaps update ..data, the file is a symlink into it
				if name == abs || strings.HasPrefix(filepath.Base(name), "..") {
					timer = time.After(debounce)
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}

				slog.Warn("configuration watch error", "err", err.Error())
			case <-timer:
				timer = nil

				// a missing file is a write in progress; unchanged files
				// come from events of other files in the directory
				next, ok := current()
				if !ok || (next.real == last.real && next.mod.Equal(last.mod) && next.size == last.size) {
					continue
				}

				last = next
				send("file")
			}
		}
	}()

	return nil
}

var (
	loadedFileMu sync.Mutex
	loadedPath   string
)

// reportSource remembers the configuration file chu loaded.
func reportSource(s loader.Source) {
	if s.Loader != loader.NameFile {
		return
	}

	loadedFileMu.Lock()
	defer loadedFileMu.Unlock()

	loadedPath = s.Key
}

// loadedFile returns the configuration file of the last load, empty when
// no file was loaded.
func loadedFile() string {
	loadedFileMu.Lock()
	defer loadedFileMu.Unlock()

	return loadedPath
}
