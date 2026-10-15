package kubernetes

import (
	"fmt"
	"net"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// requestedAddresses returns the IP addresses of gw spec.addresses; an
// address without a value asks for any address. unsupported names the first
// address that is not an IP address.
func requestedAddresses(gw *gwv1.Gateway) (ips []string, unsupported string) {
	for _, a := range gw.Spec.Addresses {
		typ := ptrOr(a.Type, gwv1.IPAddressType)
		if typ != gwv1.IPAddressType {
			return nil, fmt.Sprintf("address type %s is not supported", typ)
		}

		if a.Value == "" {
			continue
		}

		if net.ParseIP(a.Value) == nil {
			return nil, fmt.Sprintf("address %q is not an IP address", a.Value)
		}

		if !slices.Contains(ips, a.Value) {
			ips = append(ips, a.Value)
		}
	}

	return ips, ""
}

// addressProblem explains why the requested addresses of gw cannot be
// requested from the load balancer, empty when they can.
func (c *Config) addressProblem(gw *gwv1.Gateway) string {
	ips, _ := requestedAddresses(gw)
	if len(ips) == 0 {
		return ""
	}

	if c.mode() != ModeController {
		return "spec.addresses needs controller mode"
	}

	if typ := c.Proxy.ServiceType; typ != "" && typ != string(corev1.ServiceTypeLoadBalancer) {
		return "spec.addresses needs proxy.service_type LoadBalancer"
	}

	if len(ips) > 1 && c.Proxy.AddressesAnnotation == "" {
		return fmt.Sprintf("%d addresses %s are requested, more than one needs proxy.addresses_annotation", len(ips), strings.Join(ips, ", "))
	}

	return ""
}

// requestAddresses asks the load balancer of svc for the addresses of gw.
func (c *Config) requestAddresses(svc *corev1.Service, gw *gwv1.Gateway) {
	ips, _ := requestedAddresses(gw)
	if len(ips) == 0 || c.addressProblem(gw) != "" {
		return
	}

	if c.Proxy.AddressesAnnotation == "" {
		svc.Spec.LoadBalancerIP = ips[0]

		if svc.Annotations == nil {
			svc.Annotations = map[string]string{}
		}

		// marks the field as set by turna, see applyProxy
		svc.Annotations[annotationLoadBalancerIP] = ips[0]

		return
	}

	if svc.Annotations == nil {
		svc.Annotations = map[string]string{}
	}

	svc.Annotations[c.Proxy.AddressesAnnotation] = strings.Join(ips, ",")
}

// unassignedAddresses returns the requested addresses of gw the load
// balancer of svc did not assign.
func unassignedAddresses(svc *corev1.Service, gw *gwv1.Gateway) []string {
	ips, _ := requestedAddresses(gw)

	var assigned []string
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		assigned = append(assigned, ing.IP)
	}

	var out []string
	for _, ip := range ips {
		if !slices.ContainsFunc(assigned, func(a string) bool { return net.ParseIP(a).Equal(net.ParseIP(ip)) }) {
			out = append(out, ip)
		}
	}

	return out
}
