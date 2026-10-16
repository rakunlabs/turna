package docker

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strings"
	"time"
)

const (
	labelComposeProject = "com.docker.compose.project"
	labelComposeService = "com.docker.compose.service"
	labelStackNamespace = "com.docker.stack.namespace"
)

// discoverContainers groups the running containers: replicas of a compose
// service form one group, other containers are a group on their own.
func discoverContainers(ctx context.Context, c *client, cfg *Config) ([]*group, error) {
	list, err := c.containers(ctx)
	if err != nil {
		return nil, err
	}

	prefix := cfg.labelPrefix()

	type member struct {
		created time.Time
		labels  map[string]string
		target  string
		ports   map[string][]int
		host    bool
		down    bool
		err     error
	}

	members := make(map[string][]member)

	for i := range list {
		ct := &list[i]

		if !enabled(ct.Labels, prefix, cfg.ExposedByDefault) {
			continue
		}

		name := containerGroup(ct)

		m := member{
			created: time.Unix(ct.Created, 0),
			labels:  ct.Labels,
			ports:   containerPorts(ct),
			host:    ct.HostConfig.NetworkMode == "host",
		}

		// starting or failing replicas do not get traffic, but keep the group
		// routed so requests get 503 instead of another router
		if ct.State != "running" || strings.Contains(ct.Status, "(unhealthy)") || strings.Contains(ct.Status, "(health: starting)") {
			m.down = true
		}

		m.target, m.err = containerAddress(ct, cfg.Network, ct.Labels[prefix+".network"])
		if m.err != nil {
			m.err = fmt.Errorf("container %s: %w", strings.TrimPrefix(firstOr(ct.Names, shortID(ct.ID)), "/"), m.err)
		}

		members[name] = append(members[name], m)
	}

	out := make([]*group, 0, len(members))

	for name, ms := range members {
		// the newest replica carries the current labels during an update
		slices.SortFunc(ms, func(a, b member) int { return a.created.Compare(b.created) })

		g := &group{
			Name:    name,
			Created: ms[0].created,
			Labels:  ms[len(ms)-1].labels,
			Ports:   make(map[string][]int),
		}

		var errs []error

		for _, m := range ms {
			g.HostNetwork = g.HostNetwork || m.host

			if m.err != nil && !m.down {
				errs = append(errs, m.err)

				continue
			}

			if !m.down {
				g.Targets = append(g.Targets, m.target)
			}

			for proto, ports := range m.ports {
				for _, p := range ports {
					if !slices.Contains(g.Ports[proto], p) {
						g.Ports[proto] = append(g.Ports[proto], p)
					}
				}
			}
		}

		// one unreachable replica must not drop the others
		if len(g.Targets) == 0 && len(errs) > 0 {
			g.Err = errors.Join(errs...)
		}

		slices.Sort(g.Targets)

		out = append(out, g)
	}

	return out, nil
}

func firstOr(list []string, def string) string {
	if len(list) > 0 {
		return list[0]
	}

	return def
}

func containerGroup(ct *apiContainer) string {
	project, service := ct.Labels[labelComposeProject], ct.Labels[labelComposeService]
	if project != "" && service != "" {
		return project + "/" + service
	}

	if len(ct.Names) > 0 {
		return strings.TrimPrefix(ct.Names[0], "/")
	}

	return shortID(ct.ID)
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}

	return id
}

func containerPorts(ct *apiContainer) map[string][]int {
	out := make(map[string][]int)
	for _, p := range ct.Ports {
		proto := p.Type
		if proto == "" {
			proto = "tcp"
		}

		if p.PrivatePort > 0 && !slices.Contains(out[proto], p.PrivatePort) {
			out[proto] = append(out[proto], p.PrivatePort)
		}
	}

	for _, ports := range out {
		slices.Sort(ports)
	}

	return out
}

// containerAddress returns the IP of a container on the selected network.
func containerAddress(ct *apiContainer, defaultNetwork, labelNetwork string) (string, error) {
	mode := ct.HostConfig.NetworkMode
	if mode == "host" {
		// only reachable when turna runs on the host network as well
		return "127.0.0.1", nil
	}

	if strings.HasPrefix(mode, "container:") {
		return "", errors.New("network mode container is not supported, label the container owning the network")
	}

	nets := make(map[string]apiEndpoint, len(ct.NetworkSettings.Networks))
	for name, ep := range ct.NetworkSettings.Networks {
		if ep.IPAddress != "" || ep.GlobalIPv6Address != "" {
			nets[name] = ep
		}
	}

	want := labelNetwork
	if want == "" {
		want = defaultNetwork
	}

	var (
		ep apiEndpoint
		ok bool
	)

	switch {
	case want != "":
		ep, ok = nets[want]
		if !ok {
			// compose prefixes network names with the project
			ep, ok = nets[ct.Labels[labelComposeProject]+"_"+want]
		}

		if !ok {
			return "", fmt.Errorf("not attached to network %s", want)
		}
	case len(nets) == 1:
		for _, v := range nets {
			ep = v
		}
	case len(nets) == 0:
		return "", errors.New("has no network address")
	default:
		return "", fmt.Errorf("attached to several networks (%s), set network", strings.Join(slices.Sorted(maps.Keys(nets)), ", "))
	}

	if ep.IPAddress != "" {
		return ep.IPAddress, nil
	}

	return ep.GlobalIPv6Address, nil
}

