package report

import (
	"fmt"
	"math"
	"sort"
)

// Disclosure control for published breakdowns.
//
// Banding hides counts 1..min-1 in individual cells, but a reader can often
// recover a banded cell by subtraction: when a breakdown's total is published
// exactly, total minus the exact cells equals the sum of the banded cells,
// and that sum may admit only one value per cell (always when a single cell
// is banded). Derived metrics such as rates and medians can leak the same way.
//
// This file detects such one-dimensional derivations exactly and, by
// default, withholds the affected breakdown. Cross-table inference (for
// example combining the country table with the outcome breakdown) is not
// detected; SPEC §7.2 documents that residual risk.

// derivable returns the banded cells of a breakdown whose exact values can be
// computed from the cells and the exact total. It returns nil when the cells
// do not sum to the total (no linear relation is published) or nothing leaks.
func derivable(cells map[string]int, total, min int) []string {
	if min <= 1 {
		return nil
	}
	sum, exact, k := 0, 0, 0
	var banded []string
	for name, c := range cells {
		sum += c
		if c > 0 && c < min {
			k++
			banded = append(banded, name)
		} else {
			exact += c
		}
	}
	if k == 0 || sum != total {
		return nil
	}
	rest := total - exact // sum of the banded cells, known to the reader
	if k == 1 {
		sort.Strings(banded)
		return banded
	}
	// Each banded cell lies in [1, min-1]; the other k-1 can sum to any value
	// in [k-1, (k-1)(min-1)]. A cell is determined when only one value fits.
	lo := maxInt(1, rest-(k-1)*(min-1))
	hi := minInt(min-1, rest-(k-1))
	if lo == hi {
		sort.Strings(banded)
		return banded
	}
	return nil
}

// derivableOpen is derivable for a breakdown that shows only some categories
// of an exact total (the rest is hidden, so it is any value >= 0). A banded
// cell is determined only when the published cells leave no room: every
// banded cell must then be 1.
func derivableOpen(cells map[string]int, total, min int) []string {
	if min <= 1 {
		return nil
	}
	rest := total
	var banded []string
	for name, c := range cells {
		if c > 0 && c < min {
			banded = append(banded, name)
		} else {
			rest -= c
		}
	}
	if len(banded) == 0 || rest > len(banded) {
		return nil
	}
	sort.Strings(banded)
	return banded
}

// Withheld describes a breakdown removed from the public report.
type Withheld struct {
	Section string   `json:"section"`
	Cells   []string `json:"would_reveal"`
	Reason  string   `json:"reason"`
}

// policy applies the configured handling to one breakdown and returns the
// map to publish (nil when withheld).
type policy struct {
	min      int
	withhold bool
	out      []Withheld
}

func (p *policy) check(section string, cells map[string]int, total int, banded map[string]Cell) map[string]Cell {
	leak := derivable(cells, total, p.min)
	if leak == nil {
		return banded
	}
	w := Withheld{Section: section, Cells: leak,
		Reason: fmt.Sprintf("counts below %d could be calculated from the published total", p.min)}
	p.out = append(p.out, w)
	if p.withhold {
		return nil
	}
	return banded
}

// rateCell returns the share of num among den (both lists of keys of cells)
// as a percentage. A reader who sees the banded cells and, when the cells add
// up to it, the exact total can narrow each banded cell only to the values
// consistent with both. rateCell takes the minimum and maximum rate over all
// those consistent assignments: a single value is published exactly (it
// reveals nothing new), otherwise a "lo–hi" range. It returns nil when the
// denominator is zero.
func rateCell(cells map[string]int, num, den []string, total, min int) Cell {
	var banded []string
	sum := 0
	for k, c := range cells {
		sum += c
		if c > 0 && c < min {
			banded = append(banded, k)
		}
	}
	sort.Strings(banded)
	constrained := sum == total
	isNum := map[string]bool{}
	for _, k := range num {
		isNum[k] = true
	}
	inDen := map[string]bool{}
	for _, k := range den {
		inDen[k] = true
	}
	baseN, baseD, exactSum := 0, 0, 0
	for k, c := range cells {
		if c > 0 && c < min {
			continue
		}
		exactSum += c
		if isNum[k] {
			baseN += c
		}
		if inDen[k] {
			baseD += c
		}
	}
	lo, hi, found := math.Inf(1), math.Inf(-1), false
	consider := func(n, d int) {
		if d == 0 {
			return
		}
		r := float64(n) / float64(d)
		lo, hi, found = math.Min(lo, r), math.Max(hi, r), true
	}
	space := 1.0
	for range banded {
		space *= float64(min - 1)
	}
	if space <= 1e6 {
		// Enumerate every assignment of banded cells to [1, min-1].
		vals := make([]int, len(banded))
		var rec func(i, s int)
		rec = func(i, s int) {
			if i == len(banded) {
				if constrained && s != total-exactSum {
					return
				}
				n, d := baseN, baseD
				for j, k := range banded {
					if isNum[k] {
						n += vals[j]
					}
					if inDen[k] {
						d += vals[j]
					}
				}
				consider(n, d)
				return
			}
			for v := 1; v < min; v++ {
				vals[i] = v
				rec(i+1, s+v)
			}
		}
		rec(0, 0)
	} else {
		// Too many combinations: use independent bounds (wider, never narrower
		// than the true range).
		nlo, nhi, dlo, dhi := baseN, baseN, baseD, baseD
		for _, k := range banded {
			if isNum[k] {
				nlo, nhi = nlo+1, nhi+min-1
			}
			if inDen[k] {
				dlo, dhi = dlo+1, dhi+min-1
			}
		}
		consider(nlo, dhi)
		consider(nhi, maxInt(dlo, 1))
	}
	if !found {
		return nil
	}
	pct := func(r float64) float64 { return math.Round(math.Min(r, 1)*1000) / 10 }
	if pct(lo) == pct(hi) {
		return pct(lo)
	}
	return fmt.Sprintf("%.1f–%.1f", pct(lo), pct(hi))
}

// medianCell suppresses medians over fewer than min observations, which would
// otherwise reveal individual cases.
func medianCell(d Durations, min int) *float64 {
	if len(d) < min {
		return nil
	}
	return d.Median()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
