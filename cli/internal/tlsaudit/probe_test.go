package tlsaudit

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const probeTimeout = 300 * time.Millisecond

func dialer(addr string) DialFunc {
	return func(ctx context.Context) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}
}

// serve hands every connection to a fresh local listener to handle.
func serve(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handle(c)
		}
	}()
	return ln.Addr().String()
}

func hostOf(url string) string {
	return strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
}

func expect(t *testing.T, got Verdict, detail string, want Verdict, contains string) {
	t.Helper()
	if got != want || !strings.Contains(detail, contains) {
		t.Errorf("got %s (%q), want %s with %q", got, detail, want, contains)
	}
}

func TestProbeTLSHandshake(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	defer srv.Close()
	v, d := ProbeTLS(context.Background(), dialer(hostOf(srv.URL)), probeTimeout)
	expect(t, v, d, VerdictTLS, "TLS 1.3")
}

func TestProbeTLSClientCertRequired(t *testing.T) {
	// TLS 1.2 makes the server refuse during the handshake, with an alert.
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, MaxVersion: tls.VersionTLS12}
	srv.StartTLS()
	defer srv.Close()
	v, d := ProbeTLS(context.Background(), dialer(hostOf(srv.URL)), probeTimeout)
	expect(t, v, d, VerdictTLS, "alert")
}

func TestProbeTLSPlainHTTP(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	v, d := ProbeTLS(context.Background(), dialer(hostOf(srv.URL)), probeTimeout)
	expect(t, v, d, VerdictPlain, "")
}

// An SSH server sends its banner before the hello: SSH encrypts by itself.
func TestProbeTLSSSHBanner(t *testing.T) {
	addr := serve(t, func(c net.Conn) {
		_, _ = c.Write([]byte("SSH-2.0-OpenSSH_9.6\r\n"))
		_, _ = io.Copy(io.Discard, c)
	})
	v, d := ProbeTLS(context.Background(), dialer(addr), probeTimeout)
	expect(t, v, d, VerdictSSH, "")
}

func TestProbeTLSSilent(t *testing.T) {
	addr := serve(t, func(c net.Conn) { _, _ = io.Copy(io.Discard, c) })
	v, d := ProbeTLS(context.Background(), dialer(addr), probeTimeout)
	expect(t, v, d, VerdictSilent, "")
}

func TestProbeTLSClosed(t *testing.T) {
	addr := serve(t, func(c net.Conn) { _ = c.Close() })
	v, d := ProbeTLS(context.Background(), dialer(addr), probeTimeout)
	expect(t, v, d, VerdictClosed, "")
}

func TestProbeTLSDialError(t *testing.T) {
	fail := func(context.Context) (net.Conn, error) { return nil, errors.New("no route to pod") }
	v, d := ProbeTLS(context.Background(), fail, probeTimeout)
	expect(t, v, d, VerdictUnknown, "no route to pod")
}

// pgServer answers an SSLRequest with sslReply and a StartupMessage with startupReply.
func pgServer(t *testing.T, sslReply byte, startupReply []byte) string {
	return serve(t, func(c net.Conn) {
		defer func() { _ = c.Close() }()
		head := make([]byte, 8)
		if _, err := io.ReadFull(c, head); err != nil {
			return
		}
		if binary.BigEndian.Uint32(head[4:]) == 80877103 {
			_, _ = c.Write([]byte{sslReply})
			return
		}
		rest := make([]byte, binary.BigEndian.Uint32(head[:4])-8)
		if _, err := io.ReadFull(c, rest); err != nil {
			return
		}
		_, _ = c.Write(startupReply)
	})
}

func pgMessage(kind byte, body []byte) []byte {
	msg := []byte{kind, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(msg[1:], uint32(len(body)+4))
	return append(msg, body...)
}

func pgError(code, text string) []byte {
	return pgMessage('E', []byte("SFATAL\x00C"+code+"\x00M"+text+"\x00\x00"))
}

func TestProbePostgresRefusesPlaintext(t *testing.T) {
	addr := pgServer(t, 'S', pgError("28000", `pg_hba.conf rejects connection for host "10.45.1.2", user "ohmlab-audit", database "ohmlab-audit", no encryption`))
	v, d := ProbePostgres(context.Background(), dialer(addr), probeTimeout)
	expect(t, v, d, VerdictTLS, "plaintext refused")
}

// Under a trust rule a plaintext session gets past pg_hba and fails on the
// unknown role, with the SQLSTATE of a refusal; only pg_hba names its file.
func TestProbePostgresTrustRuleAcceptsPlaintext(t *testing.T) {
	addr := pgServer(t, 'S', pgError("28000", `role "ohmlab-audit" does not exist`))
	v, d := ProbePostgres(context.Background(), dialer(addr), probeTimeout)
	expect(t, v, d, VerdictTLSOptional, "past pg_hba")
}

func TestProbePostgresAcceptsPlaintext(t *testing.T) {
	sasl := pgMessage('R', append([]byte{0, 0, 0, 10}, "SCRAM-SHA-256\x00\x00"...))
	addr := pgServer(t, 'S', sasl)
	v, d := ProbePostgres(context.Background(), dialer(addr), probeTimeout)
	expect(t, v, d, VerdictTLSOptional, "plaintext accepted")
}

func TestProbePostgresNoTLS(t *testing.T) {
	addr := pgServer(t, 'N', nil)
	v, d := ProbePostgres(context.Background(), dialer(addr), probeTimeout)
	expect(t, v, d, VerdictPlain, "no TLS")
}
