package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/exoscale/vitrine/internal/store"
)

func app() (*App, *bytes.Buffer, *bytes.Buffer) {
	var o, e bytes.Buffer
	return &App{Stdin: strings.NewReader(""), Stdout: &o, Stderr: &e, Now: time.Now,
		Getenv: func(string) string { return "" }}, &o, &e
}

func TestUsageAndErrors(t *testing.T) {
	a, _, e := app()
	if a.Run(nil) != ExitUsage || !strings.Contains(e.String(), "Usage:") {
		t.Fatal("no args")
	}
	a, o, _ := app()
	if a.Run([]string{"help"}) != ExitOK || !strings.Contains(o.String(), "vitrine record add") {
		t.Fatal("help")
	}
	a, _, e = app()
	if a.Run([]string{"--config", "/nonexistent.json", "ledger", "verify"}) != ExitUsage || !strings.Contains(e.String(), "vitrine init") {
		t.Fatalf("missing config: %s", e)
	}
	a, _, e = app()
	if a.Run([]string{"publish"}) != ExitUsage || !strings.Contains(e.String(), "--period") {
		t.Fatalf("publish without period: %s", e)
	}
	a, _, e = app()
	if a.Run([]string{"record", "template", "nope"}) != ExitUsage {
		t.Fatal("bad template type")
	}
}

func TestParseFlagsAfterPositionals(t *testing.T) {
	a, _, _ := app()
	fs := a.flags("x")
	v := fs.Int64("amends", 0, "")
	pos, err := parse(fs, []string{"gov_request", "--amends", "4"})
	if err != nil || len(pos) != 1 || pos[0] != "gov_request" || *v != 4 {
		t.Fatalf("%v %v %d", pos, err, *v)
	}
}

func TestHandler(t *testing.T) {
	s, _ := store.NewFile(t.TempDir())
	ctx := context.Background()
	s.Put(ctx, "index.html", []byte("<h1>root</h1>"), store.PutOptions{})
	s.Put(ctx, "reports/2026/v1/index.html", []byte("<h1>r</h1>"), store.PutOptions{})
	s.Put(ctx, "latest.json", []byte("{}"), store.PutOptions{})
	srv := httptest.NewServer(Handler(s))
	defer srv.Close()
	get := func(method, p string) (*http.Response, string) {
		req, _ := http.NewRequest(method, srv.URL+p, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		b.ReadFrom(resp.Body)
		resp.Body.Close()
		return resp, b.String()
	}
	if r, b := get("GET", "/"); r.StatusCode != 200 || b != "<h1>root</h1>" || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/html") {
		t.Fatal("root")
	}
	if r, _ := get("GET", "/reports/2026/v1/"); r.StatusCode != 200 {
		t.Fatal("dir index")
	}
	if r, _ := get("GET", "/latest.json"); r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("json headers")
	}
	if r, _ := get("GET", "/missing"); r.StatusCode != 404 {
		t.Fatal("404")
	}
	if r, _ := get("POST", "/"); r.StatusCode != 405 {
		t.Fatal("POST must be refused")
	}
	if r, b := get("HEAD", "/latest.json"); r.StatusCode != 200 || b != "" {
		t.Fatal("HEAD")
	}
}
