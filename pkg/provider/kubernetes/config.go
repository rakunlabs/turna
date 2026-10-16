// Package kubernetes runs turna as a Kubernetes Gateway API implementation.
// It watches GatewayClasses, Gateways, HTTPRoutes and their references and
// applies the translated configuration to the running server.
package kubernetes

import (
	"errors"
	"net"
	"slices"
	"strconv"
	"time"
)

const DefaultControllerName = "rakunlabs.com/turna"

const (
	// ModeStandalone serves every gateway of the class in this process and
	// writes status. Listeners of different gateways on the same port share
	// one socket.
	ModeStandalone = "standalone"
	// ModeController provisions a proxy Deployment and Service per Gateway
	// and writes status, it does not serve traffic.
	ModeController = "controller"
	// ModeProxy serves the gateways listed in Gateways, it is started by the
	// controller and does not write status.
	ModeProxy = "proxy"
)

type Config struct {
	// Enabled starts the kubernetes provider.
	Enabled bool `cfg:"enabled"`
	// Mode is standalone, controller or proxy; default is standalone.
	Mode string `cfg:"mode"`
	// ControllerName is matched with GatewayClass spec.controllerName.
	ControllerName string `cfg:"controller_name"`
	// ControllerID identifies this installation's proxy resources. Replicas
	// must use the same ID; independent installations must use different IDs.
	// Empty derives an ID from the controller name, scope and election lease.
	ControllerID string `cfg:"controller_id"`
	// Kubeconfig path, empty uses the in-cluster config.
	Kubeconfig string `cfg:"kubeconfig"`
	// Namespaces to watch, empty watches all namespaces.
	Namespaces []string `cfg:"namespaces"`
	// GatewayClass limits the served classes to the GatewayClass with this
	// name and watches only that object; empty serves every class of the
	// controller name.
	GatewayClass string `cfg:"gateway_class"`
	// NamespaceScoped runs without cluster-wide permissions besides reading
	// GatewayClass, so it can be installed by a namespace owner. It needs
	// GatewayClass; empty Namespaces watches the pod namespace. It does not
	// watch Namespace objects (a Selector in allowedRoutes sees no namespace
	// labels) and does not write GatewayClass status.
	NamespaceScoped bool `cfg:"namespace_scoped"`
	// Gateways limits the served gateways as "namespace/name", empty serves
	// every gateway of the matching classes.
	Gateways []string `cfg:"gateways"`
	// CertificateSecret is "namespace/name" of the Secret with the TLS
	// certificates of the served gateways, written by the controller for its
	// proxies. When set, Secrets are not watched in the watched namespaces.
	CertificateSecret string `cfg:"certificate_secret"`

	// PortOffset is added to listener ports for the local entrypoint, useful
	// when running without privileges to bind low ports (80 -> 8080 with 8000).
	PortOffset int `cfg:"port_offset"`
	// Address is the bind host of entrypoints, default is all interfaces.
	Address string `cfg:"address"`

	// HealthAddress serves /healthz and /ready (after the first applied
	// configuration), for example ":9901".
	HealthAddress string `cfg:"health_address"`

	// Debounce collects changes before applying, default is 200ms.
	Debounce time.Duration `cfg:"debounce"`

	// Status configures writing of resource status.
	Status Status `cfg:"status"`

	// Backend configures the generated service middlewares.
	Backend Backend `cfg:"backend"`

	// Proxy configures the resources created per Gateway in controller mode.
	Proxy Proxy `cfg:"proxy"`
}

type Status struct {
	// Disabled turns off status updates in standalone mode.
	Disabled bool `cfg:"disabled"`
	// Addresses reported on Gateways in standalone mode, by default the
	// addresses of the Service named by Service are used.
	Addresses []string `cfg:"addresses"`
	// Service is "namespace/name" of the Service exposing turna in
	// standalone mode, its load balancer ingress is published in Gateway
	// status.
	Service string `cfg:"service"`
	// LeaderElection makes only one replica write status and, in controller
	// mode, provision proxy resources.
	LeaderElection LeaderElection `cfg:"leader_election"`
}

type LeaderElection struct {
	Disabled bool `cfg:"disabled"`
	// Namespace of the lease, default is the pod namespace.
	Namespace string `cfg:"namespace"`
	// Name of the lease, default is "turna-gateway".
	Name string `cfg:"name"`
}

type Backend struct {
	// Retry is the number of retries on another endpoint when an endpoint is
	// unreachable, default is 2.
	Retry *int `cfg:"retry"`
	// Strategy of the load balancer, default is round_robin.
	Strategy string `cfg:"strategy"`
	// UseServiceIP proxies to the cluster IP instead of endpoints.
	UseServiceIP bool `cfg:"use_service_ip"`
	// AllowExternalName permits ExternalName Services as backends. They can
	// point to any host, including addresses outside the cluster that the
	// route owner could not reach otherwise, so they are disabled by default.
	AllowExternalName bool `cfg:"allow_external_name"`
}

