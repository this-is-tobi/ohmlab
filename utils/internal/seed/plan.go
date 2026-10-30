// Package seed decides what a VaultSeed changes in Vault. It is a pure planner:
// it reads nothing and writes nothing, it turns "what the CR declares" and "what
// the path holds" into "what to write", so every rule of the seeding semantics
// is a plain function that tests can pin.
package seed

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/this-is-tobi/ohmlab/utils/internal/secretgen"
)

// Markers the controller keeps in the custom_metadata of a path.
const (
	OwnerKey         = "ohmlab.fr/owner"
	RotationTokenKey = "ohmlab.fr/rotation-token"
)

// Reason says why a path is not written. The reconciler turns it into a
// condition reason.
type Reason string

const (
	ReasonOwnedByAnother  Reason = "OwnedByAnother"
	ReasonRefPending      Reason = "RefPending"
	ReasonInvalidSpec     Reason = "InvalidSpec"
	ReasonInvalidRotation Reason = "InvalidRotation"
)

// Action labels a change for events and the planned-changes metric.
type Action string

const (
	ActionAdopt       Action = "adopt"
	ActionAddKey      Action = "add-key"
	ActionUpdateField Action = "update-field"
	ActionRotate      Action = "rotate"
	ActionPrune       Action = "prune"
)

// Change is one key-level decision. Key is a dot-separated path of key names,
// never a value.
type Change struct {
	Action Action
	Key    string
}

// Rotate asks for one rotation. It happens once per distinct Token.
type Rotate struct {
	Token string
	// Keys are dot paths of generated keys. Empty means every generated key.
	Keys []string
}

// Spec is what a VaultSeed declares.
type Spec struct {
	// Owner is "<namespace>/<name>" of the CR.
	Owner  string
	Data   map[string]any
	Rotate *Rotate
	Prune  bool
	// Managed is status.managedKeys: the keys this controller wrote.
	Managed []string
}

// Observed is what the path holds now.
type Observed struct {
	// Exists is false when the path was never written or its latest version is
	// deleted.
	Exists bool
	// Version is the compare-and-set value for the next write: the latest
	// version number, 0 when the path never existed. A deleted latest version
	// still counts.
	Version  int64
	Data     map[string]any
	Metadata map[string]string
}

// Refs resolves <ref:...> sources.
type Refs interface {
	// Lookup returns the value at keyPath of the secret at path. It returns
	// secretgen.ErrRefNotFound when the secret or the key is missing or empty;
	// any other error is a backend failure.
	Lookup(ctx context.Context, path, keyPath string) (string, error)
}

// Input is everything Compute reads.
type Input struct {
	Spec     Spec
	Observed Observed
	Refs     Refs
}

// Plan is the outcome. A Plan with Blocked set writes nothing.
type Plan struct {
	Blocked Reason
	Message string

	// WriteData: write Data to the path, compare-and-set on CAS.
	WriteData bool
	Data      map[string]any
	CAS       int64

	// WriteMetadata: set custom_metadata to Metadata. Done after the data, and
	// only for a path that exists once the data is written.
	WriteMetadata bool
	Metadata      map[string]string

	Changes []Change
	// Managed is the new status.managedKeys, sorted.
	Managed []string
}

// Empty reports whether the plan changes nothing in Vault.
func (p Plan) Empty() bool { return !p.WriteData && !p.WriteMetadata }

// Compute plans one path. The error is a backend failure while resolving a
// reference; every semantic outcome is in the Plan.
func Compute(ctx context.Context, in Input) (Plan, error) {
	w := &walker{ctx: ctx, in: in}
	return w.run()
}

type walker struct {
	ctx context.Context
	in  Input

	r        secretgen.Renderer
	newPair  bool // a fresh age pair replaces whatever the age keys hold
	pairRot  bool // the rotation in force rotates the age pair
	rotating bool
	rotKeys  map[string]bool // nil with rotating: every generated key

	changes  []Change
	written  map[string]bool // leaf keys this plan writes
	bcrypts  []bcryptLeaf
	refCache map[string]string

	blocked Reason
	message string
	err     error
}

type bcryptLeaf struct {
	key, target string
	existing    any
	has         bool
	name        string
}

func (w *walker) block(r Reason, format string, args ...any) {
	if w.blocked == "" {
		w.blocked, w.message = r, fmt.Sprintf(format, args...)
	}
}

