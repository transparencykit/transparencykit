package store

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// Reference vectors from the AWS S3 documentation ("Signature Calculations
// for the Authorization Header: Transferring Payload in a Single Chunk").
const (
	awsAK     = "AKIAIOSFODNN7EXAMPLE"
	awsSK     = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	emptyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

var awsTime = time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)

func sigOf(h string) string {
	i := strings.Index(h, "Signature=")
	if i < 0 {
		return ""
	}
	return h[i+len("Signature="):]
}

func TestSigV4GetObjectVector(t *testing.T) {
	r, _ := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	r.Header.Set("Range", "bytes=0-9")
	Signer{AccessKey: awsAK, SecretKey: awsSK, Region: "us-east-1"}.Sign(r, emptyHash, awsTime)
	auth := r.Header.Get("Authorization")
	if !strings.Contains(auth, "SignedHeaders=host;range;x-amz-content-sha256;x-amz-date") {
		t.Fatalf("signed headers: %s", auth)
	}
	if got, want := sigOf(auth), "f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"; got != want {
		t.Fatalf("signature %s, want %s", got, want)
	}
}

func TestSigV4ListObjectsVector(t *testing.T) {
	r, _ := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/?max-keys=2&prefix=J", nil)
	Signer{AccessKey: awsAK, SecretKey: awsSK, Region: "us-east-1"}.Sign(r, emptyHash, awsTime)
	if got, want := sigOf(r.Header.Get("Authorization")), "34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7"; got != want {
		t.Fatalf("signature %s, want %s", got, want)
	}
}

func TestURIEncode(t *testing.T) {
	cases := map[string]string{
		"a b":      "a%20b",
		"a/b":      "a%2Fb",
		"~-_.":     "~-_.",
		"é":        "%C3%A9",
		"$file+=x": "%24file%2B%3Dx",
	}
	for in, want := range cases {
		if got := uriEncode(in, true); got != want {
			t.Errorf("uriEncode(%q) = %q, want %q", in, got, want)
		}
	}
}
