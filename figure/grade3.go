// SPDX-License-Identifier: Apache-2.0

package figure

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// The figures grade 3 adds: polygons with labelled sides (perimeter and
// composite area), a kitchen scale and a measuring jug, and a rectangle
// split into parts (area models; grade 4's multiplying too).

// numberList reads "3|4|5" into numbers, between lo and hi.
func numberList(a Args, key string, n0, n1 int, hi float64) ([]*big.Rat, error) {
	raw, ok := a[key]
	if !ok {
		return nil, fmt.Errorf("no %s", key)
	}
	var out []*big.Rat
	for _, s := range strings.Split(raw, "|") {
		r, ok := new(big.Rat).SetString(strings.TrimSpace(s))
		f, _ := r.Float64()
		if !ok || r.Sign() <= 0 || f > hi {
			return nil, fmt.Errorf("%s: %q must be a number more than 0 and at most %s", key, s, num(hi))
		}
		out = append(out, r)
	}
	if len(out) < n0 || len(out) > n1 {
		return nil, fmt.Errorf("%s has %d numbers; it takes %d to %d", key, len(out), n0, n1)
	}
	return out, nil
}

func f(r *big.Rat) float64 { v, _ := r.Float64(); return v }

// polygon draws a shape with every side labelled, or one side as "?".
// Rectangles, squares and L shapes are drawn to scale; triangles and the
// regular shapes are not (their labels give the lengths).
func (d *drawer) polygon(a Args) error {
	shape, err := choice(a, "shape", "rectangle", "square", "triangle", "pentagon", "hexagon", "L")
	if err != nil {
		return err
	}
	want := map[string]int{"rectangle": 2, "square": 1, "triangle": 3, "pentagon": 5, "hexagon": 6, "L": 6}[shape]
	sides, err := numberList(a, "sides", want, want, 1000)
	if err != nil {
		return err
	}
	hide, err := whole(a, "hide", -1, 5)
	if err != nil {
		return err
	}
	u := unit(a)
	lbl := func(r *big.Rat) string { return strings.TrimSpace(label(r) + " " + u) }
	// Points, clockwise from the top left, and the label of each side.
	var pts [][2]float64
	var labels []string
	switch shape {
	case "rectangle", "square":
		w, h := f(sides[0]), f(sides[0])
		if shape == "rectangle" {
			h = f(sides[1])
		}
		ratio := math.Min(math.Max(h/w, 0.3), 1.4)
		W, H := 240.0, 240*ratio
		pts = [][2]float64{{0, 0}, {W, 0}, {W, H}, {0, H}}
		lw, lh := lbl(sides[0]), lbl(sides[0])
		if shape == "rectangle" {
			lh = lbl(sides[1])
		}
		labels = []string{lw, lh, lw, lh}
	case "triangle":
		pts = [][2]float64{{110, 0}, {250, 190}, {0, 190}}
		for _, s := range sides {
			labels = append(labels, lbl(s))
		}
	case "pentagon", "hexagon":
		n := want
		for _, p := range regular(n) {
			pts = append(pts, [2]float64{120 + p[0]*110, 115 + p[1]*110})
		}
		for _, s := range sides {
			labels = append(labels, lbl(s))
		}
	case "L":
		// top, down, right, down, bottom (leftwards), up: a rectangle with its
		// top right corner cut away.
		t, dn1, r, dn2, b, up := f(sides[0]), f(sides[1]), f(sides[2]), f(sides[3]), f(sides[4]), f(sides[5])
		if new(big.Rat).Add(sides[0], sides[2]).Cmp(sides[4]) != 0 || new(big.Rat).Add(sides[1], sides[3]).Cmp(sides[5]) != 0 {
			return fmt.Errorf("an L's top and step must add to its bottom, and its two right sides to its left")
		}
		s := 240 / math.Max(b, up)
		pts = [][2]float64{{0, 0}, {t * s, 0}, {t * s, dn1 * s}, {(t + r) * s, dn1 * s}, {(t + r) * s, (dn1 + dn2) * s}, {0, up * s}}
		for _, x := range sides {
			labels = append(labels, lbl(x))
		}
	}
	if hide >= len(labels) {
		return fmt.Errorf("hide %d: the shape has %d sides", hide, len(labels))
	}
	if hide >= 0 {
		labels[hide] = "?"
	}
	// Centre the shape, leaving room for labels all round.
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range pts {
		minX, minY, maxX, maxY = math.Min(minX, p[0]), math.Min(minY, p[1]), math.Max(maxX, p[0]), math.Max(maxY, p[1])
	}
	const pad = 70.0
	var ps []string
	for i := range pts {
		pts[i] = [2]float64{pts[i][0] - minX + pad, pts[i][1] - minY + pad - 20}
		ps = append(ps, num(pts[i][0])+","+num(pts[i][1]))
	}
	d.w, d.h = maxX-minX+2*pad, maxY-minY+2*pad-40
	d.label = "a " + shape + " with its sides labelled"
	if shape == "L" {
		d.label = "an L-shaped figure with its sides labelled"
	}
	d.add(`<polygon class="fig-shape" points="%s"/>`, strings.Join(ps, " "))
	for i, l := range labels {
		p, q := pts[i], pts[(i+1)%len(pts)]
		mx, my := (p[0]+q[0])/2, (p[1]+q[1])/2
		// Every shape is drawn clockwise, so (dy, -dx) points out of it.
		nx, ny := q[1]-p[1], p[0]-q[0]
		ln := math.Hypot(nx, ny)
		nx, ny = nx/ln, ny/ln
		if shape == "L" && i == 1 {
			// The notch's two sides meet in a corner; label the down side
			// inside the shape so the two labels don't collide.
			nx, ny = -nx, -ny
		}
		off := 22.0 + 4*float64(len(l))*math.Abs(nx)
		d.text(mx+nx*off, my+ny*off+7, "", l)
	}
	return nil
}

