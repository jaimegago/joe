package traceroute

import (
	"context"
	"fmt"
	"net"
	"syscall"
	"time"
)

// icmpProber implements HopProber using raw ICMP sockets.
// Requires CAP_NET_RAW on Linux or root on macOS.
//
// Approach: send an ICMP Echo request to the destination with TTL=n.
// Intermediate routers that drop the packet due to TTL exhaustion send back
// an ICMP Time Exceeded message; the destination sends ICMP Echo Reply.
// We listen for either response on a raw ICMP socket.
type icmpProber struct{}

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
	conn, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		if isPermissionError(err) {
			return "", 0, fmt.Errorf("insufficient privileges for ICMP traceroute (requires root or CAP_NET_RAW): %v", err)
		}
		return "", 0, fmt.Errorf("open ICMP socket: %w", err)
	}
	defer conn.Close()

	// Set TTL on the underlying socket.
	rawConn, err := conn.(*net.IPConn).SyscallConn()
	if err != nil {
		return "", 0, fmt.Errorf("get raw conn: %w", err)
	}

	var setsockoptErr error
	ctrlErr := rawConn.Control(func(fd uintptr) {
		setsockoptErr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_TTL, ttl)
	})
	if ctrlErr != nil {
		return "", 0, fmt.Errorf("set TTL: %w", ctrlErr)
	}
	if setsockoptErr != nil {
		return "", 0, fmt.Errorf("setsockopt IP_TTL=%d: %w", ttl, setsockoptErr)
	}

	// Build ICMP Echo Request (type=8, code=0).
	icmpMsg := buildICMPEcho(ttl) // use TTL as identifier

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return "", 0, fmt.Errorf("set deadline: %w", err)
	}

	// Send probe.
	start := time.Now()
	_, err = conn.WriteTo(icmpMsg, &net.IPAddr{IP: dstIP})
	if err != nil {
		return "", 0, fmt.Errorf("send probe: %w", err)
	}

	// Listen for ICMP response.
	buf := make([]byte, 1500)
	for {
		if ctx.Err() != nil {
			return "", 0, ctx.Err()
		}

		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			// Deadline exceeded → timeout, no response.
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
	msg[0] = 8    // Type: Echo Request
	msg[1] = 0    // Code: 0
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