func (w *walker) run() (Plan, error) {
	spec, obs := w.in.Spec, w.in.Observed
	w.written = map[string]bool{}
	w.refCache = map[string]string{}

	if owner := obs.Metadata[OwnerKey]; owner != "" && owner != spec.Owner {
		return Plan{Blocked: ReasonOwnedByAnother, Message: "the path is owned by " + owner}, nil
	}

	w.r.Ref = w.lookup
	generated := generatedLeaves(spec.Data)
	if rot := spec.Rotate; rot != nil && obs.Metadata[RotationTokenKey] != rot.Token {
		w.rotating = true
		if len(rot.Keys) > 0 {
			w.rotKeys = map[string]bool{}
			for _, k := range rot.Keys {
				if !generated[k] {
					return Plan{Blocked: ReasonInvalidRotation, Message: fmt.Sprintf("rotate.keys names %q, which is not a generated key of this path", k)}, nil
				}
				w.rotKeys[k] = true
			}
		}
	}
	w.choosePair(generated)

	out := w.merge(spec.Data, obs.Data, "")
	w.finishBcrypt(out)
	pruned := w.prune(out)

	if w.err != nil {
		return Plan{}, w.err
	}
	if w.blocked != "" {
		return Plan{Blocked: w.blocked, Message: w.message}, nil
	}

	plan := Plan{CAS: obs.Version, Changes: w.changes}
	plan.WriteData = len(w.changes) > 0
	if plan.WriteData {
		plan.Data = out
	}

	md := copyStrings(obs.Metadata)
	dirty := false
	if md[OwnerKey] == "" {
		md[OwnerKey] = spec.Owner
		dirty = true
		if obs.Exists {
			plan.Changes = append(plan.Changes, Change{Action: ActionAdopt})
		}
	}
	if w.rotating && md[RotationTokenKey] != spec.Rotate.Token {
		md[RotationTokenKey] = spec.Rotate.Token
		dirty = true
	}
	if dirty && (obs.Exists || plan.WriteData) {
		plan.WriteMetadata, plan.Metadata = true, md
	}

	plan.Managed = w.managed(out, pruned)
	return plan, nil
}

// lookup resolves one reference, once per plan.
func (w *walker) lookup(path, keyPath string) (string, error) {
	id := path + "#" + keyPath
	if v, ok := w.refCache[id]; ok {
		return v, nil
	}
	v, err := w.in.Refs.Lookup(w.ctx, path, keyPath)
	if err != nil {
		if errors.Is(err, secretgen.ErrRefNotFound) {
			return "", err
		}
		return "", backendError{err}
	}
	w.refCache[id] = v
	return v, nil
}

// render renders one template and files a failure under the right reason.
func (w *walker) render(tmpl, key string) (string, bool) {
	v, err := w.r.Render(tmpl)
	switch {
	case err == nil:
		return v, true
	case errors.Is(err, secretgen.ErrRefNotFound):
		w.block(ReasonRefPending, "key %q waits for a reference: %v", key, err)
	case isBackendFailure(err):
		if w.err == nil {
			w.err = err
		}
	default:
		w.block(ReasonInvalidSpec, "key %q: %v", key, err)
	}
	return "", false
}

// backendError marks a failure of Refs.Lookup that is not "not found".
type backendError struct{ error }

func (e backendError) Unwrap() error { return e.error }

func isBackendFailure(err error) bool {
	var be backendError
	return errors.As(err, &be)
}

// choosePair settles which age key pair the <age:...> keys of the path use.
// An existing secret key keeps its pair, so a deleted public key comes back as
// that pair's public key; otherwise a new pair replaces both keys.
func (w *walker) choosePair(generated map[string]bool) {
	secrets, publics := ageLeaves(w.in.Spec.Data)
	if len(secrets)+len(publics) == 0 {
		return
	}
	pairRotates := false
	if w.rotating {
		pairRotates = w.rotKeys == nil
		for _, k := range append(append([]string{}, secrets...), publics...) {
			if w.rotKeys[k] {
				pairRotates = true
			}
		}
	}
	w.pairRot = pairRotates
	if !pairRotates {
		for _, k := range secrets {
			if s, ok := stringAt(w.in.Observed.Data, k); ok {
				if p, err := secretgen.AgePairFromSecret(s); err == nil {
					w.r.Age = &p
					return
				}
			}
		}
	}
	// No usable pair: a new one is needed when any age key is missing or rotates.
	need := pairRotates
	for _, k := range append(append([]string{}, secrets...), publics...) {
		if _, ok := stringAt(w.in.Observed.Data, k); !ok {
			need = true
		}
	}
	if need {
		p, err := secretgen.NewAgePair()
		if err != nil {
			w.block(ReasonInvalidSpec, "%v", err)
			return
		}
		w.r.Age = &p
		w.newPair = true
	}
}

