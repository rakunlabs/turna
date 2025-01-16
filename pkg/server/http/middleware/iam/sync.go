package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	httputil2 "github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/middleware/iam/data"
	"github.com/worldline-go/klient"
)

var (
	DefaultIntervalWriteAPI = 10 * time.Second
	DefaultTriggerTTL       = 60 * time.Second

	DefaultTriggerRequestTimeout = 10 * time.Second
	DefaultSyncTimeout           = 10 * time.Minute

	DefaultSyncInterval = 10 * time.Minute
)

type Trigger struct {
	Path   string `json:"path"`
	Schema string `json:"schema"` // default is http
	Host   string `json:"host"`   // default is caller host
	Port   string `json:"port"`   // default is caller port
}

type SyncAPI struct {
	Backup  string
	Version string
	Trigger string

	WriteAPI      *url.URL
	CurrentPrefix string
	TriggerData   []byte
}

type SyncDB interface {
	Version() uint64
	Restore(r io.Reader) error
}

type SyncConfig struct {
	WriteAPI          string
	PrefixPath        string
	DB                SyncDB
	TriggerBackground bool

	SyncSchema string
	SyncHost   string
	SyncPort   string
}

type Sync struct {
	db                SyncDB
	syncAPI           *SyncAPI
	triggerAPIs       map[string]TriggerData
	triggerBackground bool

	m      sync.Mutex
	client *klient.Client
}

type TriggerData struct {
	Time time.Time
}

func NewSync(cfg SyncConfig) (*Sync, error) {
	client, err := klient.NewPlain()
	if err != nil {
		return nil, err
	}

	var syncAPI *SyncAPI
	if cfg.WriteAPI != "" {
		writeAPI := strings.TrimRight(cfg.WriteAPI, "/")

		writeAPIURL, err := url.Parse(writeAPI)
		if err != nil {
			return nil, fmt.Errorf("failed to parse write api: %w", err)
		}

		trigger := Trigger{
			Path:   cfg.PrefixPath + "/v1/sync",
			Schema: cfg.SyncSchema,
			Host:   cfg.SyncHost,
			Port:   cfg.SyncPort,
		}

		triggerBytes, err := json.Marshal(trigger)
		if err != nil {
			return nil, err
		}

		syncAPI = &SyncAPI{
			Backup:  writeAPI + "/v1/backup",
			Version: writeAPI + "/v1/version",
			Trigger: writeAPI + "/v1/trigger",

			TriggerData:   triggerBytes,
			WriteAPI:      writeAPIURL,
			CurrentPrefix: cfg.PrefixPath,
		}
	}

	return &Sync{
		db:                cfg.DB,
		syncAPI:           syncAPI,
		triggerAPIs:       make(map[string]TriggerData),
		triggerBackground: cfg.TriggerBackground,
		client:            client,
	}, nil
}

// ////////////////////////////////////////////////////////////////////////////
// for READ-ONLY service

func (s *Sync) SyncStart(ctx context.Context) {
	if s.syncAPI == nil {
		return
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(DefaultSyncInterval):
				if err := s.Sync(ctx, 0); err != nil {
					slog.Error("failed to sync", slog.String("error", err.Error()))
				}
			}
		}
	}()
}

func (s *Sync) Sync(ctx context.Context, targetVersion uint64) error {
	if s.syncAPI == nil {
		return nil
	}

	s.m.Lock()
	defer s.m.Unlock()

	if err := s.sync(ctx, targetVersion); err != nil {
		return fmt.Errorf("failed to sync: %w", err)
	}

	return nil
}

func (s *Sync) sync(ctx context.Context, targetVersion uint64) error {
	if targetVersion == 0 {
		var err error
		targetVersion, err = s.getVersion(ctx)
		if err != nil {
			return fmt.Errorf("failed to get version: %w", err)
		}
	}

	// check current version
	currentVersion := s.db.Version()
	if targetVersion <= currentVersion {
		return nil
	}

	// get backup data and restore it
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.syncAPI.Backup, nil)
	if err != nil {
		return fmt.Errorf("failed to create request for backup: %w", err)
	}

	query := req.URL.Query()
	query.Add("since", strconv.FormatUint(currentVersion, 10))
	req.URL.RawQuery = query.Encode()

	if err := s.client.Do(req, func(r *http.Response) error {
		if err := klient.UnexpectedResponse(r); err != nil {
			return err
		}

		if r.Body == nil {
			return nil
		}

		if err := s.db.Restore(r.Body); err != nil {
			return fmt.Errorf("failed to restore: %w", err)
		}

		slog.Info("updated database", slog.Uint64("version", targetVersion), slog.Uint64("current", currentVersion))

		return nil
	}); err != nil {
		return fmt.Errorf("failed to get backup: %w", err)
	}

	return nil
}

