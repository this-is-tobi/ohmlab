package storage

import "crypto/rand"

const (
	accessKeyAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	secretKeyAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	accessKeyLength   = 20
	secretKeyLength   = 40
)

// newKeyPair generates a service account's keys: 20 characters [A-Z0-9] and
// 40 characters [A-Za-z0-9].
func newKeyPair() (accessKey, secretKey string, err error) {
	if accessKey, err = randomString(accessKeyLength, accessKeyAlphabet); err != nil {
		return "", "", err
	}
	if secretKey, err = randomString(secretKeyLength, secretKeyAlphabet); err != nil {
		return "", "", err
	}
	return accessKey, secretKey, nil
}

// randomString draws n characters of alphabet from crypto/rand. A byte at or
// past the largest multiple of len(alphabet) is rejected, so no character is
// likelier than another.
func randomString(n int, alphabet string) (string, error) {
	limit := 256 - 256%len(alphabet)
	out := make([]byte, 0, n)
	buf := make([]byte, 2*n)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if int(b) >= limit {
				continue
			}
			out = append(out, alphabet[int(b)%len(alphabet)])
			if len(out) == n {
				break
			}
		}
	}
	return string(out), nil
}
