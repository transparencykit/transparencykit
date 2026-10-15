package store

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Signer implements AWS Signature Version 4 for S3.
type Signer struct {
	AccessKey, SecretKey, Region string
	Service                      string // "s3"
}

const amzDate = "20060102T150405Z"

// Sign adds x-amz-date, x-amz-content-sha256 and Authorization headers.
// payloadHash is the hex SHA-256 of the body.
func (s Signer) Sign(r *http.Request, payloadHash string, now time.Time) {
	t := now.UTC()
	r.Header.Set("X-Amz-Date", t.Format(amzDate))
	r.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if r.Host == "" {
		r.Host = r.URL.Host
	}
	signed, canonHeaders := canonicalHeaders(r)
	creq := strings.Join([]string{
		r.Method,
		canonicalURI(r.URL),
		canonicalQuery(r.URL),
		canonHeaders,
		signed,
		payloadHash,
	}, "\n")
	day := t.Format("20060102")
	scope := day + "/" + s.Region + "/" + s.svc() + "/aws4_request"
	sts := "AWS4-HMAC-SHA256\n" + t.Format(amzDate) + "\n" + scope + "\n" + hexSum([]byte(creq))
	k := hmacSHA(hmacSHA(hmacSHA(hmacSHA([]byte("AWS4"+s.SecretKey), day), s.Region), s.svc()), "aws4_request")
	sig := hex.EncodeToString(hmacSHA(k, sts))
	r.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.AccessKey+"/"+scope+
		", SignedHeaders="+signed+", Signature="+sig)
}

func (s Signer) svc() string {
	if s.Service == "" {
		return "s3"
	}
	return s.Service
}

func canonicalHeaders(r *http.Request) (signed, canon string) {
	h := map[string]string{"host": r.Host}
	for k, vs := range r.Header {
		lk := strings.ToLower(k)
		if lk == "authorization" || lk == "user-agent" {
			continue
		}
		if lk == "content-type" || lk == "content-md5" || lk == "range" || lk == "if-none-match" ||
			lk == "if-match" || strings.HasPrefix(lk, "x-amz-") {
			vals := make([]string, len(vs))
			for i, v := range vs {
				vals[i] = strings.Join(strings.Fields(v), " ")
			}
			h[lk] = strings.Join(vals, ",")
		}
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k + ":" + h[k] + "\n")
	}
	return strings.Join(keys, ";"), b.String()
}

// canonicalURI encodes each path segment per RFC 3986 (S3: no double encoding).
func canonicalURI(u *url.URL) string {
	p := u.EscapedPath()
	if p == "" {
		return "/"
	}
	// Normalise to our own strict encoding from the decoded path.
	dec, err := url.PathUnescape(p)
	if err != nil {
		return p
	}
	segs := strings.Split(dec, "/")
	for i, s := range segs {
		segs[i] = uriEncode(s, true)
	}
	return strings.Join(segs, "/")
}

func canonicalQuery(u *url.URL) string {
	q := u.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, uriEncode(k, true)+"="+uriEncode(v, true))
		}
	}
	return strings.Join(parts, "&")
}

// uriEncode encodes everything except unreserved characters (A-Z a-z 0-9 - _ . ~).
func uriEncode(s string, encodeSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && !encodeSlash:
			b.WriteByte(c)
		default:
			b.WriteString("%" + strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return b.String()
}

func hexSum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func hmacSHA(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}
