package secretgen

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ErrRefNotFound is what a RefFunc returns when the source secret or key does
// not exist or is empty. Any other error is a backend failure.
var ErrRefNotFound = errors.New("reference not found")

// RefFunc reads the value at keyPath (dot-separated) of the secret at path
// ("<mount>/<path>").
type RefFunc func(path, keyPath string) (string, error)

var (
	reRandom      = regexp.MustCompile(`<random:(\d+)>`)
	reHex         = regexp.MustCompile(`<hex:(\d+)>`)
	reUUID        = regexp.MustCompile(`<uuid>`)
	reAge         = regexp.MustCompile(`<age:(secret|public)>`)
	reRefParts    = regexp.MustCompile(`<ref:([^#>]+)#([^>]+)>`)
	reBcryptParts = regexp.MustCompile(`<bcrypt:([^>]+)>`)
	// Anything that looks like one of our placeholders, well-formed or not.
	reLooksLikeOurs = regexp.MustCompile(`<(?:random|hex|uuid|age|ref|bcrypt)\b[^>]*>`)
	reWellFormed    = regexp.MustCompile(`^(?:<random:\d+>|<hex:\d+>|<uuid>|<age:(?:secret|public)>|<ref:[^#>]+#[^>]+>|<bcrypt:[^>]+>)$`)
)

// Renderer replaces the placeholders of one secret's strings. The age key
// pair is shared by every <age:...> placeholder rendered with the same
// Renderer, which is what makes a secret key and its public key a pair.
type Renderer struct {
	// Age is the pair <age:secret> and <age:public> render. Left nil, the
	// first use generates one.
	Age *AgePair
	// Ref resolves <ref:...>. Left nil, a reference is an error.
	Ref RefFunc
}

// Render replaces every placeholder of s except <bcrypt:...>, which needs the
// finished secret (see BcryptTarget).
func (r *Renderer) Render(s string) (string, error) {
	var err error
	fail := func(e error) string {
		if err == nil {
			err = e
		}
		return ""
	}

	s = reRandom.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.Atoi(reRandom.FindStringSubmatch(m)[1])
		v, e := Random(n)
		if e != nil {
			return fail(fmt.Errorf("%s: %w", m, e))
		}
		return v
	})
	s = reHex.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.Atoi(reHex.FindStringSubmatch(m)[1])
		v, e := Hex(n)
		if e != nil {
			return fail(fmt.Errorf("%s: %w", m, e))
		}
		return v
	})
	s = reUUID.ReplaceAllStringFunc(s, func(string) string { return UUID() })
	if err != nil {
		return "", err
	}

	if reAge.MatchString(s) {
		if r.Age == nil {
			p, e := NewAgePair()
			if e != nil {
				return "", e
			}
			r.Age = &p
		}
		s = reAge.ReplaceAllStringFunc(s, func(m string) string {
			if reAge.FindStringSubmatch(m)[1] == "secret" {
				return r.Age.Secret
			}
			return r.Age.Public
		})
	}

	s = reRefParts.ReplaceAllStringFunc(s, func(m string) string {
		sub := reRefParts.FindStringSubmatch(m)
		if r.Ref == nil {
			return fail(fmt.Errorf("%s: no reference resolver configured", m))
		}
		v, e := r.Ref(sub[1], sub[2])
		if e != nil {
			return fail(fmt.Errorf("%s: %w", m, e))
		}
		return v
	})
	if err != nil {
		return "", err
	}
	return s, nil
}

// BcryptTarget returns the key path a <bcrypt:key.path> string hashes.
func BcryptTarget(s string) (string, bool) {
	m := reBcryptParts.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// RefSources returns the "<mount>/<path>" of every reference s holds.
func RefSources(s string) []string {
	var out []string
	for _, m := range reRefParts.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

// Malformed returns the placeholder-looking fragments of s that no template
// understands, such as <random:abc> or <ref:path-without-key>.
func Malformed(s string) []string {
	var out []string
	for _, m := range reLooksLikeOurs.FindAllString(s, -1) {
		if !reWellFormed.MatchString(m) {
			out = append(out, m)
		}
	}
	return out
}

// Problems lists what is wrong with the placeholders of s, without generating
// anything: malformed ones, lengths out of range, an odd hex length.
func Problems(s string) []string {
	var out []string
	for _, m := range Malformed(s) {
		out = append(out, "malformed placeholder "+m)
	}
	for _, m := range reRandom.FindAllStringSubmatch(s, -1) {
		if n, err := strconv.Atoi(m[1]); err != nil || n < 1 || n > MaxLength {
			out = append(out, fmt.Sprintf("%s: the length must be between 1 and %d", m[0], MaxLength))
		}
	}
	for _, m := range reHex.FindAllStringSubmatch(s, -1) {
		if n, err := strconv.Atoi(m[1]); err != nil || n < 1 || n > MaxLength || n%2 != 0 {
			out = append(out, fmt.Sprintf("%s: the length must be even and between 2 and %d", m[0], MaxLength))
		}
	}
	return out
}

// NavigateJSON follows a dot-separated key path through nested maps and
// returns the leaf as text. A missing path gives "".
func NavigateJSON(data map[string]any, dotPath string) string {
	var cur any = data
	for _, key := range strings.Split(dotPath, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		if cur, ok = m[key]; !ok {
			return ""
		}
	}
	switch v := cur.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return fmt.Sprintf("%v", v)
	case bool:
		return fmt.Sprintf("%v", v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
