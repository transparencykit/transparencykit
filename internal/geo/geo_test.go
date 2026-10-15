package geo

import "testing"

func TestExpandAndValidate(t *testing.T) {
	s, err := Expand([]string{"CH", "EU"})
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 28 || !s["CH"] || !s["GR"] || s["NO"] {
		t.Fatalf("EU+CH = %d", len(s))
	}
	s, _ = Expand([]string{"eea"})
	if len(s) != 30 || !s["LI"] {
		t.Fatalf("EEA = %d", len(s))
	}
	for _, bad := range []string{"UK", "EL", "che", "c1", ""} {
		if ValidCountry(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if !IsEU("DE") || IsEU("CH") || IsEU("GB") {
		t.Fatal("IsEU")
	}
	if len(EU27) != 27 {
		t.Fatal("EU27 length")
	}
}
