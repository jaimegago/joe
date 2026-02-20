// Package dnsquery provides a Go-native DNS lookup tool.
// No external CLI dependencies — uses net.Resolver from the standard library.
package dnsquery

import (
	"context"
	"fmt"
	"net"

	"github.com/jaimegago/joe/internal/llm"
)

// Resolver resolves DNS records. Abstracted for testing.
type Resolver interface {
	LookupHost(ctx context.Context, host string) (addrs []string, err error)
	LookupCNAME(ctx context.Context, host string) (cname string, err error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupNS(ctx context.Context, name string) ([]*net.NS, error)
	LookupAddr(ctx context.Context, addr string) (names []string, err error)
}

// defaultResolver wraps net.DefaultResolver.
type defaultResolver struct{}

func (r *defaultResolver) LookupHost(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

func (r *defaultResolver) LookupCNAME(ctx context.Context, host string) (string, error) {
	return net.DefaultResolver.LookupCNAME(ctx, host)
}

func (r *defaultResolver) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	return net.DefaultResolver.LookupMX(ctx, name)
}

func (r *defaultResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	return net.DefaultResolver.LookupTXT(ctx, name)
}

func (r *defaultResolver) LookupNS(ctx context.Context, name string) ([]*net.NS, error) {
	return net.DefaultResolver.LookupNS(ctx, name)
}

func (r *defaultResolver) LookupAddr(ctx context.Context, addr string) ([]string, error) {
	return net.DefaultResolver.LookupAddr(ctx, addr)
}

// DNSResult holds all resolved DNS records for a hostname.
type DNSResult struct {
	Hostname string         `json:"hostname"`
	Type     string         `json:"type"`
	A        []string       `json:"a,omitempty"`
	AAAA     []string       `json:"aaaa,omitempty"`
	CNAME    string         `json:"cname,omitempty"`
	MX       []MXRecord     `json:"mx,omitempty"`
	TXT      []string       `json:"txt,omitempty"`
	NS       []string       `json:"ns,omitempty"`
	PTR      []string       `json:"ptr,omitempty"`
	Error    string         `json:"error,omitempty"`
}

// MXRecord holds a single MX record.
type MXRecord struct {
	Host string `json:"host"`
	Pref uint16 `json:"pref"`
}

// DNSLookupTool performs DNS lookups and returns structured records.
// Replaces dig/nslookup/host for DNS diagnostics.
type DNSLookupTool struct {
	Resolver Resolver
}

// NewDNSLookupTool creates a DNSLookupTool using the system's default resolver.
func NewDNSLookupTool() *DNSLookupTool {
	return &DNSLookupTool{Resolver: &defaultResolver{}}
}

func (t *DNSLookupTool) Name() string { return "dns_lookup" }

func (t *DNSLookupTool) Description() string {
	return "Resolve DNS records for a hostname. Supports A, AAAA, CNAME, MX, TXT, NS, and PTR (reverse) lookups. Returns structured records — replaces dig/nslookup/host. Use record_type='all' to get every available record type."
}

func (t *DNSLookupTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"hostname": {
				Type:        "string",
				Description: "Hostname or IP address to resolve. Use an IP for reverse (PTR) lookup.",
			},
			"record_type": {
				Type:        "string",
				Description: "Record type to look up: A, AAAA, CNAME, MX, TXT, NS, PTR, or all. Default: all.",
			},
		},
		Required: []string{"hostname"},
	}
}

func (t *DNSLookupTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	hostname, ok := args["hostname"].(string)
	if !ok || hostname == "" {
		return nil, fmt.Errorf("missing required parameter: hostname")
	}

	recordType, _ := args["record_type"].(string)
	if recordType == "" {
		recordType = "all"
	}

	result := DNSResult{Hostname: hostname, Type: recordType}

	switch recordType {
	case "A", "AAAA", "all":
		addrs, err := t.Resolver.LookupHost(ctx, hostname)
		if err != nil {
			result.Error = err.Error()
			return result, nil
		}
		for _, addr := range addrs {
			ip := net.ParseIP(addr)
			if ip == nil {
				continue
			}
			if ip.To4() != nil {
				result.A = append(result.A, addr)
			} else {
				result.AAAA = append(result.AAAA, addr)
			}
		}
		if recordType != "all" {
			return result, nil
		}
		fallthrough

	case "CNAME":
		cname, err := t.Resolver.LookupCNAME(ctx, hostname)
		if err == nil && cname != hostname+"." && cname != hostname {
			result.CNAME = cname
		}
		if recordType == "CNAME" {
			return result, nil
		}
		if recordType != "all" {
			break
		}
		fallthrough

	case "MX":
		mxRecords, err := t.Resolver.LookupMX(ctx, hostname)
		if err == nil {
			for _, mx := range mxRecords {
				result.MX = append(result.MX, MXRecord{Host: mx.Host, Pref: mx.Pref})
			}
		}
		if recordType == "MX" {
			return result, nil
		}
		if recordType != "all" {
			break
		}
		fallthrough

	case "TXT":
		txts, err := t.Resolver.LookupTXT(ctx, hostname)
		if err == nil {
			result.TXT = txts
		}
		if recordType == "TXT" {
			return result, nil
		}
		if recordType != "all" {
			break
		}
		fallthrough

	case "NS":
		nsRecords, err := t.Resolver.LookupNS(ctx, hostname)
		if err == nil {
			for _, ns := range nsRecords {
				result.NS = append(result.NS, ns.Host)
			}
		}
		if recordType == "NS" {
			return result, nil
		}

	case "PTR":
		names, err := t.Resolver.LookupAddr(ctx, hostname)
		if err != nil {
			result.Error = err.Error()
			return result, nil
		}
		result.PTR = names
		return result, nil

	default:
		return nil, fmt.Errorf("unknown record_type %q: use A, AAAA, CNAME, MX, TXT, NS, PTR, or all", recordType)
	}

	return result, nil
}
