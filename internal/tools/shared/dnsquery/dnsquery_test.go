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

// TestDNSLookupTool_Execute_All_WithErrors ensures that errors from CNAME/MX/TXT/NS
// lookups during "all" mode are silently ignored and do not block other record types.
func TestDNSLookupTool_Execute_All_WithErrors(t *testing.T) {
	mock := &mockResolver{
		hosts:    []string{"1.2.3.4"},
		cnameErr: errors.New("no CNAME"),
		mxErr:    errors.New("no MX"),
		txtErr:   errors.New("no TXT"),
		nsErr:    errors.New("no NS"),
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
	// CNAME/MX/TXT/NS errors are ignored in "all" mode.
	if len(r.MX) != 0 {
		t.Errorf("len(MX) = %d, want 0 (error ignored)", len(r.MX))
	}
}

// TestDNSLookupTool_Execute_CNAME_MatchesHostname verifies that a CNAME equal to
// hostname (or hostname+".") is NOT stored in result.CNAME.
func TestDNSLookupTool_Execute_CNAME_MatchesHostname(t *testing.T) {
	mock := &mockResolver{
		cname: "example.com.", // same as hostname with trailing dot — should be skipped
	}
	tool := &dnsquery.DNSLookupTool{Resolver: mock}

	result, err := tool.Execute(context.Background(), map[string]any{
		"hostname":    "example.com",
		"record_type": "CNAME",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	r := result.(dnsquery.DNSResult)
	if r.CNAME != "" {
		t.Errorf("CNAME = %q, want empty (same as hostname)", r.CNAME)
	}
}

// TestDNSLookupTool_Execute_CNAME_Different verifies that a CNAME different from
// the hostname is stored.
func TestDNSLookupTool_Execute_CNAME_Different(t *testing.T) {
	mock := &mockResolver{
		cname: "other.example.com.",
	}
	tool := &dnsquery.DNSLookupTool{Resolver: mock}

	result, err := tool.Execute(context.Background(), map[string]any{
		"hostname":    "example.com",
		"record_type": "CNAME",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	r := result.(dnsquery.DNSResult)
	if r.CNAME != "other.example.com." {
		t.Errorf("CNAME = %q, want other.example.com.", r.CNAME)
	}
}

// TestDNSLookupTool_Execute_MX_Direct exercises MX lookup with record_type="MX"
// when the hosts lookup is also mocked (MX uses its own mock field).
func TestDNSLookupTool_Execute_MX_Error(t *testing.T) {
	mock := &mockResolver{
		hosts: []string{"1.2.3.4"},
		mxErr: errors.New("no MX records"),
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
	if len(r.MX) != 0 {
		t.Errorf("len(MX) = %d, want 0 on error", len(r.MX))
	}
}

// TestDNSLookupTool_Execute_TXT_Error verifies TXT lookup error is silently ignored.
func TestDNSLookupTool_Execute_TXT_Error(t *testing.T) {
	mock := &mockResolver{
		hosts:  []string{"1.2.3.4"},
		txtErr: errors.New("no TXT records"),
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
	if len(r.TXT) != 0 {
		t.Errorf("len(TXT) = %d, want 0 on error", len(r.TXT))
	}
}

// TestDNSLookupTool_Execute_NS_Error verifies NS lookup error is silently ignored.
func TestDNSLookupTool_Execute_NS_Error(t *testing.T) {
	mock := &mockResolver{
		hosts: []string{"1.2.3.4"},
		nsErr: errors.New("no NS records"),
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
	if len(r.NS) != 0 {
		t.Errorf("len(NS) = %d, want 0 on error", len(r.NS))
	}
}

// TestDNSLookupTool_Execute_EmptyHostname ensures empty string triggers the error path.
func TestDNSLookupTool_Execute_EmptyHostname(t *testing.T) {
	tool := &dnsquery.DNSLookupTool{Resolver: &mockResolver{}}
	_, err := tool.Execute(context.Background(), map[string]any{"hostname": ""})
	if err == nil {
		t.Error("expected error for empty hostname")
	}
}

// TestDefaultResolver_Methods exercises the defaultResolver wrapper methods
// using real network calls (best-effort; results may vary by environment).
func TestDefaultResolver_Methods(t *testing.T) {
	// Use NewDNSLookupTool() which uses the real defaultResolver under the hood.
	// We make real DNS calls for localhost/loopback which should always resolve.
	tool := dnsquery.NewDNSLookupTool()

	// LookupHost via "A" record_type for localhost.
	result, err := tool.Execute(context.Background(), map[string]any{
		"hostname":    "localhost",
		"record_type": "A",
	})
	if err != nil {
		t.Fatalf("Execute(localhost, A) error = %v", err)
	}
	_ = result // may have addresses or an error field — either is fine

	// LookupAddr (PTR) for the loopback address.
	result, err = tool.Execute(context.Background(), map[string]any{
		"hostname":    "127.0.0.1",
		"record_type": "PTR",
	})
	if err != nil {
		t.Fatalf("Execute(127.0.0.1, PTR) error = %v", err)
	}
	_ = result

	// LookupCNAME via record_type "CNAME".
	result, err = tool.Execute(context.Background(), map[string]any{
		"hostname":    "localhost",
		"record_type": "CNAME",
	})
	if err != nil {
		t.Fatalf("Execute(localhost, CNAME) error = %v", err)
	}
	_ = result

	// LookupMX via record_type "MX".
	result, err = tool.Execute(context.Background(), map[string]any{
		"hostname":    "localhost",
		"record_type": "MX",
	})
	if err != nil {
		t.Fatalf("Execute(localhost, MX) error = %v", err)
	}
	_ = result

	// LookupTXT via record_type "TXT".
	result, err = tool.Execute(context.Background(), map[string]any{
		"hostname":    "localhost",
		"record_type": "TXT",
	})
	if err != nil {
		t.Fatalf("Execute(localhost, TXT) error = %v", err)
	}
	_ = result

	// LookupNS via record_type "NS".
	result, err = tool.Execute(context.Background(), map[string]any{
		"hostname":    "localhost",
		"record_type": "NS",
	})
	if err != nil {
		t.Fatalf("Execute(localhost, NS) error = %v", err)
	}
	_ = result
}
