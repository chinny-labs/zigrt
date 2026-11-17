// SPDX-License-Identifier: Apache-2.0

package figure

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// The figures kindergarten needs (the syllabus's "needs"): dots, ten
// frames, flat and solid shapes, comparing sizes, a balance, and where a
// ball is. Their labels describe without giving the answer away: a shape is
// "a flat shape", never its name.

// choice reads a word argument that must be one of the options.
func choice(a Args, key string, options ...string) (string, error) {
	s, ok := a[key]
	if !ok {
		return "", fmt.Errorf("no %s", key)
	}
	s = strings.TrimSpace(s)
	if !slices.Contains(options, s) {
		return "", fmt.Errorf("%s is %q; it must be one of %s", key, s, strings.Join(options, ", "))
	}
	return s, nil
}

func (d *drawer) dot(x, y float64, class string) {
	d.add(`<circle class="%s" cx="%s" cy="%s" r="11"/>`, class, num(x), num(y))
}

// cross strikes out a dot, for taking away.
func (d *drawer) cross(x, y float64) {
	d.add(`<line class="fig-cross" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(x-14), num(y-14), num(x+14), num(y+14))
	d.add(`<line class="fig-cross" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(x-14), num(y+14), num(x+14), num(y-14))
}

// scatter places n dots in a w×h box without overlap, the same way every
// time for the same n, so a figure is repeatable.
func scatter(n int, w, h float64) [][2]float64 {
	var out [][2]float64
	seed := uint32(n)*2654435761 + 12345 //nolint:gosec // n is at most 20
	next := func() float64 {
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		return float64(seed%10000) / 10000
	}
	for tries := 0; len(out) < n && tries < 20000; tries++ {
		p := [2]float64{18 + next()*(w-36), 18 + next()*(h-36)}
		ok := true
		for _, q := range out {
			if math.Hypot(p[0]-q[0], p[1]-q[1]) < 34 {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, p)
		}
	}
	return out
}

// dice are the familiar arrangements of 1 to 6, on a 3×3 grid.
var dice = map[int][][2]int{
	1: {{1, 1}},
	2: {{0, 0}, {2, 2}},
	3: {{0, 0}, {1, 1}, {2, 2}},
	4: {{0, 0}, {2, 0}, {0, 2}, {2, 2}},
	5: {{0, 0}, {2, 0}, {1, 1}, {0, 2}, {2, 2}},
	6: {{0, 0}, {2, 0}, {0, 1}, {2, 1}, {0, 2}, {2, 2}},
}

func (d *drawer) dots(a Args) error {
	n, err := whole(a, "count", 0, 20)
	if err != nil {
		return err
	}
	arr, err := choice(a, "arrange", "line", "scatter", "dice")
	if err != nil {
		return err
	}
	d.label = fmt.Sprintf("%d %s", n, plural(n, "dot", "dots"))
	switch arr {
	case "dice":
		if n < 1 || n > 6 {
			return fmt.Errorf("dice show 1 to 6 dots, not %d", n)
		}
		d.w, d.h = 140, 140
		d.add(`<rect class="fig-face" x="6" y="6" width="128" height="128" rx="22"/>`)
		for _, p := range dice[n] {
			d.dot(30+float64(p[0])*40, 30+float64(p[1])*40, "fig-dot")
		}
	case "line":
		per := min(max(n, 1), 10)
		rows := (n + 9) / 10
		d.w, d.h = float64(per)*34+16, float64(max(rows, 1))*34+16
		for i := range n {
			d.dot(25+float64(i%10)*34, 25+float64(i/10)*34, "fig-dot")
		}
	case "scatter":
		d.w, d.h = 320, 170
		pts := scatter(n, d.w, d.h)
		if len(pts) < n {
			return fmt.Errorf("could not place %d dots", n)
		}
		for _, p := range pts {
			d.dot(p[0], p[1], "fig-dot")
		}
	}
	return nil
}

