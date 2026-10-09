package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	discoveryinformers "k8s.io/client-go/informers/discovery/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
	gwclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
	gwinformers "sigs.k8s.io/gateway-api/pkg/client/informers/externalversions"
	gwinformersv1 "sigs.k8s.io/gateway-api/pkg/client/informers/externalversions/apis/v1"
	gwinformersv1beta1 "sigs.k8s.io/gateway-api/pkg/client/informers/externalversions/apis/v1beta1"

	"github.com/rakunlabs/turna/pkg/server"
)

// Layer is the server configuration layer owned by the provider.
const Layer = "kubernetes"

// Applier receives translated configurations.
type Applier interface {
	SetLayer(name string, s *server.Server) error
}

type Provider struct {
	cfg     Config
	applier Applier

	kube kubernetes.Interface
	gw   gwclient.Interface

	listers listers
	trigger chan struct{}

	status *statusWriter

	mu        sync.Mutex
	last      *server.Server
	applied   bool
	ready     chan struct{}
	readyOnce sync.Once
}

type listers struct {
	classes   func() ([]*gwv1.GatewayClass, error)
	gateways  func() ([]*gwv1.Gateway, error)
	routes    func() ([]*gwv1.HTTPRoute, error)
	grpc      func() ([]*gwv1.GRPCRoute, error)
	tls       func() ([]*gwv1.TLSRoute, error)
	tcp       func() ([]*gwv1.TCPRoute, error)
	udp       func() ([]*gwv1.UDPRoute, error)
	grants    func() ([]*gwv1beta1.ReferenceGrant, error)
	services  func() ([]*corev1.Service, error)
	slices    func() ([]*discoveryv1.EndpointSlice, error)
	secrets   func() ([]*corev1.Secret, error)
	namespace func() ([]*corev1.Namespace, error)
}

