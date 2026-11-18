// SPDX-License-Identifier: Apache-2.0

package figure

import (
	"fmt"
	"html"
	"math/big"
	"strconv"
	"strings"
)

// The figures grade 2 adds (and grades 3 to 5 reuse): coins, picture
// graphs, bar graphs and line plots.

// coins draws US coins at their relative sizes, each marked with its value.
func (d *drawer) coins(a Args) error {
	type kind struct {
		key, mark, class string
		r                float64
	}
	kinds := []kind{
		{"quarters", "25¢", "fig-coin", 30},
		{"dimes", "10¢", "fig-coin", 22},
		{"nickels", "5¢", "fig-coin", 26},
		{"pennies", "1¢", "fig-coin fig-penny", 23.5},
	}
	var all []kind
	for _, k := range kinds {
		n, err := whole(a, k.key, 0, 9)
		if err != nil {
			return err
		}
		for range n {
			all = append(all, k)
		}
	}
	if len(all) == 0 || len(all) > 15 {
		return fmt.Errorf("%d coins; draw 1 to 15", len(all))
	}
	const cell = 70.0
	per := min(len(all), 5)
	d.w, d.h = float64(per)*cell+10, float64((len(all)+4)/5)*cell+10
	d.label = fmt.Sprintf("%d coins", len(all))
	for i, k := range all {
		cx, cy := 5+cell/2+float64(i%5)*cell, 5+cell/2+float64(i/5)*cell
		d.add(`<circle class="%s" cx="%s" cy="%s" r="%s"/>`, k.class, num(cx), num(cy), num(k.r))
		d.text(cx, cy+7, "fig-small", k.mark)
	}
	d.widen(220)
	return nil
}

// graphRow is one row of a graph: a label and a value.
type graphRow struct {
	label string
	v     *big.Rat
}

// graphRows reads "Cats:4|Dogs:7" (2 to 6 rows).
func graphRows(a Args, maxV int64) ([]graphRow, error) {
	raw, ok := a["rows"]
	if !ok {
		return nil, fmt.Errorf("no rows")
	}
	var rows []graphRow
	for _, part := range strings.Split(raw, "|") {
		label, count, ok := strings.Cut(part, ":")
		label = strings.TrimSpace(label)
		v, vok := new(big.Rat).SetString(strings.TrimSpace(count))
		if !ok || !vok || label == "" || len(label) > 12 || v.Sign() < 0 || v.Cmp(new(big.Rat).SetInt64(maxV)) > 0 {
			return nil, fmt.Errorf("row %q must be a short label, a colon and a number from 0 to %d", part, maxV)
		}
		rows = append(rows, graphRow{label, v})
	}
	if len(rows) < 2 || len(rows) > 6 {
		return nil, fmt.Errorf("%d rows; a graph has 2 to 6", len(rows))
	}
	return rows, nil
}

// pictureGraph draws a row of symbols per label; each symbol is worth key,
// and a half symbol is worth half of an even key.
func (d *drawer) pictureGraph(a Args) error {
	key, err := whole(a, "key", 1, 10)
	if err != nil {
		return err
	}
	rows, err := graphRows(a, 100)
	if err != nil {
		return err
	}
	const rh, lw, sym = 50.0, 110.0, 34.0
	most := 0
	for _, r := range rows {
		if !r.v.IsInt() {
			return fmt.Errorf("%s: picture graphs count whole things", r.label)
		}
		n := int(r.v.Num().Int64())
		halves := n % key
		if halves != 0 && (key%2 != 0 || halves != key/2) {
			return fmt.Errorf("%s: %d cannot be drawn with a key of %d", r.label, n, key)
		}
		most = max(most, (n+key-1)/key)
	}
	if most > 10 {
		return fmt.Errorf("a row needs %d symbols; at most 10 fit", most)
	}
	d.w = lw + float64(max(most, 4))*sym + 20
	d.h = float64(len(rows))*rh + 50
	d.label = fmt.Sprintf("a picture graph with %d rows", len(rows))
	for i, r := range rows {
		y := 10 + float64(i)*rh
		d.add(`<text class="fig-text" x="8" y="%s">%s</text>`, num(y+31), html.EscapeString(r.label))
		n := int(r.v.Num().Int64())
		for s := 0; s*key < n; s++ {
			cx, cy := lw+sym/2+float64(s)*sym, y+rh/2
			if n-s*key < key { // half a symbol
				d.add(`<path class="fig-mark" d="M %s %s A 14 14 0 0 1 %s %s Z"/>`, num(cx), num(cy-14), num(cx), num(cy+14))
				continue
			}
			d.add(`<circle class="fig-mark" cx="%s" cy="%s" r="14"/>`, num(cx), num(cy))
		}
	}
	ky := float64(len(rows))*rh + 34
	d.add(`<circle class="fig-mark" cx="%s" cy="%s" r="11"/>`, num(lw+12), num(ky-6))
	d.add(`<text class="fig-text fig-small" x="%s" y="%s">= %d</text>`, num(lw+30), num(ky), key)
	return nil
}

