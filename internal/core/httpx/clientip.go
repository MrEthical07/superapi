package httpx

import (
	"net"
	"net/http"
	"strings"

	"github.com/MrEthical07/superapi/internal/core/config"
	"github.com/MrEthical07/superapi/internal/core/netx"
)

// ClientIP resolves the client IP address and stores it in the request context.
// If TrustedProxies is empty, only r.RemoteAddr is used.
func ClientIP(cfg config.ClientIPConfig) func(http.Handler) http.Handler {
	trusted := parseTrustedProxies(cfg.TrustedProxies)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := remoteIP(r.RemoteAddr)
			if isTrustedProxy(clientIP, trusted) {
				if forwarded := parseForwardedHeader(r.Header.Get("Forwarded")); forwarded != "" {
					clientIP = forwarded
				} else if xff := parseXForwardedFor(r.Header.Get("X-Forwarded-For")); xff != "" {
					clientIP = xff
				} else if xri := parseIPLiteral(r.Header.Get("X-Real-IP")); xri != "" {
					clientIP = xri
				}
			}

			ctx := netx.WithClientIP(r.Context(), clientIP)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func parseTrustedProxies(items []string) []*net.IPNet {
	if len(items) == 0 {
		return nil
	}

	out := make([]*net.IPNet, 0, len(items))
	for _, raw := range items {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "/") {
			_, cidr, err := net.ParseCIDR(trimmed)
			if err != nil {
				continue
			}
			out = append(out, cidr)
			continue
		}
		ip := net.ParseIP(trimmed)
		if ip == nil {
			continue
		}
		bits := 32
		if ip.To4() == nil {
			bits = 128
		}
		out = append(out, &net.IPNet{
			IP:   ip,
			Mask: net.CIDRMask(bits, bits),
		})
	}
	return out
}

func isTrustedProxy(remote string, trusted []*net.IPNet) bool {
	if len(trusted) == 0 {
		return false
	}
	ip := net.ParseIP(strings.TrimSpace(remote))
	if ip == nil {
		return false
	}
	for _, n := range trusted {
		if n != nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

func parseForwardedHeader(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}

	parts := strings.Split(header, ",")
	for _, part := range parts {
		section := strings.TrimSpace(part)
		if section == "" {
			continue
		}
		directives := strings.Split(section, ";")
		for _, dir := range directives {
			dir = strings.TrimSpace(dir)
			if len(dir) < 4 || !strings.EqualFold(dir[:4], "for=") {
				continue
			}
			value := strings.TrimSpace(dir[4:])
			if ip := parseIPLiteral(value); ip != "" {
				return ip
			}
		}
	}
	return ""
}

func parseXForwardedFor(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	parts := strings.Split(header, ",")
	if len(parts) == 0 {
		return ""
	}
	return parseIPLiteral(parts[0])
}

func parseIPLiteral(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	value = strings.Trim(value, "\"")
	value = strings.TrimPrefix(value, "[")
	value = strings.TrimSuffix(value, "]")

	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}

	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return ""
	}
	return ip.String()
}
