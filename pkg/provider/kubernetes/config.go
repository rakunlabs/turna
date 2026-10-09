// Package kubernetes runs turna as a Kubernetes Gateway API implementation.
// It watches GatewayClasses, Gateways, HTTPRoutes and their references and
// applies the translated configuration to the running server.
package kubernetes

import (
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
	// Kubeconfig path, empty uses the in-cluster config.
	Kubeconfig string `cfg:"kubeconfig"`
	// Namespaces to watch, empty watches all namespaces.
	Namespaces []string `cfg:"namespaces"`
	// Gateways limits the served gateways as "namespace/name", empty serves
	// every gateway of the matching classes.
	Gateways []string `cfg:"gateways"`

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
	// LeaderElection makes only one replica write status.
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
}

type Proxy struct {
	// Namespace of the created resources, default is the pod namespace.
	Namespace string `cfg:"namespace"`
	// Image of the proxy pods, usually the controller image.
	Image            string   `cfg:"image"`
	ImagePullPolicy  string   `cfg:"image_pull_policy"`
	ImagePullSecrets []string `cfg:"image_pull_secrets"`
	// ServiceAccount of the proxy pods, it needs read access to the watched
	// resources.
	ServiceAccount string `cfg:"service_account"`
	// Replicas per gateway, default is 1.
	Replicas int32 `cfg:"replicas"`
	// ServiceType of the gateway Service, default is LoadBalancer.
	ServiceType string `cfg:"service_type"`
	// LogLevel of the proxies, default is info.
	LogLevel string `cfg:"log_level"`
	// HealthPort serves the readiness endpoint, default is 9901.
	HealthPort int `cfg:"health_port"`

	CPURequest    string `cfg:"cpu_request"`
	MemoryRequest string `cfg:"memory_request"`
	CPULimit      string `cfg:"cpu_limit"`
	MemoryLimit   string `cfg:"memory_limit"`
}

func (c *Config) mode() string {
	if c.Mode == "" {
		return ModeStandalone
	}

	return c.Mode
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
