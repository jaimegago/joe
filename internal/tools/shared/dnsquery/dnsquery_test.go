package dnsquery_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/jaimegago/joe/internal/tools/shared/dnsquery"
)

// mockResolver returns pre-canned DNS responses.
type mockResolver struct {
	hosts    []string
	hostsErr error
	cname    string
	cnameErr error
	mx       []*net.MX
	mxErr    error
	txt      []string
	txtErr   error
	ns       []*net.NS
	nsErr    error
	ptrs     []string
	ptrErr   error
}

func (m *mockResolver) LookupHost(_ context.Context, _ string) ([]string, error) {
	return m.hosts, m.hostsErr
}

func (m *mockResolver) LookupCNAME(_ context.Context, _ string) (string, error) {
	return m.cname, m.cnameErr
}

func (m *mockResolver) LookupMX(_ context.Context, _ string) ([]*net.MX, error) {
	return m.mx, m.mxErr
}

func (m *mockResolver) LookupTXT(_ context.Context, _ string) ([]string, error) {
	return m.txt, m.txtErr
}

func (m *mockResolver) LookupNS(_ context.Context, _ string) ([]*net.NS, error) {
	return m.ns, m.nsErr
}

func (m *mockResolver) LookupAddr(_ context.Context, _ string) ([]string, error) {
	return m.ptrs, m.ptrErr
}

func TestDNSLookupTool_Name(t *testing.T) {
	tool := dnsquery.NewDNSLookupTool()
	if tool.Name() != "dns_lookup" {
		t.Errorf("Name() = %q, want dns_lookup", tool.Name())
	}
}

func TestDNSLookupTool_Description(t *testing.T) {
	tool := dnsquery.NewDNSLookupTool()
	if tool.Description() == "" {
		t.Error("Description() should not be empty")
	}
}

func TestDNSLookupTool_Parameters(t *testing.T) {
	tool := dnsquery.NewDNSLookupTool()
	params := tool.Parameters()
	if params.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", params.Type)
	}
	if _, ok := params.Properties["hostname"]; !ok {
		t.Error("Parameters() missing 'hostname'")
	}
}

func TestDNSLookupTool_Execute_MissingHostname(t *testing.T) {
	tool := &dnsquery.DNSLookupTool{Resolver: &mockResolver{}}
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Error("expected error for missing hostname, got nil")
	}
}

