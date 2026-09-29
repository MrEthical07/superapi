package notify

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// capturedMessage is one message accepted by the fake server.
type capturedMessage struct {
	from     string
	rcpts    []string
	data     string
	authUser string
	authPass string
	viaTLS   bool
}

// fakeSMTP is a minimal in-process SMTP server: enough of RFC 5321 (EHLO,
// STARTTLS, AUTH PLAIN, MAIL, RCPT, DATA, QUIT) to exercise a real client.
type fakeSMTP struct {
	t  *testing.T
	ln net.Listener

	// Behaviour, set before the first connection.
	tlsConfig     *tls.Config // enables STARTTLS (or implicit TLS when implicit)
	implicit      bool
	advertiseAuth bool
	rejectRcpt    string        // RCPT TO for this address gets a 550 that echoes it
	stallAfterHi  time.Duration // sleep after the greeting, to simulate a hung server

	mu          sync.Mutex
	messages    []capturedMessage
	commands    []string
	connections int
}

func startFakeSMTP(t *testing.T, configure func(*fakeSMTP)) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeSMTP{t: t, ln: ln}
	if configure != nil {
		configure(s)
	}
	if s.implicit && s.tlsConfig != nil {
		s.ln = tls.NewListener(ln, s.tlsConfig)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go s.serve()
	return s
}

func (s *fakeSMTP) host() string { return "127.0.0.1" }

func (s *fakeSMTP) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *fakeSMTP) received() []capturedMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]capturedMessage(nil), s.messages...)
}

func (s *fakeSMTP) sawCommand(prefix string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.commands {
		if strings.HasPrefix(strings.ToUpper(c), strings.ToUpper(prefix)) {
			return true
		}
	}
	return false
}

func (s *fakeSMTP) connectionCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connections
}

func (s *fakeSMTP) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.connections++
		s.mu.Unlock()
		go s.handle(conn)
	}
}

func (s *fakeSMTP) handle(conn net.Conn) {
	defer conn.Close()
	_, viaTLS := conn.(*tls.Conn)
	r := bufio.NewReader(conn)
	w := func(format string, args ...any) { fmt.Fprintf(conn, format+"\r\n", args...) }

	w("220 fake.example ESMTP ready")
	if s.stallAfterHi > 0 {
		time.Sleep(s.stallAfterHi)
	}

	var cur capturedMessage
	cur.viaTLS = viaTLS
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		s.mu.Lock()
		s.commands = append(s.commands, line)
		s.mu.Unlock()

		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			w("250-fake.example")
			w("250-8BITMIME")
			if s.tlsConfig != nil && !s.implicit && !viaTLS {
				w("250-STARTTLS")
			}
			if s.advertiseAuth {
				w("250-AUTH PLAIN")
			}
			w("250 OK")
		case upper == "STARTTLS":
			if s.tlsConfig == nil || viaTLS {
				w("503 STARTTLS not available")
				continue
			}
			w("220 ready to start TLS")
			tlsConn := tls.Server(conn, s.tlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			r = bufio.NewReader(conn)
			w = func(format string, args ...any) { fmt.Fprintf(conn, format+"\r\n", args...) }
			viaTLS = true
			cur = capturedMessage{viaTLS: true}
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("AUTH PLAIN"):]))
			if err != nil {
				w("501 bad auth")
				continue
			}
			parts := strings.Split(string(raw), "\x00")
			if len(parts) == 3 {
				cur.authUser, cur.authPass = parts[1], parts[2]
			}
			w("235 2.7.0 authenticated")
		case strings.HasPrefix(upper, "MAIL FROM:"):
			cur.from = angleAddress(line[len("MAIL FROM:"):])
			w("250 OK")
		case strings.HasPrefix(upper, "RCPT TO:"):
			rcpt := angleAddress(line[len("RCPT TO:"):])
			if s.rejectRcpt != "" && strings.EqualFold(rcpt, s.rejectRcpt) {
				w("550 5.1.1 <%s>: Recipient address rejected: User unknown", rcpt)
				continue
			}
			cur.rcpts = append(cur.rcpts, rcpt)
			w("250 OK")
		case upper == "DATA":
			w("354 end data with <CR><LF>.<CR><LF>")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, "."))
			}
			cur.data = b.String()
			s.mu.Lock()
			s.messages = append(s.messages, cur)
			s.mu.Unlock()
			cur = capturedMessage{viaTLS: viaTLS, authUser: cur.authUser, authPass: cur.authPass}
			w("250 OK queued")
		case upper == "QUIT":
			w("221 bye")
			return
		case upper == "RSET", upper == "NOOP":
			w("250 OK")
		default:
			w("502 command not implemented")
		}
	}
}

// angleAddress extracts the address between < and > in a MAIL/RCPT argument,
// ignoring ESMTP parameters such as BODY=8BITMIME.
func angleAddress(arg string) string {
	arg = strings.TrimSpace(arg)
	if i := strings.Index(arg, "<"); i >= 0 {
		if j := strings.Index(arg[i:], ">"); j > 0 {
			return arg[i+1 : i+j]
		}
	}
	return arg
}

// selfSignedTLS returns a server TLS config for 127.0.0.1/localhost and a
// client pool that trusts it.
func selfSignedTLS(t *testing.T) (*tls.Config, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "fake.example"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS12,
	}, pool
}