// barGraph draws vertical bars with a scale marked every step.
func (d *drawer) barGraph(a Args) error {
	step, err := whole(a, "scale", 1, 100)
	if err != nil {
		return err
	}
	rows, err := graphRows(a, 1000)
	if err != nil {
		return err
	}
	var top int64
	for _, r := range rows {
		if !r.v.IsInt() {
			return fmt.Errorf("%s: bars show whole numbers", r.label)
		}
		top = max(top, r.v.Num().Int64())
	}
	lines := (int(top) + step - 1) / step
	if lines < 2 {
		lines = 2
	}
	if lines > 12 {
		return fmt.Errorf("a scale of %d needs %d lines; at most 12 fit", step, lines)
	}
	const left, bottom, height, bw, gap = 52.0, 230.0, 200.0, 46.0, 26.0
	d.w = left + float64(len(rows))*(bw+gap) + 10
	d.h = bottom + 36
	d.label = fmt.Sprintf("a bar graph with %d bars", len(rows))
	unitH := height / float64(lines)
	for i := 0; i <= lines; i++ {
		y := bottom - float64(i)*unitH
		class := "fig-grid"
		if i == 0 {
			class = "fig-line"
		}
		d.add(`<line class="%s" x1="%s" y1="%s" x2="%s" y2="%s"/>`, class, num(left), num(y), num(d.w-6), num(y))
		d.add(`<text class="fig-text fig-small" x="%s" y="%s" text-anchor="end">%d</text>`, num(left-8), num(y+6), i*step)
	}
	d.add(`<line class="fig-line" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(left), num(bottom), num(left), num(bottom-height))
	for i, r := range rows {
		x := left + gap/2 + float64(i)*(bw+gap)
		v, _ := r.v.Float64()
		h := v / float64(step) * unitH
		if h > 0 {
			d.add(`<rect class="fig-fill fig-part" x="%s" y="%s" width="%s" height="%s"/>`, num(x), num(bottom-h), num(bw), num(h))
		}
		d.text(x+bw/2, bottom+26, "fig-small", r.label)
	}
	d.widen(340) // two bars alone would otherwise stand very tall
	return nil
}

// linePlot draws Xs stacked over a number line, from "value:count" marks.
func (d *drawer) linePlot(a Args) error {
	from, err := rat(a, "from")
	if err != nil {
		return err
	}
	to, err := rat(a, "to")
	if err != nil {
		return err
	}
	step, err := rat(a, "step")
	if err != nil {
		return err
	}
	if to.Cmp(from) <= 0 || step.Sign() <= 0 {
		return fmt.Errorf("the line must run forwards")
	}
	span := new(big.Rat).Sub(to, from)
	n := new(big.Rat).Quo(span, step)
	if !n.IsInt() || n.Num().Int64() > 16 {
		return fmt.Errorf("step %s does not fit from %s to %s in at most 16 ticks", step.RatString(), from.RatString(), to.RatString())
	}
	ticks := int(n.Num().Int64())
	counts := make([]int, ticks+1)
	most := 0
	for _, part := range strings.Split(a["marks"], "|") {
		vs, cs, ok := strings.Cut(part, ":")
		v, vok := new(big.Rat).SetString(strings.TrimSpace(vs))
		c, err := strconv.Atoi(strings.TrimSpace(cs))
		if !ok || !vok || err != nil || c < 0 || c > 8 {
			return fmt.Errorf("mark %q must be a value, a colon and a count from 0 to 8", part)
		}
		pos := new(big.Rat).Quo(new(big.Rat).Sub(v, from), step)
		if !pos.IsInt() || pos.Sign() < 0 || pos.Num().Int64() > int64(ticks) {
			return fmt.Errorf("mark %s is not on a tick", v.RatString())
		}
		counts[pos.Num().Int64()] += c
		most = max(most, counts[pos.Num().Int64()])
	}
	if most > 8 {
		return fmt.Errorf("at most 8 Xs over one value")
	}
	const left, right = 28.0, 372.0
	xh := 24.0
	y := 20 + float64(max(most, 3))*xh + 10
	d.w, d.h = 400, y+44
	d.label = "a line plot"
	d.add(`<line class="fig-line" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(left-12), num(y), num(right+12), num(y))
	for i := 0; i <= ticks; i++ {
		x := left + float64(i)*(right-left)/float64(ticks)
		v := new(big.Rat).Add(from, new(big.Rat).Mul(step, new(big.Rat).SetInt64(int64(i))))
		d.add(`<line class="fig-line" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(x), num(y-8), num(x), num(y+8))
		lbl := label(v)
		if !v.IsInt() {
			lbl = mixedLabel(v)
		}
		d.text(x, y+32, "fig-small", lbl)
		for k := range counts[i] {
			cy := y - 18 - float64(k)*xh
			d.text(x, cy+7, "fig-x", "×")
		}
	}
	return nil
}

// mixedLabel writes 5/4 as 1¼, compactly enough for a tick each: halves,
// quarters and eighths as single characters, anything else as 1 2/3.
func mixedLabel(r *big.Rat) string {
	if r.IsInt() {
		return r.Num().String()
	}
	w := new(big.Int).Quo(r.Num(), r.Denom())
	rest := new(big.Rat).Sub(r, new(big.Rat).SetInt(w))
	part, ok := vulgar[rest.RatString()]
	if !ok {
		part = " " + rest.RatString()
		if w.Sign() == 0 {
			return rest.RatString()
		}
	}
	if w.Sign() == 0 {
		return strings.TrimSpace(part)
	}
	return w.String() + part
}

var vulgar = map[string]string{
	"1/2": "½", "1/4": "¼", "3/4": "¾",
	"1/8": "⅛", "3/8": "⅜", "5/8": "⅝", "7/8": "⅞",
}

// grid draws a rectangle cut into rows and columns of unit squares, the
// first shaded ones filled (for area).
func (d *drawer) grid(a Args) error {
	rows, err := whole(a, "rows", 1, 12)
	if err != nil {
		return err
	}
	cols, err := whole(a, "cols", 1, 12)
	if err != nil {
		return err
	}
	shaded, err := whole(a, "shaded", 0, int64(rows*cols))
	if err != nil {
		return err
	}
	cell := min(36.0, 300/float64(max(rows, cols)))
	d.w, d.h = float64(cols)*cell+8, float64(rows)*cell+8
	d.label = fmt.Sprintf("a rectangle of %d rows and %d columns of squares", rows, cols)
	for i := range rows * cols {
		class := "fig-empty fig-square"
		if i < shaded {
			class = "fig-fill fig-part"
		}
		d.add(`<rect class="%s" x="%s" y="%s" width="%s" height="%s"/>`, class, num(4+float64(i%cols)*cell), num(4+float64(i/cols)*cell), num(cell), num(cell))
	}
	d.widen(200)
	return nil
}