// dotGroups draws a and b dots: apart (two groups to put together), rows
// (one above the other, lined up, to compare), or crossed (a dots with the
// last b struck out, to take away).
func (d *drawer) dotGroups(a Args) error {
	x, err := whole(a, "a", 0, 10)
	if err != nil {
		return err
	}
	y, err := whole(a, "b", 0, 10)
	if err != nil {
		return err
	}
	style, err := choice(a, "style", "apart", "rows", "crossed")
	if err != nil {
		return err
	}
	const gap = 34.0
	switch style {
	case "apart":
		// Each group in rows of five, side by side with a clear space.
		group := func(n int, ox float64) float64 {
			for i := range n {
				d.dot(ox+float64(i%5)*gap, 25+float64(i/5)*gap, "fig-dot")
			}
			return float64(min(max(n, 1), 5)) * gap
		}
		w1 := group(x, 25)
		w2 := group(y, 25+w1+40)
		d.w, d.h = w1+w2+56, 2*gap+24
		d.label = fmt.Sprintf("a group of %d %s and a group of %d %s", x, plural(x, "dot", "dots"), y, plural(y, "dot", "dots"))
	case "rows":
		for i := range x {
			d.dot(25+float64(i)*gap, 25, "fig-dot")
		}
		for i := range y {
			d.dot(25+float64(i)*gap, 25+gap+12, "fig-fill")
		}
		d.w, d.h = float64(max(x, y, 1))*gap+16, 2*gap+28
		d.label = "two rows of dots"
	case "crossed":
		if y > x {
			return fmt.Errorf("cannot cross out %d of %d dots", y, x)
		}
		for i := range x {
			px, py := 25+float64(i%5)*gap, 25+float64(i/5)*gap
			if i >= x-y {
				d.dot(px, py, "fig-empty")
				d.cross(px, py)
			} else {
				d.dot(px, py, "fig-dot")
			}
		}
		d.w, d.h = float64(min(max(x, 1), 5))*gap+16, float64((x+4)/5)*gap+16
		d.label = fmt.Sprintf("%d dots, %d crossed out", x, y)
	}
	return nil
}

// tenFrame draws one or two ten frames filled in order, the first frame
// before the second.
func (d *drawer) tenFrame(a Args) error {
	frames, err := whole(a, "frames", 1, 2)
	if err != nil {
		return err
	}
	filled, err := whole(a, "filled", 0, int64(10*frames))
	if err != nil {
		return err
	}
	const cell, pad, between = 48.0, 8.0, 18.0
	d.w = 5*cell + 2*pad
	d.h = float64(frames)*(2*cell) + float64(frames-1)*between + 2*pad
	d.label = fmt.Sprintf("%d ten %s with %d counters", frames, plural(frames, "frame", "frames"), filled)
	for f := range frames {
		oy := pad + float64(f)*(2*cell+between)
		d.add(`<rect class="fig-frame" x="%s" y="%s" width="%s" height="%s"/>`, num(pad), num(oy), num(5*cell), num(2*cell))
		for i := range 10 {
			cx, cy := pad+float64(i%5)*cell, oy+float64(i/5)*cell
			d.add(`<rect class="fig-cell" x="%s" y="%s" width="%s" height="%s"/>`, num(cx), num(cy), num(cell), num(cell))
			if f*10+i < filled {
				d.add(`<circle class="fig-dot" cx="%s" cy="%s" r="16"/>`, num(cx+cell/2), num(cy+cell/2))
			}
		}
	}
	return nil
}

