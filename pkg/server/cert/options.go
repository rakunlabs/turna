package cert

import (
	"net"
	"time"
)

type options struct {
	Organization []string
	DNSNames     []string
	IPs          []net.IP
	NotAfter     time.Duration
}

type Options func(*options)

func WithOrganization(organizations ...string) Options {
	return func(opts *options) {
		opts.Organization = append(opts.Organization, organizations...)
	}
}

func WithDNSNames(dnsNames ...string) Options {
	return func(opts *options) {
		opts.DNSNames = append(opts.DNSNames, dnsNames...)
	}
}

func WithIPs(ips ...string) Options {
	return func(opts *options) {
		for _, ip := range ips {
			opts.IPs = append(opts.IPs, net.ParseIP(ip))
		}
	}
}

func WithNotAfter(notAfter time.Duration) Options {
	return func(opts *options) {
		opts.NotAfter = notAfter
	}
}
