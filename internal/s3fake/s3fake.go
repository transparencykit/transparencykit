// Package s3fake is an in-memory, path-style S3 server for tests. It implements
// PUT (with If-None-Match: *), GET, and ListObjectsV2 with pagination, and
// checks that requests carry a SigV4 Authorization header and a matching
// x-amz-content-sha256 and Content-MD5.
package s3fake

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Server is the fake.
type Server struct {
	*httptest.Server
	mu       sync.Mutex
	objects  map[string][]byte // "bucket/key"
	headers  map[string]http.Header
	PageSize int // ListObjectsV2 page size (default 1000)
	// IgnoreIfNoneMatch simulates a backend without conditional writes.
	IgnoreIfNoneMatch bool
	Requests          int
}

// New starts a fake server.
func New() *Server {
	s := &Server{objects: map[string][]byte{}, headers: map[string]http.Header{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// Object returns a stored object.
func (s *Server) Object(bucket, key string) ([]byte, http.Header, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.objects[bucket+"/"+key]
	return b, s.headers[bucket+"/"+key], ok
}

// SetObject overwrites an object directly (to simulate tampering).
func (s *Server) SetObject(bucket, key string, b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[bucket+"/"+key] = b
}

func xmlErr(w http.ResponseWriter, code int, c, msg string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(code)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message></Error>`, c, msg)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.Requests++
	s.mu.Unlock()
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=") {
		xmlErr(w, 403, "AccessDenied", "missing sigv4")
		return
	}
	body, _ := io.ReadAll(r.Body)
	sum := sha256.Sum256(body)
	if r.Header.Get("X-Amz-Content-Sha256") != hex.EncodeToString(sum[:]) {
		xmlErr(w, 400, "XAmzContentSHA256Mismatch", "payload hash mismatch")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(path, "/")
	if bucket == "" {
		xmlErr(w, 400, "InvalidRequest", "path-style bucket required")
		return
	}
	id := bucket + "/" + key
	switch {
	case r.Method == http.MethodPut && key != "":
		if m := r.Header.Get("Content-MD5"); m != "" {
			ms := md5.Sum(body)
			if m != base64.StdEncoding.EncodeToString(ms[:]) {
				xmlErr(w, 400, "BadDigest", "md5 mismatch")
				return
			}
		}
		s.mu.Lock()
		_, exists := s.objects[id]
		if exists && r.Header.Get("If-None-Match") == "*" && !s.IgnoreIfNoneMatch {
			s.mu.Unlock()
			xmlErr(w, 412, "PreconditionFailed", "exists")
			return
		}
		s.objects[id] = body
		s.headers[id] = r.Header.Clone()
		s.mu.Unlock()
		w.WriteHeader(200)
	case r.Method == http.MethodGet && key != "":
		s.mu.Lock()
		b, ok := s.objects[id]
		s.mu.Unlock()
		if !ok {
			xmlErr(w, 404, "NoSuchKey", "no such key")
			return
		}
		w.Write(b)
	case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		s.list(w, r, bucket)
	default:
		xmlErr(w, 405, "MethodNotAllowed", r.Method)
	}
}

type contents struct {
	Key  string `xml:"Key"`
	Size int    `xml:"Size"`
}

type listResult struct {
	XMLName               xml.Name   `xml:"ListBucketResult"`
	Name                  string     `xml:"Name"`
	Prefix                string     `xml:"Prefix"`
	KeyCount              int        `xml:"KeyCount"`
	IsTruncated           bool       `xml:"IsTruncated"`
	NextContinuationToken string     `xml:"NextContinuationToken,omitempty"`
	Contents              []contents `xml:"Contents"`
}

func (s *Server) list(w http.ResponseWriter, r *http.Request, bucket string) {
	q := r.URL.Query()
	prefix := q.Get("prefix")
	s.mu.Lock()
	var keys []string
	for id := range s.objects {
		b, k, _ := strings.Cut(id, "/")
		if b == bucket && strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	s.mu.Unlock()
	sort.Strings(keys)
	start := 0
	if t := q.Get("continuation-token"); t != "" {
		start, _ = strconv.Atoi(t)
	}
	size := s.PageSize
	if size <= 0 {
		size = 1000
	}
	if mk, _ := strconv.Atoi(q.Get("max-keys")); mk > 0 && mk < size {
		size = mk
	}
	end := start + size
	res := listResult{Name: bucket, Prefix: prefix}
	if end < len(keys) {
		res.IsTruncated = true
		res.NextContinuationToken = strconv.Itoa(end)
	} else {
		end = len(keys)
	}
	for _, k := range keys[start:end] {
		s.mu.Lock()
		n := len(s.objects[bucket+"/"+k])
		s.mu.Unlock()
		res.Contents = append(res.Contents, contents{Key: k, Size: n})
	}
	res.KeyCount = len(res.Contents)
	w.Header().Set("Content-Type", "application/xml")
	xml.NewEncoder(w).Encode(res)
}
