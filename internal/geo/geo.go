// Package geo holds jurisdiction sets used to classify requests.
package geo

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// EU27 Member States (ISO 3166-1 alpha-2; Greece is GR).
var EU27 = []string{
	"AT", "BE", "BG", "CY", "CZ", "DE", "DK", "EE", "ES", "FI", "FR", "GR", "HR", "HU",
	"IE", "IT", "LT", "LU", "LV", "MT", "NL", "PL", "PT", "RO", "SE", "SI", "SK",
}

// EEAOnly are EEA members outside the EU.
var EEAOnly = []string{"IS", "LI", "NO"}

var iso2 = regexp.MustCompile(`^[A-Z]{2}$`)

// ValidCountry checks ISO 3166-1 alpha-2 format and common non-ISO mistakes.
func ValidCountry(c string) error {
	switch c {
	case "UK":
		return fmt.Errorf("country %q is not ISO 3166-1; use GB", c)
	case "EL":
		return fmt.Errorf("country %q is the EU code; use ISO GR", c)
	}
	if !iso2.MatchString(c) {
		return fmt.Errorf("country %q must be ISO 3166-1 alpha-2 (e.g. CH, DE, US)", c)
	}
	return nil
}

// IsEU reports EU Member State membership.
func IsEU(c string) bool { return contains(EU27, c) }

// Set is an expanded set of country codes.
type Set map[string]bool

// Expand turns ["CH","EU","EEA"] into a country set.
func Expand(items []string) (Set, error) {
	s := Set{}
	for _, it := range items {
		switch strings.ToUpper(it) {
		case "EU":
			for _, c := range EU27 {
				s[c] = true
			}
		case "EEA":
			for _, c := range EU27 {
				s[c] = true
			}
			for _, c := range EEAOnly {
				s[c] = true
			}
		default:
			if err := ValidCountry(it); err != nil {
				return nil, err
			}
			s[it] = true
		}
	}
	return s, nil
}

// Sorted returns the set members in order.
func (s Set) Sorted() []string {
	out := make([]string, 0, len(s))
	for c := range s {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
