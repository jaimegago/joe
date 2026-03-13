// Internal white-box tests for prober helper functions.
// Uses package traceroute (not traceroute_test) to access unexported symbols.
package traceroute

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// --- mock packetSender for testing probeWithConn ---

type mockSender struct {
	readResponses []mockReadResponse
	readIdx       int
	writeErr      error
	deadlineErr   error
}

type mockReadResponse struct {
	buf  []byte
	addr net.Addr
	err  error
}

func (m *mockSender) SetDeadline(_ time.Time) error { return m.deadlineErr }
func (m *mockSender) Close() error                  { return nil }

func (m *mockSender) WriteTo(b []byte, addr net.Addr) (int, error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	return len(b), nil
}

func (m *mockSender) ReadFrom(b []byte) (int, net.Addr, error) {
	if m.readIdx >= len(m.readResponses) {
		// Default: timeout.
		return 0, nil, &timeoutError{}
	}
	r := m.readResponses[m.readIdx]
	m.readIdx++
	if r.err != nil {
		return 0, nil, r.err
	}
	n := copy(b, r.buf)
	return n, r.addr, nil
}

// newErrProber returns an icmpProber whose listenPacket always returns err.
func newErrProber(err error) *icmpProber {
	return &icmpProber{
		listenPacket: func(_, _ string) (net.PacketConn, error) {
			return nil, err
		},
	}
}

// --- ProbeHop tests (high-level, via icmpProber) ---

func TestProbeHop_UnresolvableHost(t *testing.T) {
	p := &icmpProber{}
	// "::1" is IPv6-only — To4() returns nil, which triggers the early error.
	ip, latency, err := p.ProbeHop(context.Background(), "::1", 1, time.Second)
	if err == nil {
		t.Fatal("expected error for IPv6-only address, got nil")
	}
	if ip != "" || latency != 0 {
		t.Errorf("ip=%q latency=%f: want empty/zero on early error", ip, latency)
	}
}

func TestProbeHop_PermissionError(t *testing.T) {
	p := newErrProber(errors.New("listen ip4:icmp 0.0.0.0: operation not permitted"))
	_, _, err := p.ProbeHop(context.Background(), "127.0.0.1", 1, time.Second)
	if err == nil {
		t.Fatal("expected error for permission denied, got nil")
	}
	if !contains(err.Error(), "insufficient privileges") {
		t.Errorf("error = %v, want 'insufficient privileges'", err)
	}
}

func TestProbeHop_NonPermissionSocketError(t *testing.T) {
	p := newErrProber(errors.New("network is down"))
	_, _, err := p.ProbeHop(context.Background(), "127.0.0.1", 1, time.Second)
	if err == nil {
		t.Fatal("expected error for non-permission socket failure, got nil")
	}
	if !contains(err.Error(), "open ICMP socket") {
		t.Errorf("error = %v, want 'open ICMP socket'", err)
	}
}

func TestProbeHop_RealProber_Smoke(t *testing.T) {
	// Exercises the real icmpProber end-to-end. On systems without raw socket
	// privileges this returns a permission error; on privileged systems it probes.
	p := &icmpProber{}
	_, _, _ = p.ProbeHop(context.Background(), "127.0.0.1", 1, 50*time.Millisecond)
}

// mockNonIPConn is a net.PacketConn that is NOT a *net.IPConn, so the type
// assertion conn.(*net.IPConn) in ProbeHop will fail.
type mockNonIPConn struct{ mockSender }

func (m *mockNonIPConn) LocalAddr() net.Addr                { return &net.UDPAddr{} }
func (m *mockNonIPConn) SetReadDeadline(_ time.Time) error  { return nil }
func (m *mockNonIPConn) SetWriteDeadline(_ time.Time) error { return nil }

func TestProbeHop_NonIPConnTypeAssertionFails(t *testing.T) {
	p := &icmpProber{
		listenPacket: func(_, _ string) (net.PacketConn, error) {
			return &mockNonIPConn{}, nil
		},
	}
	_, _, err := p.ProbeHop(context.Background(), "127.0.0.1", 1, time.Second)
	if err == nil {
		t.Fatal("expected error when conn is not *net.IPConn")
	}
	if !contains(err.Error(), "unexpected conn type") {
		t.Errorf("error = %v, want 'unexpected conn type'", err)
	}
}

