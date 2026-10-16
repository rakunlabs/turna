package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rakunlabs/turna/pkg/server"
)

// Layer is the server configuration layer owned by the provider.
const Layer = "docker"

// Applier receives translated configurations.
type Applier interface {
	SetLayer(name string, s *server.Server) error
	// CheckLayer reports whether SetLayer would succeed, without applying.
	CheckLayer(name string, s *server.Server) error
	// Current returns the merged configuration of all layers.
	Current() *server.Server
}

type Provider struct {
	cfg     Config
	applier Applier
	client  *client

	trigger chan struct{}

	mu       sync.Mutex
	last     *server.Server
	applied  bool
	reported map[string]string

	// failed is the last configuration that could only be applied in part,
	// with the errors of the dropped groups and the other layers it was
	// tried with
	failed       *server.Server
	failedErrs   map[string]error
	failedOthers *server.Server
}

func New(cfg Config, applier Applier) (*Provider, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("docker provider: %w", err)
	}

	c, err := newClient(&cfg)
	if err != nil {
		return nil, fmt.Errorf("docker provider: %w", err)
	}

	return &Provider{
		cfg:      cfg,
		applier:  applier,
		client:   c,
		trigger:  make(chan struct{}, 1),
		reported: make(map[string]string),
	}, nil
}

// Run watches Docker until ctx is done. Connection errors are retried, the
// last applied configuration keeps serving meanwhile.
func (p *Provider) Run(ctx context.Context) error {
	const maxBackoff = 30 * time.Second

	backoff := time.Second

	for {
		started := time.Now()

		err := p.run(ctx)
		if ctx.Err() != nil {
			return nil
		}

		// a connection that worked for a while starts the backoff over
		if time.Since(started) > maxBackoff {
			backoff = time.Second
		}

		slog.Error("docker provider: connection lost, retrying", "err", err.Error(), "in", backoff.String())

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, maxBackoff)
	}
}

func (p *Provider) run(ctx context.Context) error {
	if err := p.client.negotiate(ctx); err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	types := []string{"container", "network"}
	if p.cfg.Swarm.Enabled {
		// task changes have no events of their own; container events of
		// this node and the refresh interval for other nodes cover them
		types = []string{"service", "container", "network", "node"}
	}

	events := make(chan error, 1)
	go func() {
		events <- p.client.events(runCtx, types, func(apiEvent) { p.notify() })
	}()

	// the first sync after the event stream is open misses no change
	if err := p.sync(runCtx); err != nil {
		return err
	}

	slog.Info("docker provider: watching", "endpoint", p.cfg.endpoint(), "api", p.client.version, "swarm", p.cfg.Swarm.Enabled)

	refresh := time.NewTicker(p.cfg.refreshInterval())
	defer refresh.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-events:
			if err == nil {
				err = errors.New("event stream closed")
			}

			return err
		case <-refresh.C:
		case <-p.trigger:
			if !p.wait(ctx) {
				return nil
			}
		}

		if err := p.sync(runCtx); err != nil && ctx.Err() == nil {
			slog.Error("docker provider: sync failed", "err", err.Error())
		}
	}
}

func (p *Provider) notify() {
	select {
	case p.trigger <- struct{}{}:
	default:
	}
}

// wait collects further events for the debounce duration.
func (p *Provider) wait(ctx context.Context) bool {
	timer := time.NewTimer(p.cfg.debounce())
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-p.trigger:
		case <-timer.C:
			return true
		}
	}
}

func (p *Provider) discover(ctx context.Context) ([]*group, error) {
	if p.cfg.Swarm.Enabled {
		return discoverSwarm(ctx, p.client, &p.cfg)
	}

	return discoverContainers(ctx, p.client, &p.cfg)
}

func (p *Provider) sync(ctx context.Context) error {
	groups, err := p.discover(ctx)
	if err != nil {
		return err
	}

	others := withoutOwned(p.applier.Current())
	fragments, errs := translateAll(&p.cfg, groups, others)

	applyErrs := p.apply(fragments, others)
	maps.Copy(errs, applyErrs)

	p.report(fragments, errs)

	return nil
}

// apply sets the layer. When the merged configuration fails, groups are added
// one by one so a single broken group does not take the others down.
func (p *Provider) apply(fragments map[string]*fragment, others *server.Server) map[string]error {
	next := merge(fragments)

	p.mu.Lock()
	unchanged := p.applied && reflect.DeepEqual(p.last, next)
	// a partly applied configuration is not retried until it or the other
	// layers change
	knownErrs := p.failedErrs
	known := p.applied && p.failed != nil && reflect.DeepEqual(p.failed, next) && reflect.DeepEqual(p.failedOthers, others)
	p.mu.Unlock()

	if unchanged {
		return nil
	}

	if known {
		return knownErrs
	}

	err := p.applier.SetLayer(Layer, next)
	if err == nil {
		p.done(next, len(fragments), nil, nil, nil)

		return nil
	}

	slog.Warn("docker provider: configuration failed, checking containers one by one", "err", err.Error())

	// groups are only checked one by one, so the running configuration does
	// not lose working groups in between
	errs := make(map[string]error)
	accepted := make(map[string]*fragment)

	for _, name := range slices.Sorted(maps.Keys(fragments)) {
		accepted[name] = fragments[name]

		if err := p.applier.CheckLayer(Layer, merge(accepted)); err != nil {
			errs[name] = err
			delete(accepted, name)
		}
	}

	applied := merge(accepted)
	if err := p.applier.SetLayer(Layer, applied); err != nil {
		// other layers changed meanwhile, retried on the next sync
		slog.Error("docker provider: apply failed, previous configuration is kept", "err", err.Error())

		p.mu.Lock()
		p.applied = false
		p.mu.Unlock()

		return errs
	}

	p.done(applied, len(accepted), next, errs, others)

	return errs
}

func (p *Provider) done(s *server.Server, groups int, failed *server.Server, failedErrs map[string]error, others *server.Server) {
	p.mu.Lock()
	p.last = s
	p.applied = true
	p.failed = failed
	p.failedErrs = failedErrs
	p.failedOthers = others
	p.mu.Unlock()

	slog.Info("docker provider: configuration applied",
		"groups", groups,
		"http_routers", len(s.HTTP.Routers),
		"tcp_routers", len(s.TCP.Routers),
		"udp_routers", len(s.UDP.Routers),
	)
}

// report logs rejected groups and warnings once until they change.
func (p *Provider) report(fragments map[string]*fragment, errs map[string]error) {
	current := make(map[string]string)

	for name, err := range errs {
		current[name] = "error: " + err.Error()
	}

	for name, f := range fragments {
		if _, failed := errs[name]; failed || len(f.warnings) == 0 {
			continue
		}

		msgs := make([]string, 0, len(f.warnings))
		for _, w := range f.warnings {
			msgs = append(msgs, w.Error())
		}

		current[name] = "warning: " + strings.Join(msgs, "; ")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for name, msg := range current {
		if p.reported[name] == msg {
			continue
		}

		if err, ok := errs[name]; ok {
			slog.Error("docker provider: container ignored", "name", name, "err", err.Error())
		} else {
			for _, w := range fragments[name].warnings {
				slog.Warn("docker provider: router ignored", "name", name, "err", w.Error())
			}
		}
	}

	p.reported = current
}