func (w *walker) record(a Action, key string) {
	w.changes = append(w.changes, Change{Action: a, Key: key})
	w.written[key] = true
}

// merge returns observed with the declared keys applied.
func (w *walker) merge(desired, observed map[string]any, prefix string) map[string]any {
	out := copyMap(observed)
	for _, k := range sortedKeys(desired) {
		key := join(prefix, k)
		dv := desired[k]
		ov, has := out[k]
		switch d := dv.(type) {
		case map[string]any:
			if !has {
				out[k] = w.materialize(d, key)
			} else if om, ok := ov.(map[string]any); ok {
				out[k] = w.merge(d, om, key)
			}
			// A scalar where a map is declared stays as it is.
		case string:
			if v, set := w.leaf(k, d, ov, has, key); set {
				out[k] = v
			}
		default:
			if !has {
				out[k] = w.materialize(dv, key)
			}
		}
	}
	return out
}

// materialize builds a value that does not exist yet.
func (w *walker) materialize(v any, key string) any {
	switch d := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(d))
		for _, k := range sortedKeys(d) {
			out[k] = w.materialize(d[k], join(key, k))
		}
		return out
	case []any:
		out := make([]any, len(d))
		for i, item := range d {
			if s, ok := item.(string); ok {
				if r, ok := w.render(s, key); ok {
					out[i] = r
				}
			} else {
				out[i] = deepCopy(item)
			}
		}
		w.record(ActionAddKey, key)
		return out
	case string:
		return w.newLeaf(d, key, ActionAddKey)
	default:
		w.record(ActionAddKey, key)
		return deepCopy(v)
	}
}

// newLeaf computes the value of a string leaf that is written now.
func (w *walker) newLeaf(tmpl, key string, action Action) any {
	if secretgen.Classify(tmpl) == secretgen.KindBcrypt {
		// Hashed once the whole secret is known.
		w.bcrypts = append(w.bcrypts, bcryptLeaf{key: key, target: mustTarget(tmpl), name: lastSegment(key)})
		w.record(action, key)
		return tmpl
	}
	v, ok := w.render(tmpl, key)
	if !ok {
		return nil
	}
	w.record(action, key)
	return v
}

// leaf applies one declared string. It returns the value to put at name and
// whether to put it.
func (w *walker) leaf(name, tmpl string, ov any, has bool, key string) (any, bool) {
	kind := secretgen.Classify(tmpl)
	switch {
	case kind == secretgen.KindBcrypt:
		w.bcrypts = append(w.bcrypts, bcryptLeaf{key: key, target: mustTarget(tmpl), existing: ov, has: has, name: name})
		return nil, false
	case !has:
		return w.newLeaf(tmpl, key, ActionAddKey), true
	case kind == secretgen.KindGenerated && w.rotates(tmpl, key):
		return w.newLeaf(tmpl, key, ActionRotate), true
	case kind == secretgen.KindGenerated && w.newPair && hasAge(tmpl):
		return w.newLeaf(tmpl, key, ActionRotate), true
	case kind == secretgen.KindRef:
		want, ok := w.render(tmpl, key)
		if !ok {
			return nil, false
		}
		if cur, isStr := ov.(string); isStr && cur == want {
			return nil, false
		}
		w.record(ActionUpdateField, key)
		return want, true
	}
	return nil, false
}

// rotates reports whether the rotation in force regenerates this generated key.
func (w *walker) rotates(tmpl, key string) bool {
	if !w.rotating {
		return false
	}
	// Rotating either half of an age pair rotates the pair.
	return w.rotKeys == nil || w.rotKeys[key] || (hasAge(tmpl) && w.pairRot)
}

// finishBcrypt hashes the <bcrypt:...> leaves once every other key has its
// final value. An existing hash that still matches is kept, so a resync does
// not write a new salt.
func (w *walker) finishBcrypt(out map[string]any) {
	for _, b := range w.bcrypts {
		plain := secretgen.NavigateJSON(out, b.target)
		if plain == "" {
			w.block(ReasonInvalidSpec, "key %q hashes %q, which is missing or empty", b.key, b.target)
			continue
		}
		parent := parentOf(out, b.key)
		if parent == nil {
			continue
		}
		if cur, ok := b.existing.(string); ok && b.has && secretgen.BcryptMatches(cur, plain) {
			continue
		}
		h, err := secretgen.Bcrypt(plain)
		if err != nil {
			w.block(ReasonInvalidSpec, "key %q: %v", b.key, err)
			continue
		}
		parent[b.name] = h
		if b.has {
			w.record(ActionUpdateField, b.key)
		} else if !w.written[b.key] {
			w.record(ActionAddKey, b.key)
		}
	}
}

