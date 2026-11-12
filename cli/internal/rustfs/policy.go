package rustfs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

type statement struct {
	Effect   string   `json:"Effect"`
	Action   []string `json:"Action"`
	Resource []string `json:"Resource"`
}

type policyDocument struct {
	Version   string      `json:"Version"`
	Statement []statement `json:"Statement"`
}

// An app key never removes object versions or changes its bucket's
// configuration: lifecycle and versioning belong to whoever runs the store.
// Action names are RustFS's (crates/policy/src/policy/action.rs): it refuses
// AWS's s3:PutLifecycleConfiguration and names it s3:PutBucketLifecycle.
var deniedActions = []string{
	"s3:BypassGovernanceRetention",
	"s3:DeleteBucket",
	"s3:DeleteBucketPolicy",
	"s3:DeleteObjectVersion",
	"s3:ForceDeleteBucket",
	"s3:PutBucketEncryption",
	"s3:PutBucketLifecycle",
	"s3:PutBucketObjectLockConfiguration",
	"s3:PutBucketPolicy",
	"s3:PutBucketVersioning",
	"s3:PutReplicationConfiguration",
	"s3:ResetBucketReplicationState",
}

// BucketPolicy returns the canned policy that confines an app key to one
// bucket, with access "readWrite" or "readOnly".
func BucketPolicy(bucket, access string) ([]byte, error) {
	var bucketActions, objectActions []string
	switch access {
	case "readWrite":
		bucketActions = []string{"s3:GetBucketLocation", "s3:ListBucket", "s3:ListBucketMultipartUploads"}
		objectActions = []string{"s3:AbortMultipartUpload", "s3:DeleteObject", "s3:GetObject", "s3:ListMultipartUploadParts", "s3:PutObject"}
	case "readOnly":
		bucketActions = []string{"s3:GetBucketLocation", "s3:ListBucket"}
		objectActions = []string{"s3:GetObject"}
	default:
		return nil, fmt.Errorf("access %q: want readWrite or readOnly", access)
	}
	bucketARN := "arn:aws:s3:::" + bucket
	objectsARN := bucketARN + "/*"
	doc := policyDocument{
		Version: "2012-10-17",
		Statement: []statement{
			{Effect: "Allow", Action: sortedCopy(bucketActions), Resource: []string{bucketARN}},
			{Effect: "Allow", Action: sortedCopy(objectActions), Resource: []string{objectsARN}},
			{Effect: "Deny", Action: sortedCopy(deniedActions), Resource: []string{bucketARN, objectsARN}},
		},
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func sortedCopy(s []string) []string {
	c := slices.Clone(s)
	slices.Sort(c)
	return c
}

// SamePolicy reports whether two policy documents say the same thing: keys
// and statements in any order, actions and resources as sets (a lone string
// being a set of one), and empty values as absent. A store may answer with
// its own layout, and a resync must not rewrite a policy that is right.
func SamePolicy(a, b []byte) bool {
	ca, err := canonicalPolicy(a)
	if err != nil {
		return false
	}
	cb, err := canonicalPolicy(b)
	return err == nil && bytes.Equal(ca, cb)
}

var setKeys = []string{"Action", "NotAction", "Resource", "NotResource"}

func canonicalPolicy(doc []byte) ([]byte, error) {
	var v map[string]any
	if err := json.Unmarshal(doc, &v); err != nil {
		return nil, err
	}
	if v == nil {
		return nil, fmt.Errorf("no policy document")
	}
	var stmts []any
	switch s := v["Statement"].(type) {
	case []any:
		stmts = s
	case map[string]any:
		stmts = []any{s}
	}
	canon := make([]string, 0, len(stmts))
	for _, s := range stmts {
		m, ok := s.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("a statement is not an object")
		}
		for _, k := range setKeys {
			if x, ok := m[k]; ok {
				m[k] = stringSet(x)
			}
		}
		dropEmpty(m)
		out, err := json.Marshal(m) // map keys come out sorted
		if err != nil {
			return nil, err
		}
		canon = append(canon, string(out))
	}
	slices.Sort(canon)
	v["Statement"] = canon
	dropEmpty(v)
	return json.Marshal(v)
}

func stringSet(x any) []string {
	var out []string
	switch s := x.(type) {
	case string:
		out = []string{s}
	case []any:
		for _, e := range s {
			out = append(out, fmt.Sprint(e))
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func dropEmpty(m map[string]any) {
	for k, x := range m {
		switch e := x.(type) {
		case nil:
			delete(m, k)
		case string:
			if e == "" {
				delete(m, k)
			}
		case []any:
			if len(e) == 0 {
				delete(m, k)
			}
		case []string:
			if len(e) == 0 {
				delete(m, k)
			}
		case map[string]any:
			if len(e) == 0 {
				delete(m, k)
			}
		}
	}
}