// dial draws a kitchen scale reading value, marked every step up to max.
func (d *drawer) dial(a Args) error {
	mx, err := whole(a, "max", 2, 10000)
	if err != nil {
		return err
	}
	step, err := whole(a, "step", 1, 5000)
	if err != nil {
		return err
	}
	val, err := rat(a, "value")
	if err != nil {
		return err
	}
	if mx%step != 0 || mx/step > 20 || mx/step < 2 {
		return fmt.Errorf("step %d must split %d into 2 to 20 marks", step, mx)
	}
	v := f(val)
	if v < 0 || v > float64(mx) {
		return fmt.Errorf("value %s is off the dial", val.RatString())
	}
	u := unit(a)
	const c, r = 130.0, 110.0
	d.w, d.h = 260, 262
	d.label = "a kitchen scale with a needle"
	d.add(`<rect class="fig-shape fig-alt2" x="10" y="226" width="240" height="30" rx="8"/>`)
	d.add(`<circle class="fig-face" cx="130" cy="130" r="%s"/>`, num(r))
	// The marks run clockwise from the top, round 300 degrees.
	ang := func(x float64) float64 { return -150 + 300*x/float64(mx) }
	marks := mx / step
	labelEvery := 1
	if marks > 10 {
		labelEvery = 2
		if marks%5 == 0 {
			labelEvery = 5
		}
	}
	for i := 0; i <= marks; i++ {
		t := ang(float64(i*step)) * math.Pi / 180
		s, co := math.Sincos(t)
		in := r - 12
		if i%labelEvery == 0 {
			in = r - 20
			d.text(c+s*(r-38), c-co*(r-38)+6, "fig-small", strconv.Itoa(i*step))
		}
		d.add(`<line class="fig-tick" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(c+s*in), num(c-co*in), num(c+s*(r-4)), num(c-co*(r-4)))
	}
	t := ang(v) * math.Pi / 180
	s, co := math.Sincos(t)
	d.add(`<line class="fig-hand fig-hand-minute" x1="130" y1="130" x2="%s" y2="%s"/>`, num(c+s*(r-24)), num(c-co*(r-24)))
	d.add(`<circle class="fig-pin" cx="130" cy="130" r="7"/>`)
	if u != "" {
		d.text(130, 190, "fig-small", u)
	}
	return nil
}

// jug draws a measuring jug filled to value, marked every step up to max.
func (d *drawer) jug(a Args) error {
	mx, err := whole(a, "max", 2, 10000)
	if err != nil {
		return err
	}
	step, err := whole(a, "step", 1, 5000)
	if err != nil {
		return err
	}
	val, err := rat(a, "value")
	if err != nil {
		return err
	}
	if mx%step != 0 || mx/step > 20 || mx/step < 2 {
		return fmt.Errorf("step %d must split %d into 2 to 20 marks", step, mx)
	}
	v := f(val)
	if v < 0 || v > float64(mx) {
		return fmt.Errorf("value %s is off the jug", val.RatString())
	}
	u := unit(a)
	const x0, x1, top, bottom = 70.0, 190.0, 30.0, 270.0
	d.w, d.h = 260, 290
	d.label = "a measuring jug with water in it"
	level := bottom - (bottom-top)*v/float64(mx)
	if v > 0 {
		d.add(`<rect class="fig-water" x="%s" y="%s" width="%s" height="%s"/>`, num(x0), num(level), num(x1-x0), num(bottom-level))
	}
	d.add(`<path class="fig-line" d="M %s %s L %s %s L %s %s L %s %s"/>`, num(x0), num(top-14), num(x0), num(bottom), num(x1), num(bottom), num(x1), num(top-14))
	d.add(`<path class="fig-line" d="M %s %s Q %s %s %s %s"/>`, num(x1), num(top+20), num(x1+40), num(top+60), num(x1), num(top+110))
	marks := mx / step
	labelEvery := 1
	if marks > 10 {
		labelEvery = 2
	}
	for i := 0; i <= marks; i++ {
		y := bottom - (bottom-top)*float64(i)/float64(marks)
		w := 12.0
		if i%labelEvery == 0 {
			w = 20
			d.add(`<text class="fig-text fig-small" x="%s" y="%s" text-anchor="end">%d</text>`, num(x0-8), num(y+6), i*step)
		}
		d.add(`<line class="fig-tick" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(x0), num(y), num(x0+w), num(y))
	}
	if u != "" {
		d.text(130, 20, "fig-small", u)
	}
	return nil
}