func restConfig(path string) (*rest.Config, error) {
	if path == "" {
		if c, err := rest.InClusterConfig(); err == nil {
			return c, nil
		}
	}

	// The default loading rules merge the KUBECONFIG path list. Only the
	// explicitly configured single file should become ExplicitPath.
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if path != "" {
		rules.ExplicitPath = path
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
}

// New creates a provider using the kube config of cfg.
func New(cfg Config, applier Applier) (*Provider, error) {
	rc, err := restConfig(cfg.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("kubernetes config: %w", err)
	}

	rc.UserAgent = "turna-gateway"

	kube, err := kubernetes.NewForConfig(rc)
	if err != nil {
		return nil, err
	}

	gw, err := gwclient.NewForConfig(rc)
	if err != nil {
		return nil, err
	}

	return NewWithClients(cfg, applier, kube, gw), nil
}

// NewWithClients creates a provider with the given clients.
func NewWithClients(cfg Config, applier Applier, kube kubernetes.Interface, gw gwclient.Interface) *Provider {
	return &Provider{
		cfg:     cfg,
		applier: applier,
		kube:    kube,
		gw:      gw,
		trigger: make(chan struct{}, 1),
		ready:   make(chan struct{}),
	}
}

// Ready is closed after the first configuration is applied.
func (p *Provider) Ready() <-chan struct{} {
	return p.ready
}

// Run watches resources until ctx is done.
func (p *Provider) Run(ctx context.Context) error {
	namespaces := p.cfg.Namespaces
	if len(namespaces) == 0 {
		namespaces = []string{corev1.NamespaceAll}
	}

	var (
		syncs    []cache.InformerSynced
		starters []func(<-chan struct{})
		perNS    []nsListers
	)

	available := p.availableRoutes()

	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { p.notify() },
		UpdateFunc: func(any, any) { p.notify() },
		DeleteFunc: func(any) { p.notify() },
	}

	for _, ns := range namespaces {
		kf := informers.NewSharedInformerFactoryWithOptions(p.kube, 0, informers.WithNamespace(ns))
		gf := gwinformers.NewSharedInformerFactoryWithOptions(p.gw, 0, gwinformers.WithNamespace(ns))

		l := nsListers{
			gateways: gf.Gateway().V1().Gateways(),
			routes:   gf.Gateway().V1().HTTPRoutes(),
			grants:   gf.Gateway().V1beta1().ReferenceGrants(),
			services: kf.Core().V1().Services(),
			slices:   kf.Discovery().V1().EndpointSlices(),
			secrets:  kf.Core().V1().Secrets(),
		}

		infs := []cache.SharedIndexInformer{
			l.gateways.Informer(), l.routes.Informer(), l.grants.Informer(),
			l.services.Informer(), l.slices.Informer(), l.secrets.Informer(),
		}

		// optional route kinds are watched only when their CRD is installed
		if available[kindGRPCRoute] {
			l.grpc = gf.Gateway().V1().GRPCRoutes()
			infs = append(infs, l.grpc.Informer())
		}
		if available[kindTLSRoute] {
			l.tls = gf.Gateway().V1().TLSRoutes()
			infs = append(infs, l.tls.Informer())
		}
		if available[kindTCPRoute] {
			l.tcp = gf.Gateway().V1().TCPRoutes()
			infs = append(infs, l.tcp.Informer())
		}
		if available[kindUDPRoute] {
			l.udp = gf.Gateway().V1().UDPRoutes()
			infs = append(infs, l.udp.Informer())
		}

		for _, inf := range infs {
			if _, err := inf.AddEventHandler(handler); err != nil {
				return err
			}

			syncs = append(syncs, inf.HasSynced)
		}

		perNS = append(perNS, l)
		starters = append(starters, kf.Start, gf.Start)
	}

	// cluster scoped resources
	cf := informers.NewSharedInformerFactory(p.kube, 0)
	gcf := gwinformers.NewSharedInformerFactory(p.gw, 0)

	classes := gcf.Gateway().V1().GatewayClasses()
	nsInf := cf.Core().V1().Namespaces()

	for _, inf := range []cache.SharedIndexInformer{classes.Informer(), nsInf.Informer()} {
		if _, err := inf.AddEventHandler(handler); err != nil {
			return err
		}

		syncs = append(syncs, inf.HasSynced)
	}

	starters = append(starters, cf.Start, gcf.Start)

	if p.cfg.mode() == ModeController {
		// proxy readiness and load balancer addresses feed gateway status
		pf := informers.NewSharedInformerFactoryWithOptions(p.kube, 0,
			informers.WithNamespace(p.proxyNamespace()),
			informers.WithTweakListOptions(func(o *metav1.ListOptions) {
				o.LabelSelector = labelManagedBy + "=" + managedByValue
			}),
		)

		for _, inf := range []cache.SharedIndexInformer{pf.Apps().V1().Deployments().Informer(), pf.Core().V1().Services().Informer()} {
			if _, err := inf.AddEventHandler(handler); err != nil {
				return err
			}

			syncs = append(syncs, inf.HasSynced)
		}

		starters = append(starters, pf.Start)
	}

	p.listers = listers{
		classes:   func() ([]*gwv1.GatewayClass, error) { return classes.Lister().List(labels.Everything()) },
		namespace: func() ([]*corev1.Namespace, error) { return nsInf.Lister().List(labels.Everything()) },
		gateways: collect(perNS, func(l nsListers) ([]*gwv1.Gateway, error) {
			return l.gateways.Lister().List(labels.Everything())
		}),
		routes: collect(perNS, func(l nsListers) ([]*gwv1.HTTPRoute, error) {
			return l.routes.Lister().List(labels.Everything())
		}),
		grpc: collect(perNS, func(l nsListers) ([]*gwv1.GRPCRoute, error) {
			if l.grpc == nil {
				return nil, nil
			}

			return l.grpc.Lister().List(labels.Everything())
		}),
		tls: collect(perNS, func(l nsListers) ([]*gwv1.TLSRoute, error) {
			if l.tls == nil {
				return nil, nil
			}

			return l.tls.Lister().List(labels.Everything())
		}),
		tcp: collect(perNS, func(l nsListers) ([]*gwv1.TCPRoute, error) {
			if l.tcp == nil {
				return nil, nil
			}

			return l.tcp.Lister().List(labels.Everything())
		}),
		udp: collect(perNS, func(l nsListers) ([]*gwv1.UDPRoute, error) {
			if l.udp == nil {
				return nil, nil
			}

			return l.udp.Lister().List(labels.Everything())
		}),
		grants: collect(perNS, func(l nsListers) ([]*gwv1beta1.ReferenceGrant, error) {
			return l.grants.Lister().List(labels.Everything())
		}),
		services: collect(perNS, func(l nsListers) ([]*corev1.Service, error) {
			return l.services.Lister().List(labels.Everything())
		}),
		slices: collect(perNS, func(l nsListers) ([]*discoveryv1.EndpointSlice, error) {
			return l.slices.Lister().List(labels.Everything())
		}),
		secrets: collect(perNS, func(l nsListers) ([]*corev1.Secret, error) {
			return l.secrets.Lister().List(labels.Everything())
		}),
	}

	for _, start := range starters {
		start(ctx.Done())
	}

	slog.Info("kubernetes provider: waiting for cache sync", "controller", p.cfg.controllerName())

	if !cache.WaitForCacheSync(ctx.Done(), syncs...) {
		return errors.New("kubernetes provider: cache sync failed")
	}

	mode := p.cfg.mode()
	switch mode {
	case ModeStandalone, ModeController, ModeProxy:
	default:
		return fmt.Errorf("kubernetes provider: unknown mode %q", mode)
	}

	if mode == ModeController || mode != ModeProxy && !p.cfg.Status.Disabled {
		p.status = newStatusWriter(p)
		go p.status.run(ctx)
	}

	if p.cfg.HealthAddress != "" {
		go p.serveHealth(ctx)
	}

	p.notify()
	p.loop(ctx)

	return nil
}

