package store

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// S3Config configures an S3-compatible store.
type S3Config struct {
	Endpoint  string // https://sos-ch-gva-2.exo.io
	Region    string // ch-gva-2
	Bucket    string
	Prefix    string // optional key prefix, no leading/trailing slash
	PathStyle bool
	AccessKey string
	SecretKey string
	Client    *http.Client
	Now       func() time.Time
}

// S3 is a minimal S3 client (PUT, GET, ListObjectsV2) with SigV4 signing.
type S3 struct {
	cfg    S3Config
	signer Signer
	base   *url.URL
}

// NewS3 validates config and returns a store.
func NewS3(cfg S3Config) (*S3, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("s3: invalid endpoint %q", cfg.Endpoint)
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 60 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &S3{cfg: cfg, base: u, signer: Signer{AccessKey: cfg.AccessKey, SecretKey: cfg.SecretKey, Region: cfg.Region}}, nil
}

func (s *S3) String() string {
	p := s.cfg.Bucket
	if s.cfg.Prefix != "" {
		p += "/" + s.cfg.Prefix
	}
	return "s3://" + p + " @ " + s.cfg.Endpoint
}

func (s *S3) fullKey(key string) string {
	if s.cfg.Prefix == "" {
		return key
	}
	return s.cfg.Prefix + "/" + key
}

func (s *S3) objectURL(fullKey string, q url.Values) *url.URL {
	u := *s.base
	if s.cfg.PathStyle {
		u.Path = "/" + s.cfg.Bucket
		if fullKey != "" {
			u.Path += "/" + fullKey
		}
	} else {
		u.Host = s.cfg.Bucket + "." + s.base.Host
		u.Path = "/" + fullKey
	}
	u.RawPath = ""
	if q != nil {
		u.RawQuery = q.Encode()
	}
	return &u
}

func (s *S3) do(ctx context.Context, method string, u *url.URL, body []byte, hdr http.Header) (*http.Response, error) {
	if s.cfg.AccessKey == "" || s.cfg.SecretKey == "" {
		return nil, fmt.Errorf("s3: missing credentials (set EXOSCALE_API_KEY/EXOSCALE_API_SECRET)")
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	// Keep our exact path encoding.
	req.URL.Opaque = ""
	req.URL.RawPath = uriEncodePath(u.Path)
	for k, vs := range hdr {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("User-Agent", "vitrine")
	s.signer.Sign(req, hexSum(body), s.cfg.Now())
	return s.cfg.Client.Do(req)
}

func uriEncodePath(p string) string {
	segs := strings.Split(p, "/")
	for i, x := range segs {
		segs[i] = uriEncode(x, true)
	}
	return strings.Join(segs, "/")
}

type s3Error struct {
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

func readErr(resp *http.Response, op, key string) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var e s3Error
	_ = xml.Unmarshal(b, &e)
	if e.Code == "" {
		e.Code = resp.Status
	}
	return fmt.Errorf("s3 %s %s: %s %s", op, key, e.Code, e.Message)
}

// Put uploads an object.
func (s *S3) Put(ctx context.Context, key string, data []byte, opt PutOptions) error {
	h := http.Header{}
	if opt.ContentType != "" {
		h.Set("Content-Type", opt.ContentType)
	}
	sum := md5.Sum(data)
	h.Set("Content-MD5", base64.StdEncoding.EncodeToString(sum[:]))
	if opt.CreateOnly {
		h.Set("If-None-Match", "*")
	}
	if opt.Public {
		h.Set("X-Amz-Acl", "public-read")
	}
	if opt.Retention != nil && opt.Retention.Days > 0 {
		h.Set("X-Amz-Object-Lock-Mode", strings.ToUpper(opt.Retention.Mode))
		until := s.cfg.Now().UTC().AddDate(0, 0, opt.Retention.Days).Format(time.RFC3339)
		h.Set("X-Amz-Object-Lock-Retain-Until-Date", until)
	}
	fk := s.fullKey(key)
	resp, err := s.do(ctx, http.MethodPut, s.objectURL(fk, nil), data, h)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusPreconditionFailed:
		return ErrExists
	case resp.StatusCode == http.StatusConflict && opt.CreateOnly:
		return ErrExists
	case resp.StatusCode/100 != 2:
		return readErr(resp, "PUT", fk)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

// Get downloads an object.
func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	fk := s.fullKey(key)
	resp, err := s.do(ctx, http.MethodGet, s.objectURL(fk, nil), nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode/100 != 2 {
		return nil, readErr(resp, "GET", fk)
	}
	return io.ReadAll(resp.Body)
}

type listResult struct {
	Contents []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
}

// List pages through ListObjectsV2.
func (s *S3) List(ctx context.Context, prefix string) ([]string, error) {
	fp := s.fullKey(prefix)
	strip := ""
	if s.cfg.Prefix != "" {
		strip = s.cfg.Prefix + "/"
	}
	var keys []string
	token := ""
	for page := 0; page < 100000; page++ {
		q := url.Values{"list-type": {"2"}, "prefix": {fp}, "max-keys": {strconv.Itoa(1000)}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		resp, err := s.do(ctx, http.MethodGet, s.objectURL("", q), nil, nil)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode/100 != 2 {
			err := readErr(resp, "LIST", fp)
			resp.Body.Close()
			return nil, err
		}
		var lr listResult
		err = xml.NewDecoder(resp.Body).Decode(&lr)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("s3 LIST %s: %w", fp, err)
		}
		for _, c := range lr.Contents {
			keys = append(keys, strings.TrimPrefix(c.Key, strip))
		}
		if !lr.IsTruncated || lr.NextContinuationToken == "" {
			break
		}
		token = lr.NextContinuationToken
	}
	sort.Strings(keys)
	return keys, nil
}
