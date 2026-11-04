// Package tlsaudit finds out which in-cluster ports speak TLS.
package tlsaudit

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"
)

// Verdict is what a port answered.
type Verdict string

// The verdicts a probe returns.
const (
	VerdictTLS         Verdict = "tls"          // handshake, or a TLS alert
	VerdictTLSOptional Verdict = "tls-optional" // Postgres offering TLS but accepting plaintext
	VerdictSSH         Verdict = "ssh"          // an SSH banner: encrypted by SSH itself
	VerdictPlain       Verdict = "plain"        // answered without TLS
	VerdictSilent      Verdict = "silent"       // accepted, never answered (Redis, NFS)
	VerdictClosed      Verdict = "closed"       // closed without answering (PROXY protocol listeners)
	VerdictUnknown     Verdict = "unknown"      // not probed
)

// DialFunc opens a connection to the port under probe.
type DialFunc func(ctx context.Context) (net.Conn, error)

// countingConn counts the bytes read, which tells a silent peer from one that
// answered something the TLS client could not parse.
type countingConn struct {
	net.Conn
	read int
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.read += n
	return n, err
}

// ProbeTLS sends a TLS ClientHello and classifies the answer. It verifies
// nothing: the question is whether the port speaks TLS, not whom to trust.
func ProbeTLS(ctx context.Context, dial DialFunc, timeout time.Duration) (Verdict, string) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := dial(ctx)
	if err != nil {
		return VerdictUnknown, err.Error()
	}
	conn := &countingConn{Conn: raw}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	client := tls.Client(conn, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // probing, not trusting
	if err := client.HandshakeContext(ctx); err != nil {
		return classify(err, conn.read)
	}
	return VerdictTLS, describe(client.ConnectionState())
}

func classify(err error, read int) (Verdict, string) {
	var header tls.RecordHeaderError
	var op *net.OpError
	switch {
	case errors.As(err, &header) && bytes.HasPrefix(header.RecordHeader[:], []byte("SSH-")):
		return VerdictSSH, "SSH banner"
	case errors.As(err, &header):
		return VerdictPlain, "answered without TLS"
	case errors.As(err, &op) && op.Op == "remote error":
		return VerdictTLS, "TLS alert: " + op.Err.Error()
	case read == 0 && timedOut(err):
		return VerdictSilent, "no answer"
	case read == 0:
		return VerdictClosed, "closed without answering"
	}
	return VerdictUnknown, err.Error()
}

func timedOut(err error) bool {
	var ne net.Error
	return errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &ne) && ne.Timeout())
}

func describe(st tls.ConnectionState) string {
	d := tls.VersionName(st.Version)
	if len(st.PeerCertificates) > 0 {
		c := st.PeerCertificates[0]
		d += fmt.Sprintf(", %q issued by %q", c.Subject.CommonName, c.Issuer.CommonName)
	}
	return d
}

const (
	pgSSLRequest = 80877103
	pgProtocol3  = 196608
	pgUser       = "ohmlab-audit"
)

// ProbePostgres asks a Postgres server for TLS, then whether it would start a
// plaintext session (it answers that before any password, so no credentials
// are needed; the server logs the refused attempt).
func ProbePostgres(ctx context.Context, dial DialFunc, timeout time.Duration) (Verdict, string) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req := make([]byte, 8)
	binary.BigEndian.PutUint32(req[0:], 8)
	binary.BigEndian.PutUint32(req[4:], pgSSLRequest)
	answer, _, err := pgExchange(ctx, dial, timeout, req)
	if err != nil {
		return VerdictUnknown, err.Error()
	}
	switch answer {
	case 'N':
		return VerdictPlain, "no TLS offered"
	case 'S':
	default:
		return VerdictUnknown, fmt.Sprintf("unexpected answer %q to an SSLRequest", answer)
	}

	answer, body, err := pgExchange(ctx, dial, timeout, pgStartup())
	if err != nil {
		return VerdictUnknown, "TLS offered; plaintext attempt: " + err.Error()
	}
	switch answer {
	case 'R':
		return VerdictTLSOptional, "TLS offered, plaintext accepted"
	case 'E':
		code, message := pgField(body, 'C'), pgField(body, 'M')
		switch {
		// invalid_authorization_specification: a pg_hba refusal names its file
		// in every language; anything else came after pg_hba let it through.
		case code == "28000" && strings.Contains(message, "pg_hba.conf"):
			return VerdictTLS, "TLS offered, plaintext refused"
		case code == "28000":
			return VerdictTLSOptional, "TLS offered, plaintext past pg_hba: " + message
		}
		return VerdictUnknown, fmt.Sprintf("TLS offered; plaintext attempt: error %s: %s", code, message)
	}
	return VerdictUnknown, fmt.Sprintf("TLS offered; unexpected answer %q to a plaintext startup", answer)
}

// pgExchange sends msg on a new connection and reads the first byte of the
// answer and, for a message with a body (anything but the one-byte SSLRequest
// answers), its body.
func pgExchange(ctx context.Context, dial DialFunc, timeout time.Duration, msg []byte) (byte, []byte, error) {
	conn, err := dial(ctx)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(msg); err != nil {
		return 0, nil, err
	}
	kind := make([]byte, 1)
	if _, err := io.ReadFull(conn, kind); err != nil {
		return 0, nil, err
	}
	if kind[0] == 'S' || kind[0] == 'N' {
		return kind[0], nil, nil
	}
	size := make([]byte, 4)
	if _, err := io.ReadFull(conn, size); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(size)
	if n < 4 || n > 1<<16 {
		return 0, nil, fmt.Errorf("message of %d bytes", n)
	}
	body := make([]byte, n-4)
	if _, err := io.ReadFull(conn, body); err != nil {
		return 0, nil, err
	}
	return kind[0], body, nil
}

func pgStartup() []byte {
	params := "user\x00" + pgUser + "\x00database\x00" + pgUser + "\x00\x00"
	msg := make([]byte, 8, 8+len(params))
	binary.BigEndian.PutUint32(msg[0:], uint32(8+len(params)))
	binary.BigEndian.PutUint32(msg[4:], pgProtocol3)
	return append(msg, params...)
}

// pgField returns a field of an ErrorResponse body.
func pgField(body []byte, code byte) string {
	for _, f := range strings.Split(string(body), "\x00") {
		if len(f) > 1 && f[0] == code {
			return f[1:]
		}
	}
	return ""
}