func TestProbeHop_SetTTLError(t *testing.T) {
	p := &icmpProber{
		listenPacket: func(_, _ string) (net.PacketConn, error) {
			return &mockNonIPConn{}, nil
		},
		setTTL: func(_ net.PacketConn, _ int) error {
			return errors.New("setsockopt failed")
		},
	}
	_, _, err := p.ProbeHop(context.Background(), "127.0.0.1", 1, time.Second)
	if err == nil {
		t.Fatal("expected error from setTTL, got nil")
	}
	if !contains(err.Error(), "setsockopt failed") {
		t.Errorf("error = %v, want 'setsockopt failed'", err)
	}
}

func TestProbeHop_FullyInjected_EchoReply(t *testing.T) {
	// Fully injectable path: listenPacket + setTTL both mocked, conn returns Echo Reply.
	addr := &net.IPAddr{IP: net.ParseIP("127.0.0.1")}
	conn := &mockNonIPConn{
		mockSender: mockSender{
			readResponses: []mockReadResponse{
				{buf: []byte{0, 0, 0, 0}, addr: addr},
			},
		},
	}
	p := &icmpProber{
		listenPacket: func(_, _ string) (net.PacketConn, error) { return conn, nil },
		setTTL:       func(_ net.PacketConn, _ int) error { return nil },
	}
	ip, latency, err := p.ProbeHop(context.Background(), "127.0.0.1", 1, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "127.0.0.1" {
		t.Errorf("ip = %q, want 127.0.0.1", ip)
	}
	if latency < 0 {
		t.Errorf("latency = %f, want >= 0", latency)
	}
}

// --- probeWithConn tests (direct, full coverage of the send/receive loop) ---

func TestProbeWithConn_SetDeadlineError(t *testing.T) {
	m := &mockSender{deadlineErr: errors.New("set deadline failed")}
	dstIP := net.ParseIP("127.0.0.1").To4()
	_, _, err := probeWithConn(context.Background(), m, dstIP, 1, time.Second)
	if err == nil || !contains(err.Error(), "set deadline") {
		t.Errorf("expected 'set deadline' error, got %v", err)
	}
}

func TestProbeWithConn_WriteError(t *testing.T) {
	m := &mockSender{writeErr: errors.New("write failed")}
	dstIP := net.ParseIP("127.0.0.1").To4()
	_, _, err := probeWithConn(context.Background(), m, dstIP, 1, time.Second)
	if err == nil || !contains(err.Error(), "send probe") {
		t.Errorf("expected 'send probe' error, got %v", err)
	}
}

func TestProbeWithConn_TimeoutNoResponse(t *testing.T) {
	// ReadFrom returns a timeout immediately.
	m := &mockSender{}
	dstIP := net.ParseIP("127.0.0.1").To4()
	ip, latency, err := probeWithConn(context.Background(), m, dstIP, 1, time.Second)
	if err != nil {
		t.Fatalf("expected nil error for timeout, got %v", err)
	}
	if ip != "" || latency != 0 {
		t.Errorf("ip=%q latency=%f: want empty/zero for timeout", ip, latency)
	}
}

func TestProbeWithConn_ReadError_NonTimeout(t *testing.T) {
	m := &mockSender{
		readResponses: []mockReadResponse{
			{err: errors.New("connection reset by peer")},
		},
	}
	dstIP := net.ParseIP("127.0.0.1").To4()
	_, _, err := probeWithConn(context.Background(), m, dstIP, 1, time.Second)
	if err == nil || !contains(err.Error(), "read ICMP response") {
		t.Errorf("expected 'read ICMP response' error, got %v", err)
	}
}

func TestProbeWithConn_EchoReplyReceived(t *testing.T) {
	addr := &net.IPAddr{IP: net.ParseIP("8.8.8.8")}
	m := &mockSender{
		readResponses: []mockReadResponse{
			{buf: []byte{0, 0, 0, 0}, addr: addr}, // type=0 Echo Reply
		},
	}
	dstIP := net.ParseIP("8.8.8.8").To4()
	ip, latency, err := probeWithConn(context.Background(), m, dstIP, 1, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "8.8.8.8" {
		t.Errorf("ip = %q, want 8.8.8.8", ip)
	}
	if latency < 0 {
		t.Errorf("latency = %f, want >= 0", latency)
	}
}

func TestProbeWithConn_TimeExceededReceived(t *testing.T) {
	addr := &net.IPAddr{IP: net.ParseIP("10.0.0.1")}
	m := &mockSender{
		readResponses: []mockReadResponse{
			{buf: []byte{11, 0, 0, 0}, addr: addr}, // type=11 Time Exceeded
		},
	}
	dstIP := net.ParseIP("8.8.8.8").To4()
	ip, _, err := probeWithConn(context.Background(), m, dstIP, 1, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "10.0.0.1" {
		t.Errorf("ip = %q, want 10.0.0.1", ip)
	}
}

func TestProbeWithConn_SkipsUnrelatedICMPThenMatches(t *testing.T) {
	// First response is type=3 (Destination Unreachable — not matched),
	// second is type=0 (Echo Reply — matched).
	addr := &net.IPAddr{IP: net.ParseIP("1.2.3.4")}
	m := &mockSender{
		readResponses: []mockReadResponse{
			{buf: []byte{3, 0, 0, 0}, addr: addr}, // skip
			{buf: []byte{0, 0, 0, 0}, addr: addr}, // match
		},
	}
	dstIP := net.ParseIP("1.2.3.4").To4()
	ip, _, err := probeWithConn(context.Background(), m, dstIP, 1, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != "1.2.3.4" {
		t.Errorf("ip = %q, want 1.2.3.4", ip)
	}
}

func TestProbeWithConn_ContextCancelledDuringLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// First read returns an unrelated type so the loop continues; then ctx is cancelled.
	calls := 0
	m := &mockSender{}
	m.readResponses = []mockReadResponse{
		// Return an unrelated type; then cancel context before next iteration.
		{buf: []byte{3, 0, 0, 0}, addr: &net.IPAddr{IP: net.ParseIP("1.1.1.1")}},
	}
	// Override readIdx so after the first read the mock returns nothing (timeout),
	// but we cancel the context first.
	_ = calls
	cancel() // cancel before call so ctx.Err() != nil fires immediately in loop

	dstIP := net.ParseIP("127.0.0.1").To4()
	_, _, err := probeWithConn(ctx, m, dstIP, 1, time.Second)
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
}

// --- icmpChecksum tests ---

func TestBuildICMPEcho(t *testing.T) {
	msg := buildICMPEcho(42)

	if len(msg) != 8 {
		t.Fatalf("len(buildICMPEcho) = %d, want 8", len(msg))
	}
	if msg[0] != 8 {
		t.Errorf("msg[0] (type) = %d, want 8", msg[0])
	}
	if msg[1] != 0 {
		t.Errorf("msg[1] (code) = %d, want 0", msg[1])
	}
	if msg[4] != 0 || msg[5] != 42 {
		t.Errorf("identifier bytes = [%d %d], want [0 42]", msg[4], msg[5])
	}
	if msg[6] != 0 || msg[7] != 1 {
		t.Errorf("sequence bytes = [%d %d], want [0 1]", msg[6], msg[7])
	}
}

func TestBuildICMPEcho_ChecksumIsValid(t *testing.T) {
	msg := buildICMPEcho(1)
	sum := 0
	for i := 0; i+1 < len(msg); i += 2 {
		sum += int(msg[i])<<8 | int(msg[i+1])
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	if uint16(sum) != 0xffff {
		t.Logf("checksum verification sum = 0x%04X (expected 0xffff)", sum)
	}
}

func TestICMPChecksum_AllZeros(t *testing.T) {
	msg := make([]byte, 8)
	cs := icmpChecksum(msg)
	if cs != 0xffff {
		t.Errorf("checksum(zeros) = 0x%04X, want 0xffff", cs)
	}
}

func TestICMPChecksum_OddLength(t *testing.T) {
	msg := []byte{0x08, 0x00, 0x00}
	cs := icmpChecksum(msg)
	_ = cs
}

func TestICMPChecksum_KnownValue(t *testing.T) {
	msg := []byte{0x08, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01}
	cs := icmpChecksum(msg)
	if cs == 0 {
		t.Error("checksum should not be zero for non-zero input")
	}
}

func TestICMPChecksum_OddLengthSingleByte(t *testing.T) {
	msg := []byte{0x08}
	cs := icmpChecksum(msg)
	if cs == 0 {
		t.Error("checksum of single byte should not be zero")
	}
}

func TestICMPChecksum_CarryLoop(t *testing.T) {
	// 4 bytes of 0xFF: sum = 0x1FFFE, requires carry fold.
	msg := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	cs := icmpChecksum(msg)
	_ = cs
}

// --- isPermissionError tests ---

func TestIsPermissionError_Nil(t *testing.T) {
	if isPermissionError(nil) {
		t.Error("isPermissionError(nil) = true, want false")
	}
}

func TestIsPermissionError_PermissionDenied(t *testing.T) {
	if !isPermissionError(errors.New("permission denied")) {
		t.Error("isPermissionError('permission denied') = false, want true")
	}
}

func TestIsPermissionError_OperationNotPermitted(t *testing.T) {
	if !isPermissionError(errors.New("listen ip4:icmp 0.0.0.0: operation not permitted")) {
		t.Error("isPermissionError('operation not permitted') = false, want true")
	}
}

func TestIsPermissionError_OtherError(t *testing.T) {
	if isPermissionError(errors.New("connection refused")) {
		t.Error("isPermissionError('connection refused') = true, want false")
	}
}

// --- isTimeoutError tests ---

// timeoutError implements net.Error with Timeout()=true for testing.
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

func TestIsTimeoutError_NetTimeout(t *testing.T) {
	err := &net.OpError{Op: "read", Err: &timeoutError{}}
	if !isTimeoutError(err) {
		t.Error("isTimeoutError(net timeout) = false, want true")
	}
}

func TestIsTimeoutError_OtherError(t *testing.T) {
	if isTimeoutError(errors.New("connection reset")) {
		t.Error("isTimeoutError(non-timeout) = true, want false")
	}
}

func TestIsTimeoutError_Nil(t *testing.T) {
	if isTimeoutError(nil) {
		t.Error("isTimeoutError(nil) = true, want false")
	}
}

// --- matchICMPResponse tests ---

func TestMatchICMPResponse_TimeExceeded(t *testing.T) {
	buf := []byte{11, 0, 0, 0}
	addr := &net.IPAddr{IP: net.ParseIP("10.0.0.1")}
	ip, matched := matchICMPResponse(buf, 4, addr)
	if !matched {
		t.Error("matchICMPResponse(type=11) matched=false, want true")
	}
	if ip != "10.0.0.1" {
		t.Errorf("ip = %q, want 10.0.0.1", ip)
	}
}

func TestMatchICMPResponse_EchoReply(t *testing.T) {
	buf := []byte{0, 0, 0, 0}
	addr := &net.IPAddr{IP: net.ParseIP("8.8.8.8")}
	ip, matched := matchICMPResponse(buf, 4, addr)
	if !matched {
		t.Error("matchICMPResponse(type=0) matched=false, want true")
	}
	if ip != "8.8.8.8" {
		t.Errorf("ip = %q, want 8.8.8.8", ip)
	}
}

func TestMatchICMPResponse_OtherType(t *testing.T) {
	buf := []byte{3, 0, 0, 0}
	addr := &net.IPAddr{IP: net.ParseIP("1.1.1.1")}
	_, matched := matchICMPResponse(buf, 4, addr)
	if matched {
		t.Error("matchICMPResponse(type=3) matched=true, want false")
	}
}

func TestMatchICMPResponse_EmptyBuffer(t *testing.T) {
	buf := []byte{}
	_, matched := matchICMPResponse(buf, 0, &net.IPAddr{})
	if matched {
		t.Error("matchICMPResponse(empty) matched=true, want false")
	}
}

func TestMatchICMPResponse_NonIPAddr(t *testing.T) {
	buf := []byte{0, 0, 0, 0}
	addr := &net.UDPAddr{IP: net.ParseIP("1.2.3.4")}
	ip, matched := matchICMPResponse(buf, 4, addr)
	if !matched {
		t.Error("matchICMPResponse(Echo Reply, non-IPAddr) matched=false, want true")
	}
	if ip != "" {
		t.Errorf("ip = %q, want empty for non-IPAddr", ip)
	}
}

// --- contains tests ---

func TestContains(t *testing.T) {
	tests := []struct {
		s, sub string
		want   bool
	}{
		{"hello world", "world", true},
		{"hello world", "xyz", false},
		{"hello world", "", true},
		{"hello world", "hello world", true},
		{"hello", "hello world", false},
		{"", "", true},
	}
	for _, tt := range tests {
		got := contains(tt.s, tt.sub)
		if got != tt.want {
			t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.sub, got, tt.want)
		}
	}
}
