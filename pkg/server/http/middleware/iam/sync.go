package iam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	httputil2 "github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/redis/go-redis/v9"
	"github.com/worldline-go/klient"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

var (
	DefaultSyncTimeout = 10 * time.Minute
	DefaultTrigger     = 4 * time.Second
)

type PubSubModel struct {
	Type string `json:"type"`

	Version uint64 `json:"version,omitempty"`
	ID      string `json:"id,omitempty"`
}

type SyncAPI struct {
	Backup  string
	Version string
	Trigger string

	WriteAPI      *url.URL
	CurrentPrefix string
}

type SyncDB interface {
	Version() uint64
	Restore(r io.Reader) error
}

type SyncConfig struct {
	WriteAPI    string
	PrefixPath  string
	DB          SyncDB
	Redis       redis.UniversalClient
	PubSubTopic string
}

type Sync struct {
	db SyncDB
	// syncAPI is not nil, it means this service is read-only
	syncAPI     *SyncAPI
	client      *klient.Client
	redis       redis.UniversalClient
	topic       string
	shared      Shared
	lastVersion LastVersion

	m sync.Mutex
}

type LastVersion struct {
	Version uint64

	m sync.RWMutex
}

func (l *LastVersion) Set(version uint64) {
	l.m.Lock()
	defer l.m.Unlock()

	l.Version = version
}

func (l *LastVersion) Get() uint64 {
	l.m.RLock()
	defer l.m.RUnlock()

	return l.Version
}

type Shared struct {
	V map[string]VersionTime

	m sync.Mutex
}

type VersionTime struct {
	Version uint64
	Time    time.Time
}

func (s *Shared) Set(key string, version uint64) {
	s.m.Lock()
	defer s.m.Unlock()

	if s.V == nil {
		s.V = make(map[string]VersionTime)
	}

	s.V[key] = VersionTime{
		Version: version,
		Time:    time.Now(),
	}
}

func (s *Shared) Wait(version uint64) {
	vMax := time.After(30 * time.Second)
	for {
		select {
		case <-vMax:
			slog.Error("timeout waiting for version", "version", version)
			return
		default:
			s.m.Lock()
			ready := true
			for key, v := range s.V {
				if time.Since(v.Time) > 10*time.Second {
					delete(s.V, key)

					continue
				}

				if v.Version < version {
					ready = false

					break
				}
			}
			s.m.Unlock()

			if ready {
				return
			}

			time.Sleep(1 * time.Second)
		}
	}
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

		syncAPI = &SyncAPI{
			Backup:  writeAPI + "/v1/backup",
			Version: writeAPI + "/v1/version",
			Trigger: writeAPI + "/v1/trigger",

			WriteAPI:      writeAPIURL,
			CurrentPrefix: cfg.PrefixPath,
		}
	}

	return &Sync{
		db:      cfg.DB,
		syncAPI: syncAPI,
		client:  client,
		redis:   cfg.Redis,
		topic:   cfg.PubSubTopic,
	}, nil
}

// ////////////////////////////////////////////////////////////////////////////
// for READ-ONLY service

func (s *Sync) SyncStart(ctx context.Context) (func() error, error) {
	idGen := ulid.Make().String()

	pubsub := s.redis.Subscribe(ctx, s.topic)
	ch := pubsub.Channel()

	if s.syncAPI == nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case msg := <-ch:
					if msg == nil {
						slog.Error("pubsub message is nil")

						continue
					}

					var data PubSubModel
					if err := json.Unmarshal([]byte(msg.Payload), &data); err != nil {
						slog.Error("pubsub unmarshal", "error", err.Error())

						continue
					}

					if data.Type != "id" {
						continue
					}

					if data.ID == "" {
						continue
					}

					s.shared.Set(data.ID, data.Version)

					if s.lastVersion.Get() > data.Version {
						s.triggerPush(ctx)
					}
				}
			}
		}()

		return pubsub.Close, nil
	}

	go func() {
		idData, err := json.Marshal(PubSubModel{
			ID:      idGen,
			Type:    "id",
			Version: s.db.Version(),
		})
		if err != nil {
			slog.Error("marshal id", "error", err)
		}

		if err := s.redis.Publish(ctx, s.topic, idData).Err(); err != nil {
			slog.Error("pubsub publish id", "error", err)
			return
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(DefaultTrigger):
				idData, err := json.Marshal(PubSubModel{
					ID:      idGen,
					Type:    "id",
					Version: s.db.Version(),
				})
				if err != nil {
					slog.Error("marshal id", "error", err)

					continue
				}

				if err := s.redis.Publish(ctx, s.topic, idData).Err(); err != nil {
					slog.Error("pubsub publish id", "error", err)
				}
			}
		}
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-ch:
				if msg == nil {
					slog.Error("pubsub message is nil")

					continue
				}

				var data PubSubModel
				if err := json.Unmarshal([]byte(msg.Payload), &data); err != nil {
					slog.Error("pubsub unmarshal", "error", err.Error())

					continue
				}

				if data.Type != "version" {
					continue
				}

				if data.Version == 0 {
					slog.Error("pubsub version is 0")

					continue
				}

				if err := s.sync(ctx, data.Version); err != nil {
					slog.Error("failed to sync", "error", err.Error())

					continue
				}

				idData, err := json.Marshal(PubSubModel{
					ID:      idGen,
					Type:    "id",
					Version: s.db.Version(),
				})
				if err != nil {
					slog.Error("marshal id", "error", err)

					continue
				}

				if err := s.redis.Publish(ctx, s.topic, idData).Err(); err != nil {
					slog.Error("pubsub publish id", "error", err)

					continue
				}
			}
		}
	}()

	return pubsub.Close, nil
}

func (s *Sync) Sync(ctx context.Context, targetVersion uint64) error {
	if s.syncAPI == nil {
		s.lastVersion.Set(s.db.Version())
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
	query.Add("deleted", "true")
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

func (s *Sync) Trigger(ctx context.Context) {
	if s.syncAPI != nil {
		return
	}

	s.trigger(ctx)
}

func (s *Sync) UpdateLastVersion() {
	s.m.Lock()
	defer s.m.Unlock()

	s.lastVersion.Set(s.db.Version())
}

func (s *Sync) trigger(ctx context.Context) {
	s.m.Lock()
	defer s.m.Unlock()

	s.lastVersion.Set(s.db.Version())

	if err := s.triggerPush(ctx); err != nil {
		slog.Error("failed to trigger push", "error", err.Error())

		return
	}

	s.shared.Wait(s.lastVersion.Get())
}

func (s *Sync) triggerPush(ctx context.Context) error {
	v, err := json.Marshal(PubSubModel{
		Version: s.lastVersion.Get(),
		Type:    "version",
	})
	if err != nil {
		return fmt.Errorf("failed to marshal: %w", err)
	}

	if err := s.redis.Publish(ctx, s.topic, v).Err(); err != nil {
		return fmt.Errorf("failed to publish: %w", err)
	}

	return nil
}