type nsListers struct {
	gateways gwinformersv1.GatewayInformer
	routes   gwinformersv1.HTTPRouteInformer
	grpc     gwinformersv1.GRPCRouteInformer
	tls      gwinformersv1.TLSRouteInformer
	tcp      gwinformersv1.TCPRouteInformer
	udp      gwinformersv1.UDPRouteInformer
	grants   gwinformersv1beta1.ReferenceGrantInformer
	services coreinformers.ServiceInformer
	slices   discoveryinformers.EndpointSliceInformer
	secrets  coreinformers.SecretInformer
}

func collect[T any](per []nsListers, fn func(nsListers) ([]T, error)) func() ([]T, error) {
	return func() ([]T, error) {
		var out []T

		for _, l := range per {
			items, err := fn(l)
			if err != nil {
				return nil, err
			}

			out = append(out, items...)
		}

		return out, nil
	}
}

func (p *Provider) notify() {
	select {
	case p.trigger <- struct{}{}:
	default:
	}
}

func (p *Provider) loop(ctx context.Context) {
	var retryTimer *time.Timer
	var retryC <-chan time.Time
	backoff := 200 * time.Millisecond
	defer func() {
		if retryTimer != nil {
			retryTimer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.trigger:
		case <-retryC:
		}
		if retryTimer != nil {
			retryTimer.Stop()
		}
		retryC = nil

		// collect bursts of events
		timer := time.NewTimer(p.cfg.debounce())
		select {
		case <-ctx.Done():
			timer.Stop()

			return
		case <-timer.C:
		}

		if err := p.sync(ctx); err != nil {
			slog.Error("kubernetes provider: sync failed", "err", err.Error())
			retryTimer = time.NewTimer(backoff)
			retryC = retryTimer.C
			backoff = min(backoff*2, 30*time.Second)
		} else {
			backoff = 200 * time.Millisecond
		}
	}
}

