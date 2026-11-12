package rustfs

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestBucketPolicyReadWriteGolden(t *testing.T) {
	got, err := BucketPolicy("homelab-gitea-pg", "readWrite")
	if err != nil {
		t.Fatalf("BucketPolicy: %v", err)
	}
	want, err := os.ReadFile("testdata/policy-readwrite.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("policy differs from the golden file\n--- got\n%s\n--- want\n%s", got, want)
	}
}

func TestBucketPolicyRejectsUnknownAccess(t *testing.T) {
	if _, err := BucketPolicy("b", "admin"); err == nil {
		t.Fatal(`want an error for access "admin"`)
	}
}

// SamePolicy compares documents, not bytes: a store may answer with its own
// layout, and a resync must not rewrite a policy that is already right.
func TestSamePolicy(t *testing.T) {
	doc, err := BucketPolicy("b", "readWrite")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := BucketPolicy("c", "readWrite")
	readOnly, _ := BucketPolicy("b", "readOnly")
	const shuffled = `{"Statement":[
	  {"Resource":"arn:aws:s3:::b","Effect":"Allow","Action":["s3:ListBucketMultipartUploads","s3:ListBucket","s3:GetBucketLocation"]},
	  {"Effect":"Allow","Resource":["arn:aws:s3:::b/*"],"Action":["s3:PutObject","s3:ListMultipartUploadParts","s3:GetObject","s3:DeleteObject","s3:AbortMultipartUpload"]},
	  {"Effect":"Deny","Resource":["arn:aws:s3:::b/*","arn:aws:s3:::b"],"Action":["s3:ResetBucketReplicationState","s3:PutReplicationConfiguration","s3:PutBucketVersioning","s3:PutBucketPolicy","s3:PutBucketObjectLockConfiguration","s3:PutBucketLifecycle","s3:PutBucketEncryption","s3:ForceDeleteBucket","s3:DeleteObjectVersion","s3:DeleteBucketPolicy","s3:DeleteBucket","s3:BypassGovernanceRetention"]}
	],"Version":"2012-10-17"}`
	withCondition := strings.Replace(shuffled, `"Effect":"Deny",`, `"Effect":"Deny","Condition":{"Bool":{"aws:SecureTransport":"false"}},`, 1)
	tests := []struct {
		name string
		a, b []byte
		want bool
	}{
		{"the same document", doc, doc, true},
		{"another layout, the same statements", doc, []byte(shuffled), true},
		{"another bucket", doc, other, false},
		{"another access", doc, readOnly, false},
		{"an extra condition", doc, []byte(withCondition), false},
		{"not JSON", doc, []byte("{"), false},
		{"nothing", doc, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SamePolicy(tt.a, tt.b); got != tt.want {
				t.Errorf("SamePolicy = %v, want %v", got, tt.want)
			}
		})
	}
}

// readOnly keeps the same Deny statement and allows only reads.
func TestBucketPolicyReadOnlyActions(t *testing.T) {
	got, err := BucketPolicy("b", "readOnly")
	if err != nil {
		t.Fatalf("BucketPolicy: %v", err)
	}
	var doc struct {
		Statement []struct {
			Effect string
			Action []string
		}
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	var allowed, denied []string
	for _, s := range doc.Statement {
		if s.Effect == "Allow" {
			allowed = append(allowed, s.Action...)
		} else {
			denied = append(denied, s.Action...)
		}
	}
	slices.Sort(allowed)
	if want := []string{"s3:GetBucketLocation", "s3:GetObject", "s3:ListBucket"}; !slices.Equal(allowed, want) {
		t.Errorf("allowed = %v, want %v", allowed, want)
	}
	if !slices.Contains(denied, "s3:DeleteObjectVersion") || len(denied) != 12 {
		t.Errorf("denied = %v, want the 12 readWrite denials", denied)
	}
}
