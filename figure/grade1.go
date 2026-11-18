// SPDX-License-Identifier: Apache-2.0

package figure

import (
	"fmt"
	"html"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// The figures grade 1 adds: base-ten blocks, a ruler (or a row of unit
// cubes), a table or tally chart, and a shape cut into parts.

// baseTen draws hundreds flats, tens rods and ones cubes, left to right.
func (d *drawer) baseTen(a Args) error {
	h, err := whole(a, "hundreds", 0, 9)
	if err != nil {
		return err
	}
	t, err := whole(a, "tens", 0, 19)
	if err != nil {
		return err
	}
	o, err := whole(a, "ones", 0, 19)
	if err != nil {
		return err
	}
	if h+t+o == 0 {
		return fmt.Errorf("no blocks to draw")
	}
	const u = 12.0 // one cube
	x, top := 8.0, 8.0
	d.label = "hundreds, tens and ones blocks"
	for range h {
		d.add(`<rect class="fig-block fig-alt2" x="%s" y="%s" width="%s" height="%s"/>`, num(x), num(top), num(10*u), num(10*u))
		for i := 1; i < 10; i++ {
			d.add(`<line class="fig-block-line" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(x+float64(i)*u), num(top), num(x+float64(i)*u), num(top+10*u))
			d.add(`<line class="fig-block-line" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(x), num(top+float64(i)*u), num(x+10*u), num(top+float64(i)*u))
		}
		x += 10*u + 10
	}
	for range t {
		d.add(`<rect class="fig-block fig-rod" x="%s" y="%s" width="%s" height="%s"/>`, num(x), num(top), num(u), num(10*u))
		for i := 1; i < 10; i++ {
			d.add(`<line class="fig-block-line" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(x), num(top+float64(i)*u), num(x+u), num(top+float64(i)*u))
		}
		x += u + 7
	}
	if o > 0 {
		x += 6
		// Ones in columns of five, from the bottom up, like a stack.
		for i := range o {
			cx := x + float64(i/5)*(u+7)
			cy := top + 10*u - float64(i%5+1)*(u+3)
			d.add(`<rect class="fig-block fig-alt" x="%s" y="%s" width="%s" height="%s"/>`, num(cx), num(cy), num(u), num(u))
		}
		x += float64((o+4)/5) * (u + 7)
	}
	d.w, d.h = x+4, 10*u+16
	d.widen(240) // a few rods would otherwise fill the screen's height
	return nil
}

// ruler draws a pencil above a ruler, or above a row of unit cubes. The
// pencil may start away from 0, the classic mistake to look out for.
func (d *drawer) ruler(a Args) error {
	max, err := whole(a, "max", 2, 15)
	if err != nil {
		return err
	}
	start, err := rat(a, "start")
	if err != nil {
		return err
	}
	length, err := rat(a, "length")
	if err != nil {
		return err
	}
	unitName := unit(a)
	quarter := new(big.Rat).SetFrac64(1, 4)
	if !new(big.Rat).Quo(length, quarter).IsInt() || !new(big.Rat).Quo(start, quarter).IsInt() {
		return fmt.Errorf("start and length must be whole numbers, halves or quarters")
	}
	end := new(big.Rat).Add(start, length)
	if start.Sign() < 0 || length.Sign() <= 0 || end.Cmp(new(big.Rat).SetInt64(int64(max))) > 0 {
		return fmt.Errorf("a pencil from %s, %s long, does not fit on a ruler to %d", start.RatString(), length.RatString(), max)
	}
	fs, _ := start.Float64()
	fl, _ := length.Float64()
	const left, width = 24.0, 352.0
	px := width / float64(max)
	d.w, d.h = 400, 150
	d.label = "a pencil above a ruler"
	// The pencil: a body and a point.
	x0, x1, y := left+fs*px, left+(fs+fl)*px, 30.0
	tip := math.Min(18, (x1-x0)/3)
	d.add(`<path class="fig-shape fig-alt" d="M %s %s L %s %s L %s %s L %s %s L %s %s Z"/>`,
		num(x0), num(y-12), num(x1-tip), num(y-12), num(x1), num(y), num(x1-tip), num(y+12), num(x0), num(y+12))
	if unitName == "cubes" {
		if !start.IsInt() || start.Sign() != 0 || !length.IsInt() {
			return fmt.Errorf("cubes measure whole lengths from 0")
		}
		d.label = "a pencil above a row of cubes"
		n := int(length.Num().Int64())
		for i := range n {
			d.add(`<rect class="fig-shape fig-fill" x="%s" y="62" width="%s" height="%s"/>`, num(left+float64(i)*px), num(px), num(math.Min(px, 40)))
		}
		d.h = 62 + math.Min(px, 40) + 12
		return nil
	}
	// The ruler: a strip with ticks every quarter, numbers on the wholes.
	d.add(`<rect class="fig-shape" x="%s" y="60" width="%s" height="62" rx="4"/>`, num(left-12), num(width+24))
	for i := 0; i <= max*4; i++ {
		x := left + float64(i)*px/4
		h := 9.0
		switch {
		case i%4 == 0:
			h = 22
		case i%2 == 0:
			h = 15
		}
		d.add(`<line class="fig-tick" x1="%s" y1="60" x2="%s" y2="%s"/>`, num(x), num(x), num(60+h))
		if i%4 == 0 {
			size := "" // past 10 units, two-digit labels need the smaller size
			if max > 10 {
				size = "fig-small"
			}
			d.text(x, 108, size, strconv.Itoa(i/4))
		}
	}
	if unitName != "" {
		d.text(left+width-6, 140, "fig-small", unitName)
		d.h = 150
	}
	return nil
}

// table draws rows of "label:count" separated by |, as numbers or tally
// marks.
func (d *drawer) table(a Args) error {
	raw, ok := a["rows"]
	if !ok {
		return fmt.Errorf("no rows")
	}
	style, err := choice(a, "style", "number", "tally")
	if err != nil {
		return err
	}
	type row struct {
		label string
		n     int
	}
	var rows []row
	for _, part := range strings.Split(raw, "|") {
		label, count, ok := strings.Cut(part, ":")
		label = strings.TrimSpace(label)
		n, err := strconv.Atoi(strings.TrimSpace(count))
		if !ok || err != nil || label == "" || len(label) > 14 || n < 0 || n > 25 {
			return fmt.Errorf("row %q must be a short label, a colon and a count from 0 to 25", part)
		}
		rows = append(rows, row{label, n})
	}
	if len(rows) < 2 || len(rows) > 5 {
		return fmt.Errorf("%d rows; a table has 2 to 5", len(rows))
	}
	const rh, lw, vw = 46.0, 150.0, 190.0
	d.w, d.h = lw+vw+8, float64(len(rows))*rh+8
	d.label = fmt.Sprintf("a table with %d rows", len(rows))
	d.add(`<rect class="fig-face" x="4" y="4" width="%s" height="%s" rx="6"/>`, num(lw+vw), num(float64(len(rows))*rh))
	d.add(`<line class="fig-line fig-thin" x1="%s" y1="4" x2="%s" y2="%s"/>`, num(4+lw), num(4+lw), num(4+float64(len(rows))*rh))
	for i, r := range rows {
		y := 4 + float64(i)*rh
		if i > 0 {
			d.add(`<line class="fig-line fig-thin" x1="4" y1="%s" x2="%s" y2="%s"/>`, num(y), num(4+lw+vw), num(y))
		}
		d.add(`<text class="fig-text" x="16" y="%s">%s</text>`, num(y+30), html.EscapeString(r.label))
		if style == "number" {
			d.text(4+lw+vw/2, y+30, "", strconv.Itoa(r.n))
			continue
		}
		// Tally: four strokes and a fifth across, in groups.
		x := 4 + lw + 14
		for g := 0; g < r.n; g += 5 {
			k := min(5, r.n-g)
			for s := range min(k, 4) {
				sx := x + float64(s)*8
				d.add(`<line class="fig-tick" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(sx), num(y+10), num(sx), num(y+36))
			}
			if k == 5 {
				d.add(`<line class="fig-tick" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(x-4), num(y+32), num(x+28), num(y+14))
			}
			x += 44
		}
	}
	return nil
}

// cutShape draws a circle, square or rectangle cut into parts, equal or
// not, with the first shaded parts filled.
func (d *drawer) cutShape(a Args) error {
	name, err := choice(a, "name", "circle", "square", "rectangle")
	if err != nil {
		return err
	}
	parts, err := whole(a, "parts", 2, 8)
	if err != nil {
		return err
	}
	equal, err := whole(a, "equal", 0, 1)
	if err != nil {
		return err
	}
	shaded, err := whole(a, "shaded", 0, int64(parts))
	if err != nil {
		return err
	}
	weights := make([]float64, parts)
	for i := range weights {
		weights[i] = 1
	}
	if equal == 0 {
		uneven := map[int][]float64{2: {1, 2}, 3: {1, 1, 2}, 4: {1, 2, 1, 3}}[parts]
		if uneven == nil {
			return fmt.Errorf("unequal parts are drawn for 2 to 4 parts, not %d", parts)
		}
		weights = uneven
	}
	var sum float64
	for _, w := range weights {
		sum += w
	}
	class := func(i int) string {
		if i < shaded {
			return "fig-fill fig-part"
		}
		return "fig-empty"
	}
	d.label = fmt.Sprintf("a %s cut into %d parts", name, parts)
	if name == "circle" {
		const c, r = 110.0, 96.0
		d.w, d.h = 220, 220
		ang := -math.Pi / 2
		for i, w := range weights {
			next := ang + 2*math.Pi*w/sum
			large := 0
			if next-ang > math.Pi {
				large = 1
			}
			d.add(`<path class="%s" d="M %s %s L %s %s A %s %s 0 %d 1 %s %s Z"/>`, class(i), num(c), num(c),
				num(c+r*math.Cos(ang)), num(c+r*math.Sin(ang)), num(r), num(r), large, num(c+r*math.Cos(next)), num(c+r*math.Sin(next)))
			ang = next
		}
		d.add(`<circle class="fig-line" cx="110" cy="110" r="96"/>`)
		return nil
	}
	w, h := 200.0, 200.0
	if name == "rectangle" {
		w, h = 300, 150
	}
	d.w, d.h = w+16, h+16
	x := 8.0
	for i, wt := range weights {
		pw := w * wt / sum
		d.add(`<rect class="%s" x="%s" y="8" width="%s" height="%s"/>`, class(i), num(x), num(pw), num(h))
		x += pw
	}
	d.add(`<rect class="fig-line" x="8" y="8" width="%s" height="%s"/>`, num(w), num(h))
	return nil
}