func (p *Provider) snapshot() (*Snapshot, error) {
	s := &Snapshot{}

	var err error

	if s.GatewayClasses, err = p.listers.classes(); err != nil {
		return nil, err
	}
	if s.Gateways, err = p.listers.gateways(); err != nil {
		return nil, err
	}
	if s.HTTPRoutes, err = p.listers.routes(); err != nil {
		return nil, err
	}
	if s.GRPCRoutes, err = p.listers.grpc(); err != nil {
		return nil, err
	}
	if s.TLSRoutes, err = p.listers.tls(); err != nil {
		return nil, err
	}
	if s.TCPRoutes, err = p.listers.tcp(); err != nil {
		return nil, err
	}
	if s.UDPRoutes, err = p.listers.udp(); err != nil {
		return nil, err
	}
	if s.ReferenceGrants, err = p.listers.grants(); err != nil {
		return nil, err
	}
	if s.Services, err = p.listers.services(); err != nil {
		return nil, err
	}
	if s.EndpointSlices, err = p.listers.slices(); err != nil {
		return nil, err
	}
	if s.Secrets, err = p.listers.secrets(); err != nil {
		return nil, err
	}
	if s.Namespaces, err = p.listers.namespace(); err != nil {
		return nil, err
	}

	return s, nil
}

func (p *Provider) sync(ctx context.Context) error {
	snap, err := p.snapshot()
	if err != nil {
		return err
	}

	res := Translate(&p.cfg, snap)

	if p.cfg.mode() == ModeController {
		// proxies serve traffic, the controller only provisions and reports
		leaderCtx, leader := p.status.leadership()
		if !leader {
			// A synced standby is ready to take over but must not mutate proxies.
			p.markReady()
			return nil
		}
		provisionErr := p.provision(leaderCtx, res.served)
		if provisionErr == nil {
			p.markReady()
		}

		if p.status != nil {
			p.status.update(snap, res, provisionErr)
		}

		return provisionErr
	}

	p.mu.Lock()
	changed := !p.applied || !reflect.DeepEqual(p.last, res.Server)
	p.mu.Unlock()

	applyErr := error(nil)
	if changed {
		applyErr = p.applier.SetLayer(Layer, res.Server)
		if applyErr != nil {
			slog.Error("kubernetes provider: apply failed, previous configuration is kept", "err", applyErr.Error())
		} else {
			p.mu.Lock()
			p.last = res.Server
			p.applied = true
			p.mu.Unlock()

			slog.Info("kubernetes provider: configuration applied",
				"entrypoints", len(res.Server.EntryPoints), "routers", len(res.Server.HTTP.Routers))

			p.markReady()
		}
	}

	if p.status != nil {
		p.status.update(snap, res, applyErr)
	}

	return applyErr
}

// availableRoutes reports which optional route CRDs are installed. All are
// assumed available when discovery fails.
func (p *Provider) availableRoutes() map[gwv1.Kind]bool {
	out := map[gwv1.Kind]bool{kindGRPCRoute: true, kindTLSRoute: true, kindTCPRoute: true, kindUDPRoute: true}

	res, err := p.gw.Discovery().ServerResourcesForGroupVersion(gwv1.GroupVersion.String())
	if err != nil {
		slog.Debug("kubernetes provider: discovery failed, watching all route kinds", "err", err.Error())

		return out
	}

	found := map[string]bool{}
	for _, r := range res.APIResources {
		found[r.Kind] = true
	}

	for kind := range out {
		if !found[string(kind)] {
			out[kind] = false
			slog.Info("kubernetes provider: route kind not installed", "kind", kind)
		}
	}

	return out
}

func (p *Provider) markReady() {
	p.readyOnce.Do(func() { close(p.ready) })
}

func splitNsName(s string) (nsName, bool) {
	ns, name, ok := strings.Cut(s, "/")
	if !ok || ns == "" || name == "" {
		return nsName{}, false
	}

	return nsName{Namespace: ns, Name: name}, true
}
