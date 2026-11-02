// Package secretgen generates the secret material a template placeholder asks
// for and classifies template strings. It is the single implementation behind
// both the CLI engine and the controller.
package secretgen

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"regexp"

	"filippo.io/age"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// MaxLength bounds <random:N> and <hex:N>, so a template cannot ask for an
// allocation that hurts the process that renders it.
const MaxLength = 4096

const alphanumeric = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

var (
	reGenerated = regexp.MustCompile(`<random:\d+>|<hex:\d+>|<uuid>|<age:(?:secret|public)>`)
	reRef       = regexp.MustCompile(`<ref:[^#]+#[^>]+>`)
	reBcrypt    = regexp.MustCompile(`<bcrypt:[^>]+>`)
)

// Kind says how a template string behaves across reconciles.
type Kind int

const (
	// KindLiteral has no placeholder: the declared text is only a default, an
	// existing value always wins.
	KindLiteral Kind = iota
	// KindGenerated holds a random, hex, uuid or age placeholder: made once, then
	// kept. A string mixing generated placeholders with references is
	// KindGenerated too: its reference part is resolved when the value is made.
	KindGenerated
	// KindRef holds only reference placeholders: the value is a copy of its
	// source and follows it.
	KindRef
	// KindBcrypt is a hash of another field of the same secret.
	KindBcrypt
)

// Classify reports the Kind of one string leaf.
func Classify(s string) Kind {
	switch {
	case reGenerated.MatchString(s):
		return KindGenerated
	case reBcrypt.MatchString(s):
		return KindBcrypt
	case reRef.MatchString(s):
		return KindRef
	default:
		return KindLiteral
	}
}

// Random returns a cryptographically random alphanumeric string of length n.
func Random(n int) (string, error) {
	if n <= 0 || n > MaxLength {
		return "", fmt.Errorf("length must be between 1 and %d, got %d", MaxLength, n)
	}
	out := make([]byte, n)
	max := big.NewInt(int64(len(alphanumeric)))
	for i := range out {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("crypto/rand: %w", err)
		}
		out[i] = alphanumeric[v.Int64()]
	}
	return string(out), nil
}

// Hex returns a cryptographically random lowercase hex string of n characters.
// Consumers that need N random bytes (an AES key, for instance) ask for 2N.
func Hex(n int) (string, error) {
	if n <= 0 || n > MaxLength {
		return "", fmt.Errorf("length must be between 1 and %d, got %d", MaxLength, n)
	}
	if n%2 != 0 {
		return "", fmt.Errorf("length must be even, got %d", n)
	}
	buf := make([]byte, n/2)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("crypto/rand: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// UUID returns a random UUID v4.
func UUID() string { return uuid.New().String() }

// AgePair is an age X25519 key pair in its text form.
type AgePair struct {
	Secret string
	Public string
}

// NewAgePair generates a fresh key pair.
func NewAgePair() (AgePair, error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return AgePair{}, fmt.Errorf("generate age keypair: %w", err)
	}
	return AgePair{Secret: id.String(), Public: id.Recipient().String()}, nil
}

// AgePairFromSecret rebuilds the pair an existing secret key belongs to.
func AgePairFromSecret(secret string) (AgePair, error) {
	id, err := age.ParseX25519Identity(secret)
	if err != nil {
		return AgePair{}, fmt.Errorf("parse age secret key: %w", err)
	}
	return AgePair{Secret: id.String(), Public: id.Recipient().String()}, nil
}

// Bcrypt returns the bcrypt hash of plain at the default cost.
func Bcrypt(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt hash: %w", err)
	}
	return string(h), nil
}

// BcryptMatches reports whether hash is a bcrypt hash of plain.
func BcryptMatches(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