func TestDNSLookupTool_Execute_A(t *testing.T) {
	mock := &mockResolver{hosts: []string{"1.2.3.4", "5.6.7.8"}}
	tool := &dnsquery.DNSLookupTool{Resolver: mock}

	result, err := tool.Execute(context.Background(), map[string]any{
		"hostname":    "example.com",
		"record_type": "A",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(dnsquery.DNSResult)
	if len(r.A) != 2 {
		t.Errorf("len(A) = %d, want 2", len(r.A))
	}
	if len(r.AAAA) != 0 {
		t.Errorf("len(AAAA) = %d, want 0 for A-only query", len(r.AAAA))
	}
}

func TestDNSLookupTool_Execute_AAAA(t *testing.T) {
	mock := &mockResolver{hosts: []string{"2001:db8::1"}}
	tool := &dnsquery.DNSLookupTool{Resolver: mock}

	result, err := tool.Execute(context.Background(), map[string]any{
		"hostname":    "example.com",
		"record_type": "AAAA",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(dnsquery.DNSResult)
	if len(r.AAAA) != 1 {
		t.Errorf("len(AAAA) = %d, want 1", len(r.AAAA))
	}
}

func TestDNSLookupTool_Execute_MX(t *testing.T) {
	mock := &mockResolver{
		hosts: []string{"1.2.3.4"},
		mx:    []*net.MX{{Host: "mail.example.com.", Pref: 10}},
	}
	tool := &dnsquery.DNSLookupTool{Resolver: mock}

	result, err := tool.Execute(context.Background(), map[string]any{
		"hostname":    "example.com",
		"record_type": "MX",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(dnsquery.DNSResult)
	if len(r.MX) != 1 {
		t.Errorf("len(MX) = %d, want 1", len(r.MX))
	}
	if r.MX[0].Host != "mail.example.com." {
		t.Errorf("MX[0].Host = %q, want mail.example.com.", r.MX[0].Host)
	}
	if r.MX[0].Pref != 10 {
		t.Errorf("MX[0].Pref = %d, want 10", r.MX[0].Pref)
	}
}

func TestDNSLookupTool_Execute_TXT(t *testing.T) {
	mock := &mockResolver{
		hosts: []string{"1.2.3.4"},
		txt:   []string{"v=spf1 include:example.com ~all"},
	}
	tool := &dnsquery.DNSLookupTool{Resolver: mock}

	result, err := tool.Execute(context.Background(), map[string]any{
		"hostname":    "example.com",
		"record_type": "TXT",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(dnsquery.DNSResult)
	if len(r.TXT) != 1 {
		t.Errorf("len(TXT) = %d, want 1", len(r.TXT))
	}
}

func TestDNSLookupTool_Execute_NS(t *testing.T) {
	mock := &mockResolver{
		hosts: []string{"1.2.3.4"},
		ns:    []*net.NS{{Host: "ns1.example.com."}, {Host: "ns2.example.com."}},
	}
	tool := &dnsquery.DNSLookupTool{Resolver: mock}

	result, err := tool.Execute(context.Background(), map[string]any{
		"hostname":    "example.com",
		"record_type": "NS",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(dnsquery.DNSResult)
	if len(r.NS) != 2 {
		t.Errorf("len(NS) = %d, want 2", len(r.NS))
	}
}

func TestDNSLookupTool_Execute_PTR(t *testing.T) {
	mock := &mockResolver{ptrs: []string{"host.example.com."}}
	tool := &dnsquery.DNSLookupTool{Resolver: mock}

	result, err := tool.Execute(context.Background(), map[string]any{
		"hostname":    "1.2.3.4",
		"record_type": "PTR",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(dnsquery.DNSResult)
	if len(r.PTR) != 1 {
		t.Errorf("len(PTR) = %d, want 1", len(r.PTR))
	}
}

func TestDNSLookupTool_Execute_PTRError(t *testing.T) {
	mock := &mockResolver{ptrErr: errors.New("no PTR record")}
	tool := &dnsquery.DNSLookupTool{Resolver: mock}

	result, err := tool.Execute(context.Background(), map[string]any{
		"hostname":    "1.2.3.4",
		"record_type": "PTR",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	r := result.(dnsquery.DNSResult)
	if r.Error == "" {
		t.Error("expected Error in result for PTR failure")
	}
}

func TestDNSLookupTool_Execute_All(t *testing.T) {
	mock := &mockResolver{
		hosts: []string{"1.2.3.4", "2001:db8::1"},
		cname: "alias.example.com.",
		mx:    []*net.MX{{Host: "mail.example.com.", Pref: 10}},
		txt:   []string{"v=spf1 ~all"},
		ns:    []*net.NS{{Host: "ns1.example.com."}},
	}
	tool := &dnsquery.DNSLookupTool{Resolver: mock}

	result, err := tool.Execute(context.Background(), map[string]any{
		"hostname":    "example.com",
		"record_type": "all",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	r := result.(dnsquery.DNSResult)
	if len(r.A) != 1 {
		t.Errorf("len(A) = %d, want 1", len(r.A))
	}
	if len(r.AAAA) != 1 {
		t.Errorf("len(AAAA) = %d, want 1", len(r.AAAA))
	}
	if len(r.MX) != 1 {
		t.Errorf("len(MX) = %d, want 1", len(r.MX))
	}
}

func TestDNSLookupTool_Execute_DefaultTypeIsAll(t *testing.T) {
	mock := &mockResolver{hosts: []string{"1.2.3.4"}}
	tool := &dnsquery.DNSLookupTool{Resolver: mock}

	result, err := tool.Execute(context.Background(), map[string]any{
		"hostname": "example.com",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	r := result.(dnsquery.DNSResult)
	if r.Type != "all" {
		t.Errorf("Type = %q, want all (default)", r.Type)
	}
}

func TestDNSLookupTool_Execute_LookupError(t *testing.T) {
	mock := &mockResolver{hostsErr: errors.New("DNS server unreachable")}
	tool := &dnsquery.DNSLookupTool{Resolver: mock}

	result, err := tool.Execute(context.Background(), map[string]any{
		"hostname":    "dead.example.com",
		"record_type": "A",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	r := result.(dnsquery.DNSResult)
	if r.Error == "" {
		t.Error("expected Error in result for DNS failure")
	}
}

func TestDNSLookupTool_Execute_UnknownType(t *testing.T) {
	tool := &dnsquery.DNSLookupTool{Resolver: &mockResolver{}}
	_, err := tool.Execute(context.Background(), map[string]any{
		"hostname":    "example.com",
		"record_type": "UNKNOWN",
	})
	if err == nil {
		t.Error("expected error for unknown record_type, got nil")
	}
}
