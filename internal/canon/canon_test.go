package canon

import "testing"

func TestCanonicalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"b":1,"a":{"d":[3,2],"c":"x"}}`, `{"a":{"c":"x","d":[3,2]},"b":1}`},
		{"{ \"a\" : \"<&>\" }", `{"a":"<&>"}`},
		{`{"a":"éé"}`, `{"a":"éé"}`},
		{`{"n":12345678901234567890}`, `{"n":12345678901234567890}`},
		{`[true,null,false]`, `[true,null,false]`},
	}
	for _, c := range cases {
		got, err := Canonicalize([]byte(c.in))
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if string(got) != c.want {
			t.Errorf("Canonicalize(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestRejectsFloatsAndTrailing(t *testing.T) {
	for _, in := range []string{`{"a":1.5}`, `{"a":1e3}`, `{"a":1} {"b":2}`, `{"a":`} {
		if _, err := Canonicalize([]byte(in)); err == nil {
			t.Errorf("%s: expected error", in)
		}
	}
}

func TestHashStableAcrossKeyOrder(t *testing.T) {
	a, _ := Canonicalize([]byte(`{"x":1,"y":2}`))
	b, _ := Canonicalize([]byte(`{"y":2,"x":1}`))
	if Hash(a) != Hash(b) {
		t.Fatal("hash depends on key order")
	}
	if Hash([]byte("")) != "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatal("unexpected empty hash")
	}
	if len(Zero) != 7+64 {
		t.Fatal("bad Zero")
	}
}