// splitRectangle draws an area model: columns and rows of given sizes,
// labelled outside, and with areas = 1 each part's area written inside.
func (d *drawer) splitRectangle(a Args) error {
	cols, err := numberList(a, "cols", 1, 3, 1000)
	if err != nil {
		return err
	}
	rows, err := numberList(a, "rows", 1, 3, 1000)
	if err != nil {
		return err
	}
	areas, err := whole(a, "areas", 0, 1)
	if err != nil {
		return err
	}
	if len(cols)*len(rows) < 2 {
		return fmt.Errorf("an area model needs at least two parts")
	}
	// Part sizes follow the numbers, but no part is drawn thinner than 56.
	sizes := func(xs []*big.Rat, total float64) []float64 {
		var sum float64
		for _, x := range xs {
			sum += f(x)
		}
		out := make([]float64, len(xs))
		var used float64
		for i, x := range xs {
			out[i] = math.Max(56, total*f(x)/sum)
			used += out[i]
		}
		for i := range out {
			out[i] *= total / used
		}
		return out
	}
	const left, top, W, H = 62.0, 44.0, 260.0, 180.0
	cw, rh := sizes(cols, W), sizes(rows, H)
	d.w, d.h = left+W+16, top+H+12
	d.label = fmt.Sprintf("a rectangle split into %d parts", len(cols)*len(rows))
	y := top
	for j, r := range rows {
		x := left
		for i, c := range cols {
			class := "fig-empty"
			if (i+j)%2 == 1 {
				class = "fig-alt fig-part"
			}
			d.add(`<rect class="%s fig-square" x="%s" y="%s" width="%s" height="%s"/>`, class, num(x), num(y), num(cw[i]), num(rh[j]))
			if areas == 1 {
				d.text(x+cw[i]/2, y+rh[j]/2+7, "", label(new(big.Rat).Mul(c, r)))
			}
			if j == 0 {
				d.text(x+cw[i]/2, top-14, "", label(c))
			}
			x += cw[i]
		}
		d.add(`<text class="fig-text" x="%s" y="%s" text-anchor="end">%s</text>`, num(left-10), num(y+rh[j]/2+7), label(r))
		y += rh[j]
	}
	return nil
}