// discoverSwarm groups running tasks by swarm service.
func discoverSwarm(ctx context.Context, c *client, cfg *Config) ([]*group, error) {
	services, err := c.services(ctx)
	if err != nil {
		return nil, err
	}

	networks, err := c.networks(ctx)
	if err != nil {
		return nil, err
	}

	netByID := make(map[string]apiNetwork, len(networks))
	for _, n := range networks {
		netByID[n.ID] = n
	}

	var tasks []apiTask
	if !cfg.Swarm.UseVIP {
		if tasks, err = c.tasks(ctx); err != nil {
			return nil, err
		}
	}

	prefix := cfg.labelPrefix()
	out := make([]*group, 0, len(services))

	for i := range services {
		svc := &services[i]

		if !enabled(svc.Spec.Labels, prefix, cfg.ExposedByDefault) {
			continue
		}

		g := &group{
			Name:    svc.Spec.Name,
			Created: svc.CreatedAt,
			Labels:  svc.Spec.Labels,
			Ports:   make(map[string][]int),
			Swarm:   true,
		}

		for _, p := range svc.Endpoint.Ports {
			proto := p.Protocol
			if proto == "" {
				proto = "tcp"
			}

			if p.TargetPort > 0 && !slices.Contains(g.Ports[proto], p.TargetPort) {
				g.Ports[proto] = append(g.Ports[proto], p.TargetPort)
			}
		}

		pick := networkPicker(netByID, cfg.Network, svc.Spec.Labels[prefix+".network"], svc.Spec.Labels[labelStackNamespace])

		if cfg.Swarm.UseVIP {
			g.Targets, g.Err = serviceVIP(svc, pick)
		} else {
			g.Targets, g.Err = taskAddresses(svc.ID, tasks, pick)
		}

		if g.Err != nil {
			g.Err = fmt.Errorf("service %s: %w", svc.Spec.Name, g.Err)
		}

		out = append(out, g)
	}

	return out, nil
}

type attachment struct {
	network string
	address string
}

// networkPicker returns a function selecting the address on the wanted
// network, or on the only non-ingress network.
func networkPicker(netByID map[string]apiNetwork, defaultNetwork, labelNetwork, stack string) func([]attachment) (string, error) {
	want := labelNetwork
	if want == "" {
		want = defaultNetwork
	}

	return func(list []attachment) (string, error) {
		candidates := make(map[string]string)
		for _, a := range list {
			n, ok := netByID[a.network]
			if !ok || n.Ingress || a.address == "" {
				continue
			}

			ip, _, err := net.ParseCIDR(a.address)
			if err != nil {
				ip = net.ParseIP(a.address)
			}

			if ip == nil {
				continue
			}

			candidates[n.Name] = ip.String()
		}

		if want != "" {
			if ip, ok := candidates[want]; ok {
				return ip, nil
			}

			if ip, ok := candidates[stack+"_"+want]; ok && stack != "" {
				return ip, nil
			}

			return "", fmt.Errorf("not attached to network %s", want)
		}

		switch len(candidates) {
		case 0:
			return "", errors.New("has no network address besides ingress")
		case 1:
			for _, ip := range candidates {
				return ip, nil
			}
		}

		return "", fmt.Errorf("attached to several networks (%s), set network", strings.Join(slices.Sorted(maps.Keys(candidates)), ", "))
	}
}

func serviceVIP(svc *apiService, pick func([]attachment) (string, error)) ([]string, error) {
	if svc.Spec.EndpointSpec != nil && svc.Spec.EndpointSpec.Mode == "dnsrr" {
		return nil, errors.New("endpoint mode dnsrr has no virtual ip, disable use_vip")
	}

	list := make([]attachment, 0, len(svc.Endpoint.VirtualIPs))
	for _, v := range svc.Endpoint.VirtualIPs {
		list = append(list, attachment{network: v.NetworkID, address: v.Addr})
	}

	ip, err := pick(list)
	if err != nil {
		return nil, err
	}

	return []string{ip}, nil
}

func taskAddresses(serviceID string, tasks []apiTask, pick func([]attachment) (string, error)) ([]string, error) {
	var (
		out  []string
		errs []error
	)

	for _, t := range tasks {
		if t.ServiceID != serviceID || t.Status.State != "running" {
			continue
		}

		list := make([]attachment, 0, len(t.NetworksAttachments))
		for _, na := range t.NetworksAttachments {
			for _, addr := range na.Addresses {
				list = append(list, attachment{network: na.Network.ID, address: addr})
			}
		}

		ip, err := pick(list)
		if err != nil {
			errs = append(errs, fmt.Errorf("task %s: %w", t.ID, err))

			continue
		}

		out = append(out, ip)
	}

	if len(out) == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	slices.Sort(out)

	return out, nil
}
