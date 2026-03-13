package traceroute

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"
)

// packetSender is the subset of net.PacketConn used by probeWithConn.
// Abstracted to allow injection in tests.
type packetSender interface {
	SetDeadline(t time.Time) error
	WriteTo(b []byte, addr net.Addr) (int, error)
	ReadFrom(b []byte) (int, net.Addr, error)
	Close() error
}

// ttlSetter sets the IP TTL on a raw socket. Abstracted for testing.
type ttlSetter interface {
	SetTTL(ttl int) error
}

// icmpProber implements HopProber using raw ICMP sockets.
// Requires CAP_NET_RAW on Linux or root on macOS.
//
// Approach: send an ICMP Echo request to the destination with TTL=n.
// Intermediate routers that drop the packet due to TTL exhaustion send back
// an ICMP Time Exceeded message; the destination sends ICMP Echo Reply.
// We listen for either response on a raw ICMP socket.
type icmpProber struct {
	// listenPacket is the factory used to open an ICMP packet connection.
	// Defaults to net.ListenPacket; overridden in tests.
	listenPacket func(network, address string) (net.PacketConn, error)

	// setTTL sets the IP TTL on an open conn. Defaults to syscallTTLSetter.SetTTL;
	// overridden in tests to inject errors without a real *net.IPConn.
	setTTL func(conn net.PacketConn, ttl int) error
}

func (p *icmpProber) openConn() (net.PacketConn, error) {
	fn := p.listenPacket
	if fn == nil {
		fn = net.ListenPacket
	}
	return fn("ip4:icmp", "0.0.0.0")
}

// syscallTTLSetter wraps a *net.IPConn and sets IP_TTL via setsockopt.
type syscallTTLSetter struct{ conn *net.IPConn }

func (s *syscallTTLSetter) SetTTL(ttl int) error {
	rawConn, err := s.conn.SyscallConn()
	if err != nil {
		return fmt.Errorf("get raw conn: %w", err)
	}
	var setsockoptErr error
	ctrlErr := rawConn.Control(func(fd uintptr) {
		setsockoptErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_TTL, ttl)
	})
	if ctrlErr != nil {
		return fmt.Errorf("set TTL ctrl: %w", ctrlErr)
	}
	if setsockoptErr != nil {
		return fmt.Errorf("setsockopt IP_TTL=%d: %w", ttl, setsockoptErr)
	}
	return nil
}

// ProbeHop sends a probe toward dst with the given TTL and returns the
// IP address that responded, the round-trip latency, and any error.
//
// This implementation uses raw sockets via net.ListenPacket("ip4:icmp").
// If the process lacks the required privileges, each call returns an error
// with a clear message explaining the limitation.
func (p *icmpProber) ProbeHop(ctx context.Context, dst string, ttl int, timeout time.Duration) (string, float64, error) {
	// Resolve destination.
	dstIP := net.ParseIP(resolveHost(dst)).To4()
	if dstIP == nil {
		return "", 0, fmt.Errorf("cannot resolve %q to IPv4 address", dst)
	}

	// Open a raw ICMP socket to listen for responses.
	// This will fail with EPERM if the process lacks privileges.
	conn, err := p.openConn()
	if err != nil {
		if isPermissionError(err) {
			return "", 0, fmt.Errorf("insufficient privileges for ICMP traceroute (requires root or CAP_NET_RAW): %v", err)
		}
		return "", 0, fmt.Errorf("open ICMP socket: %w", err)
	}
	defer conn.Close()

	// Set TTL — use injected function if provided, otherwise fall back to the real
	// syscall setter which requires conn to be a *net.IPConn.
	if p.setTTL != nil {
		if err := p.setTTL(conn, ttl); err != nil {
			return "", 0, err
		}
	} else {
		ipConn, ok := conn.(*net.IPConn)
		if !ok {
			return "", 0, fmt.Errorf("unexpected conn type %T", conn)
		}
		if err := (&syscallTTLSetter{conn: ipConn}).SetTTL(ttl); err != nil {
			return "", 0, err
		}
	}

	return probeWithConn(ctx, conn, dstIP, ttl, timeout)
}

// probeWithConn performs the send/receive loop given an already-open connection
// with TTL already set. Separated for testability.
func probeWithConn(ctx context.Context, conn packetSender, dstIP net.IP, ttl int, timeout time.Duration) (string, float64, error) {
	icmpMsg := buildICMPEcho(ttl)

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return "", 0, fmt.Errorf("set deadline: %w", err)
	}

	start := time.Now()
	if _, err := conn.WriteTo(icmpMsg, &net.IPAddr{IP: dstIP}); err != nil {
		return "", 0, fmt.Errorf("send probe: %w", err)
	}

	buf := make([]byte, 1500)
	for {
		if ctx.Err() != nil {
			return "", 0, ctx.Err()
		}

		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			if isTimeoutError(err) {
				return "", 0, nil
			}
			return "", 0, fmt.Errorf("read ICMP response: %w", err)
		}
		latencyMS := float64(time.Since(start).Microseconds()) / 1000.0

		if ip, ok := matchICMPResponse(buf, n, addr); ok {
			return ip, latencyMS, nil
		}
	}
}

// matchICMPResponse returns the responder's IP if the buffer contains a
// relevant ICMP message (Time Exceeded type=11 or Echo Reply type=0).
// Extracted for testability.
func matchICMPResponse(buf []byte, n int, addr net.Addr) (ip string, matched bool) {
	if n < 1 {
		return "", false
	}
	icmpType := buf[0]
	// ICMP Time Exceeded (type=11) or Echo Reply (type=0).
	if icmpType != 11 && icmpType != 0 {
		return "", false
	}
	if ipAddr, ok := addr.(*net.IPAddr); ok {
		return ipAddr.IP.String(), true
	}
	return "", true
}

// buildICMPEcho constructs a minimal ICMP Echo Request message.
func buildICMPEcho(id int) []byte {
	msg := make([]byte, 8)
	msg[0] = 8 // Type: Echo Request
	msg[1] = 0 // Code: 0
	msg[4] = byte(id >> 8)
	msg[5] = byte(id)
	msg[6] = 0 // Sequence high
	msg[7] = 1 // Sequence low
	// Compute checksum.
	cs := icmpChecksum(msg)
	msg[2] = byte(cs >> 8)
	msg[3] = byte(cs)
	return msg
}

// icmpChecksum computes the Internet checksum for an ICMP message.
func icmpChecksum(msg []byte) uint16 {
	sum := 0
	for i := 0; i+1 < len(msg); i += 2 {
		sum += int(msg[i])<<8 | int(msg[i+1])
	}
	if len(msg)%2 == 1 {
		sum += int(msg[len(msg)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// isPermissionError returns true for EPERM / EACCES errors.
func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	e := err.Error()
	return contains(e, "permission denied") || contains(e, "operation not permitted")
}

// isTimeoutError returns true for network timeout errors.
func isTimeoutError(err error) bool {
	if netErr, ok := err.(net.Error); ok {
		return netErr.Timeout()
	}
	return false
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