type Proxy struct {
	// Namespace of the created resources, default is the pod namespace. The
	// controller needs write access to Deployments, Services, ConfigMaps,
	// Secrets and PodDisruptionBudgets in this namespace.
	Namespace string `cfg:"namespace"`
	// GatewayNamespace creates the proxy resources in the namespace of each
	// Gateway instead of Namespace, with a ServiceAccount per Gateway bound
	// to ClusterRole. The controller needs cluster-wide write access to the
	// proxy resources, ServiceAccounts, Roles, RoleBindings and
	// ClusterRoleBindings, and the bind verb on ClusterRole.
	GatewayNamespace bool `cfg:"gateway_namespace"`
	// ClusterRole grants the proxies read access to the watched resources.
	// When set, every proxy gets its own ServiceAccount bound to it and a
	// Role reading only its certificate Secret, instead of the shared
	// ServiceAccount. The controller then needs write access to
	// ServiceAccounts, Roles and RoleBindings where proxies live, to
	// ClusterRoleBindings, and the bind verb on ClusterRole. Required with
	// GatewayNamespace, not used with namespace_scoped.
	ClusterRole string `cfg:"cluster_role"`
	// Image of the proxy pods. Empty uses the image of the controller pod
	// (POD_NAME), so both stay in sync.
	Image            string   `cfg:"image"`
	ImagePullPolicy  string   `cfg:"image_pull_policy"`
	ImagePullSecrets []string `cfg:"image_pull_secrets"`
	// ServiceAccount shared by the proxy pods, it needs read access to the
	// watched resources and to the certificate Secrets. Not used with
	// ClusterRole.
	ServiceAccount string `cfg:"service_account"`
	// Replicas per gateway, default is 1.
	Replicas int32 `cfg:"replicas"`
	// ServiceType of the gateway Service, default is LoadBalancer.
	ServiceType string `cfg:"service_type"`
	// AddressesAnnotation is the Service annotation that requests the IP
	// addresses of Gateway spec.addresses from the load balancer, for example
	// "metallb.io/loadBalancerIPs". Empty requests a single address with
	// spec.loadBalancerIP.
	AddressesAnnotation string `cfg:"addresses_annotation"`
	// LogLevel of the proxies, default is info.
	LogLevel string `cfg:"log_level"`
	// HealthPort serves the readiness endpoint, default is 9901.
	HealthPort int `cfg:"health_port"`
	// ShutdownDelay keeps a terminating proxy serving while it is removed
	// from load balancers, default is 5s; negative disables it. It uses the
	// preStop sleep action of Kubernetes 1.30 and later.
	ShutdownDelay time.Duration `cfg:"shutdown_delay"`

	CPURequest    string `cfg:"cpu_request"`
	MemoryRequest string `cfg:"memory_request"`
	CPULimit      string `cfg:"cpu_limit"`
	MemoryLimit   string `cfg:"memory_limit"`
}

// ownIdentity reports whether each proxy gets its own ServiceAccount.
func (pc Proxy) ownIdentity() bool {
	return pc.GatewayNamespace || pc.ClusterRole != ""
}

func (pc Proxy) replicas() int32 {
	if pc.Replicas <= 0 {
		return 1
	}

	return pc.Replicas
}

func (pc Proxy) healthPort() int {
	if pc.HealthPort == 0 {
		return 9901
	}

	return pc.HealthPort
}

func (pc Proxy) shutdownDelay() time.Duration {
	switch {
	case pc.ShutdownDelay == 0:
		return 5 * time.Second
	case pc.ShutdownDelay < 0:
		return 0
	default:
		return pc.ShutdownDelay
	}
}

// reservedPort is the local port of the health server, which listeners must
// not use. In controller mode it is the health port of the proxies.
func (c *Config) reservedPort() int {
	if c.mode() == ModeController {
		return c.Proxy.healthPort()
	}

	_, port, err := net.SplitHostPort(c.HealthAddress)
	if err != nil {
		return 0
	}

	p, _ := strconv.Atoi(port)

	return p
}

func (c *Config) mode() string {
	if c.Mode == "" {
		return ModeStandalone
	}

	return c.Mode
}

func (c *Config) validate() error {
	if c.mode() == ModeController && c.Proxy.GatewayNamespace {
		if c.NamespaceScoped {
			return errors.New("proxy.gateway_namespace is not used with namespace_scoped, proxies are already in the gateway namespace")
		}

		if c.Proxy.ClusterRole == "" {
			return errors.New("proxy.gateway_namespace needs proxy.cluster_role")
		}
	}

	if c.mode() == ModeController && c.NamespaceScoped && c.Proxy.ClusterRole != "" {
		return errors.New("proxy.cluster_role is not used with namespace_scoped, it needs cluster-wide bindings; use proxy.service_account")
	}

	if !c.NamespaceScoped {
		return nil
	}

	if len(c.Namespaces) == 0 || slices.Contains(c.Namespaces, "") {
		return errors.New("namespace_scoped needs namespaces")
	}

	if c.GatewayClass == "" {
		return errors.New("namespace_scoped needs gateway_class")
	}

	return nil
}

func (c *Config) controllerName() string {
	if c.ControllerName == "" {
		return DefaultControllerName
	}

	return c.ControllerName
}

func (c *Config) debounce() time.Duration {
	if c.Debounce <= 0 {
		return 200 * time.Millisecond
	}

	return c.Debounce
}