// prune removes the keys this controller wrote that the CR no longer declares.
func (w *walker) prune(out map[string]any) map[string]bool {
	pruned := map[string]bool{}
	if !w.in.Spec.Prune {
		return pruned
	}
	declared := leafKeys(w.in.Spec.Data)
	for _, k := range w.in.Spec.Managed {
		if declared[k] || !hasPath(out, k) {
			continue
		}
		deletePath(out, k)
		pruned[k] = true
		w.changes = append(w.changes, Change{Action: ActionPrune, Key: k})
	}
	return pruned
}

// managed computes status.managedKeys after the plan: the keys written so far
// that the path still holds.
func (w *walker) managed(out map[string]any, pruned map[string]bool) []string {
	set := map[string]bool{}
	for _, k := range w.in.Spec.Managed {
		if !pruned[k] && hasPath(out, k) {
			set[k] = true
		}
	}
	for k := range w.written {
		set[k] = true
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// --- tree helpers ---------------------------------------------------------

func join(prefix, k string) string {
	if prefix == "" {
		return k
	}
	return prefix + "." + k
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func copyStrings(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = deepCopy(v)
	}
	return out
}

func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return copyMap(t)
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = deepCopy(item)
		}
		return out
	default:
		return v
	}
}

func lastSegment(dotPath string) string {
	return dotPath[strings.LastIndex(dotPath, ".")+1:]
}

func hasAge(tmpl string) bool {
	return strings.Contains(tmpl, "<age:")
}

func mustTarget(tmpl string) string {
	t, _ := secretgen.BcryptTarget(tmpl)
	return t
}

// stringAt returns the string at a dot path.
func stringAt(m map[string]any, dotPath string) (string, bool) {
	var cur any = m
	for _, k := range strings.Split(dotPath, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		if cur, ok = mm[k]; !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	return s, ok
}

func hasPath(m map[string]any, dotPath string) bool {
	var cur any = m
	for _, k := range strings.Split(dotPath, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		if cur, ok = mm[k]; !ok {
			return false
		}
	}
	return true
}

// parentOf returns the map that holds the last segment of dotPath.
func parentOf(m map[string]any, dotPath string) map[string]any {
	parts := strings.Split(dotPath, ".")
	cur := m
	for _, k := range parts[:len(parts)-1] {
		next, ok := cur[k].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

// deletePath removes the leaf at dotPath and the maps it leaves empty.
func deletePath(m map[string]any, dotPath string) {
	parts := strings.Split(dotPath, ".")
	if len(parts) == 1 {
		delete(m, parts[0])
		return
	}
	child, ok := m[parts[0]].(map[string]any)
	if !ok {
		return
	}
	deletePath(child, strings.Join(parts[1:], "."))
	if len(child) == 0 {
		delete(m, parts[0])
	}
}

// leafKeys lists the dot paths of every leaf (lists count as one leaf).
func leafKeys(m map[string]any) map[string]bool {
	out := map[string]bool{}
	var walk func(prefix string, v map[string]any)
	walk = func(prefix string, v map[string]any) {
		for k, x := range v {
			key := join(prefix, k)
			if sub, ok := x.(map[string]any); ok {
				walk(key, sub)
				continue
			}
			out[key] = true
		}
	}
	walk("", m)
	return out
}

// generatedLeaves lists the dot paths of string leaves that are generated.
func generatedLeaves(m map[string]any) map[string]bool {
	out := map[string]bool{}
	var walk func(prefix string, v map[string]any)
	walk = func(prefix string, v map[string]any) {
		for k, x := range v {
			key := join(prefix, k)
			switch t := x.(type) {
			case map[string]any:
				walk(key, t)
			case string:
				if secretgen.Classify(t) == secretgen.KindGenerated {
					out[key] = true
				}
			}
		}
	}
	walk("", m)
	return out
}

// ageLeaves lists the dot paths of the <age:secret> and <age:public> leaves.
func ageLeaves(m map[string]any) (secrets, publics []string) {
	var walk func(prefix string, v map[string]any)
	walk = func(prefix string, v map[string]any) {
		for _, k := range sortedKeys(v) {
			key := join(prefix, k)
			switch t := v[k].(type) {
			case map[string]any:
				walk(key, t)
			case string:
				switch t {
				case "<age:secret>":
					secrets = append(secrets, key)
				case "<age:public>":
					publics = append(publics, key)
				}
			}
		}
	}
	walk("", m)
	return secrets, publics
}