// Flat shapes, as points on a unit-ish outline centred on 0, before turning.
var flat = map[string][][2]float64{
	"triangle":      {{0, -1}, {0.95, 0.75}, {-0.95, 0.75}},
	"square":        {{-0.8, -0.8}, {0.8, -0.8}, {0.8, 0.8}, {-0.8, 0.8}},
	"rectangle":     {{-1, -0.55}, {1, -0.55}, {1, 0.55}, {-1, 0.55}},
	"trapezoid":     {{-0.5, -0.6}, {0.5, -0.6}, {1, 0.6}, {-1, 0.6}},
	"rhombus":       {{0, -1}, {0.65, 0}, {0, 1}, {-0.65, 0}},
	"parallelogram": {{-0.6, -0.55}, {1, -0.55}, {0.6, 0.55}, {-1, 0.55}},
	"pentagon":      regular(5),
	"hexagon":       regular(6),
	"octagon":       regular(8),
}

// FlatShapes lists the flat shapes the shape figure draws.
var FlatShapes = append(slices.Sorted(func(yield func(string) bool) {
	for k := range flat {
		if !yield(k) {
			return
		}
	}
}), "circle")

func regular(n int) [][2]float64 {
	out := make([][2]float64, n)
	for i := range n {
		ang := -math.Pi/2 + float64(i)*2*math.Pi/float64(n)
		if n%2 == 0 {
			ang += math.Pi / float64(n)
		}
		out[i] = [2]float64{math.Cos(ang), math.Sin(ang)}
	}
	return out
}

func turned(pts [][2]float64, deg float64, cx, cy, r float64) string {
	s, c := math.Sincos(deg * math.Pi / 180)
	var b []string
	for _, p := range pts {
		x, y := p[0]*c-p[1]*s, p[0]*s+p[1]*c
		b = append(b, num(cx+x*r)+","+num(cy+y*r))
	}
	return strings.Join(b, " ")
}

func (d *drawer) shape(a Args) error {
	name, err := choice(a, "name", FlatShapes...)
	if err != nil {
		return err
	}
	turn, err := whole(a, "turn", 0, 359)
	if err != nil {
		return err
	}
	d.w, d.h, d.label = 220, 220, "a flat shape"
	if name == "circle" {
		d.add(`<circle class="fig-shape" cx="110" cy="110" r="88"/>`)
		return nil
	}
	d.add(`<polygon class="fig-shape" points="%s"/>`, turned(flat[name], float64(turn), 110, 110, 92))
	return nil
}