func (s *Sync) getVersion(ctx context.Context) (uint64, error) {
	ctx, ctxCancel := context.WithTimeout(ctx, DefaultSyncTimeout)
	defer ctxCancel()

	// first check version is upper than current
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.syncAPI.Version, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request for version: %w", err)
	}

	responseVersion := data.ResponseVersion{}
	if err := s.client.Do(req, klient.ResponseFuncJSON(&responseVersion)); err != nil {
		return 0, fmt.Errorf("failed to get version: %w", err)
	}

	return responseVersion.Version, nil
}

func (s *Sync) SyncTTL(ctx context.Context) {
	if s.syncAPI == nil {
		return
	}

	if err := s.informWrite(ctx); err != nil {
		slog.Error("failed to trigger write", slog.String("error", err.Error()))
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(DefaultIntervalWriteAPI):
				if err := s.informWrite(ctx); err != nil {
					slog.Error("failed to trigger write", slog.String("error", err.Error()))
				}
			}
		}
	}()
}

func (s *Sync) informWrite(ctx context.Context) error {
	ctx, ctxCancel := context.WithTimeout(ctx, DefaultTriggerRequestTimeout)
	defer ctxCancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.syncAPI.Trigger, bytes.NewReader(s.syncAPI.TriggerData))
	if err != nil {
		return err
	}

	if err := s.client.Do(req, klient.UnexpectedResponse); err != nil {
		return err
	}

	return nil
}

func (s *Sync) Redirect(w http.ResponseWriter, r *http.Request) bool {
	if s.syncAPI == nil {
		return false
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			httputil2.RewriteRequestURLTarget(r.Out, s.syncAPI.WriteAPI)
			r.Out.URL.Path = s.syncAPI.WriteAPI.Path + strings.TrimPrefix(r.In.URL.Path, s.syncAPI.CurrentPrefix)
			r.Out.URL.RawPath = r.Out.URL.Path
		},
	}

	proxy.ServeHTTP(w, r)

	return true
}

// ////////////////////////////////////////////////////////////////////////////
// for WRITE service

func (s *Sync) AddSync(ctx context.Context, trigger Trigger) {
	if s.syncAPI != nil {
		return
	}

	s.m.Lock()
	defer s.m.Unlock()

	request := url.URL{}
	request.Scheme = trigger.Schema
	request.Host = net.JoinHostPort(trigger.Host, trigger.Port)
	request.Path = trigger.Path

	path := request.String()

	triggerPath := false

	if _, ok := s.triggerAPIs[path]; !ok {
		slog.Info("add sync", slog.String("path", path))
		// trigger sync
		triggerPath = true
	}

	s.triggerAPIs[path] = TriggerData{
		Time: time.Now(),
	}

	if triggerPath {
		go func() {
			s.call(ctx, path, strconv.FormatUint(s.db.Version(), 10))
		}()
	}
}

func (s *Sync) Trigger(ctx context.Context) {
	if s.syncAPI != nil {
		return
	}

	if s.triggerBackground {
		go func() {
			s.trigger(ctx)
		}()

		return
	}

	s.trigger(ctx)
}

func (s *Sync) trigger(ctx context.Context) {
	s.m.Lock()
	defer s.m.Unlock()

	version := strconv.FormatUint(s.db.Version(), 10)

	deleteList := make([]string, 0, len(s.triggerAPIs))

	for path, data := range s.triggerAPIs {
		if time.Since(data.Time) > DefaultTriggerTTL {
			deleteList = append(deleteList, path)

			continue
		}

		s.call(ctx, path, version)
	}

	for _, path := range deleteList {
		delete(s.triggerAPIs, path)
		slog.Info("delete sync", slog.String("path", path))
	}
}

func (s *Sync) call(ctx context.Context, path string, version string) {
	ctx, ctxCancel := context.WithTimeout(ctx, DefaultSyncTimeout)
	defer ctxCancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, nil)
	if err != nil {
		slog.Error("failed to create request", slog.String("path", path), slog.String("error", err.Error()))
		return
	}

	req.Header.Set("X-Sync-Version", version)

	if err := s.client.Do(req, klient.UnexpectedResponse); err != nil {
		slog.Error("failed to trigger sync", slog.String("path", path), slog.String("error", err.Error()))
	}
}