// shapeMix draws circles, triangles and squares in a jumbled row, to sort
// and count.
func (d *drawer) shapeMix(a Args) error {
	var kinds []string
	for _, k := range []string{"circles", "triangles", "squares"} {
		n, err := whole(a, k, 0, 8)
		if err != nil {
			return err
		}
		for range n {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 0 || len(kinds) > 15 {
		return fmt.Errorf("%d shapes; draw 1 to 15", len(kinds))
	}
	// A fixed shuffle, so the same counts always look the same.
	seed := uint32(len(kinds))*40503 + 7 //nolint:gosec // at most 15 shapes
	for i := len(kinds) - 1; i > 0; i-- {
		seed = seed*1103515245 + 12345
		j := int(seed>>16) % (i + 1)
		kinds[i], kinds[j] = kinds[j], kinds[i]
	}
	const cell = 60.0
	per := min(len(kinds), 5)
	d.w, d.h = float64(per)*cell+20, float64((len(kinds)+4)/5)*cell+20
	d.label = fmt.Sprintf("%d shapes", len(kinds))
	for i, k := range kinds {
		cx, cy := 10+cell/2+float64(i%5)*cell, 10+cell/2+float64(i/5)*cell
		switch k {
		case "circles":
			d.add(`<circle class="fig-shape" cx="%s" cy="%s" r="22"/>`, num(cx), num(cy))
		case "triangles":
			d.add(`<polygon class="fig-shape fig-alt" points="%s"/>`, turned(flat["triangle"], 0, cx, cy+3, 25))
		case "squares":
			d.add(`<polygon class="fig-shape fig-alt2" points="%s"/>`, turned(flat["square"], 0, cx, cy, 25))
		}
	}
	return nil
}

// SolidShapes lists the solids the solid figure draws.
var SolidShapes = []string{"box", "cone", "cube", "cylinder", "pyramid", "sphere"}

func (d *drawer) solid(a Args) error {
	name, err := choice(a, "name", SolidShapes...)
	if err != nil {
		return err
	}
	d.w, d.h, d.label = 240, 220, "a solid shape"
	poly := func(class string, p ...float64) {
		var s []string
		for i := 0; i+1 < len(p); i += 2 {
			s = append(s, num(p[i])+","+num(p[i+1]))
		}
		d.add(`<polygon class="%s" points="%s"/>`, class, strings.Join(s, " "))
	}
	hidden := func(x1, y1, x2, y2 float64) {
		d.add(`<line class="fig-hidden" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(x1), num(y1), num(x2), num(y2))
	}
	switch name {
	case "cube", "box":
		w, h, dp := 120.0, 120.0, 50.0
		if name == "box" {
			w, h = 160, 90
		}
		x0, y0 := (240-w-dp)/2, (220-h+dp)/2
		hidden(x0+dp, y0-dp+h, x0+dp, y0-dp)
		hidden(x0+dp, y0-dp+h, x0, y0+h)
		hidden(x0+dp, y0-dp+h, x0+dp+w, y0-dp+h)
		poly("fig-shape", x0, y0, x0+w, y0, x0+w, y0+h, x0, y0+h)
		poly("fig-shape fig-top", x0, y0, x0+dp, y0-dp, x0+w+dp, y0-dp, x0+w, y0)
		poly("fig-shape fig-side", x0+w, y0, x0+w+dp, y0-dp, x0+w+dp, y0+h-dp, x0+w, y0+h)
	case "cylinder":
		d.add(`<path class="fig-shape" d="M 60 50 L 60 170 A 60 18 0 0 0 180 170 L 180 50"/>`)
		d.add(`<path class="fig-hidden" d="M 60 170 A 60 18 0 0 1 180 170"/>`)
		d.add(`<ellipse class="fig-shape fig-top" cx="120" cy="50" rx="60" ry="18"/>`)
	case "cone":
		d.add(`<path class="fig-shape" d="M 120 25 L 180 170 A 60 18 0 0 1 60 170 Z"/>`)
		d.add(`<path class="fig-hidden" d="M 60 170 A 60 18 0 0 1 180 170"/>`)
	case "pyramid":
		hidden(60, 165, 105, 135)
		hidden(105, 135, 120, 30)
		hidden(105, 135, 190, 135)
		poly("fig-shape", 60, 165, 145, 165, 120, 30)
		poly("fig-shape fig-side", 145, 165, 190, 135, 120, 30)
	case "sphere":
		d.add(`<circle class="fig-shape" cx="120" cy="110" r="85"/>`)
		d.add(`<path class="fig-hidden" d="M 35 110 A 85 24 0 0 1 205 110"/>`)
		d.add(`<path class="fig-line fig-thin" d="M 35 110 A 85 24 0 0 0 205 110"/>`)
	}
	return nil
}

// compare draws two or three things of different sizes: bars for length
// (left-aligned, one above another) or towers for height (side by side on
// the ground). c = 0 draws two.
func (d *drawer) compare(a Args) error {
	var sizes []int
	for _, k := range []string{"a", "b", "c"} {
		lo := int64(1)
		if k == "c" {
			lo = 0
		}
		n, err := whole(a, k, lo, 10)
		if err != nil {
			return err
		}
		if n > 0 {
			sizes = append(sizes, n)
		}
	}
	kind, err := choice(a, "kind", "length", "height")
	if err != nil {
		return err
	}
	names := []string{"A", "B", "C"}
	classes := []string{"fig-fill", "fig-mark", "fig-alt"}
	switch kind {
	case "length":
		d.w, d.h = 380, float64(len(sizes))*56+12
		d.label = fmt.Sprintf("%d bars of different lengths, named A to %s", len(sizes), names[len(sizes)-1])
		for i, n := range sizes {
			y := 12 + float64(i)*56
			d.text(18, y+26, "", names[i])
			d.add(`<rect class="%s" x="40" y="%s" width="%s" height="34" rx="17"/>`, classes[i], num(y), num(float64(n)*32))
		}
	case "height":
		d.w, d.h = float64(len(sizes))*90+30, 380
		d.label = fmt.Sprintf("%d towers of different heights, named A to %s", len(sizes), names[len(sizes)-1])
		d.add(`<line class="fig-line" x1="6" y1="340" x2="%s" y2="340"/>`, num(d.w-6))
		for i, n := range sizes {
			x := 30 + float64(i)*90
			hgt := float64(n) * 31
			d.add(`<rect class="%s" x="%s" y="%s" width="54" height="%s" rx="8"/>`, classes[i], num(x), num(340-hgt), num(hgt))
			d.text(x+27, 368, "", names[i])
		}
	}
	return nil
}

// balance draws a pan balance with a box on the left and a ball on the
// right, tipped towards the heavier one.
func (d *drawer) balance(a Args) error {
	tip, err := choice(a, "tip", "left", "right", "level")
	if err != nil {
		return err
	}
	d.w, d.h, d.label = 340, 230, "a balance with a box on the left and a ball on the right"
	tilt := map[string]float64{"left": 16, "right": -16, "level": 0}[tip]
	cx, cy := 170.0, 80.0
	lx, ly, rx, ry := cx-120, cy+tilt, cx+120, cy-tilt
	d.add(`<polygon class="fig-shape" points="150,210 190,210 170,%s"/>`, num(cy))
	d.add(`<line class="fig-line" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(lx), num(ly), num(rx), num(ry))
	for _, p := range [][2]float64{{lx, ly}, {rx, ry}} {
		d.add(`<line class="fig-line fig-thin" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(p[0]), num(p[1]), num(p[0]), num(p[1]+40))
		d.add(`<path class="fig-shape" d="M %s %s Q %s %s %s %s Z"/>`, num(p[0]-46), num(p[1]+40), num(p[0]), num(p[1]+66), num(p[0]+46), num(p[1]+40))
	}
	d.add(`<rect class="fig-shape fig-alt" x="%s" y="%s" width="44" height="40" rx="4"/>`, num(lx-22), num(ly))
	d.add(`<circle class="fig-mark" cx="%s" cy="%s" r="20"/>`, num(rx), num(ry+19))
	return nil
}

// position draws a ball above, below, beside or inside a box.
func (d *drawer) position(a Args) error {
	place, err := choice(a, "place", "above", "below", "left", "right", "inside")
	if err != nil {
		return err
	}
	d.w, d.h, d.label = 320, 300, "a ball and a box"
	const bx, by, bw, bh = 110.0, 110.0, 100.0, 80.0
	if place == "inside" {
		// An open box: no lid, the ball sitting in it.
		d.add(`<path class="fig-shape" d="M %s %s L %s %s L %s %s L %s %s"/>`, num(bx), num(by), num(bx), num(by+bh), num(bx+bw), num(by+bh), num(bx+bw), num(by))
		d.add(`<circle class="fig-mark" cx="%s" cy="%s" r="24"/>`, num(bx+bw/2), num(by+bh-26))
		return nil
	}
	d.add(`<rect class="fig-shape fig-alt" x="%s" y="%s" width="%s" height="%s" rx="4"/>`, num(bx), num(by), num(bw), num(bh))
	ball := map[string][2]float64{
		"above": {bx + bw/2, by - 50}, "below": {bx + bw/2, by + bh + 50},
		"left": {bx - 50, by + bh/2}, "right": {bx + bw + 50, by + bh/2},
	}[place]
	d.add(`<circle class="fig-mark" cx="%s" cy="%s" r="24"/>`, num(ball[0]), num(ball[1]))
	return nil
}
