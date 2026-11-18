// SPDX-License-Identifier: Apache-2.0

package figure

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The figures grade 4 adds: points, lines and rays; angles (one, or two
// side by side); a protractor; a shape with a line through it, for
// symmetry; and triangles by kind, with equal sides and right angles marked.

// rot turns (x, y) about (cx, cy) by deg degrees, clockwise on screen.
func rot(x, y, cx, cy, deg float64) (float64, float64) {
	s, c := math.Sincos(deg * math.Pi / 180)
	dx, dy := x-cx, y-cy
	return cx + dx*c - dy*s, cy + dx*s + dy*c
}

func (d *drawer) seg(class string, x1, y1, x2, y2 float64) {
	d.add(`<line class="%s" x1="%s" y1="%s" x2="%s" y2="%s"/>`, class, num(x1), num(y1), num(x2), num(y2))
}

// arrow draws an arrowhead at (x, y) pointing from (fx, fy).
func (d *drawer) arrow(fx, fy, x, y float64) {
	a := math.Atan2(y-fy, x-fx)
	for _, side := range []float64{-1, 1} {
		t := a + math.Pi - side*0.45
		d.seg("fig-line", x, y, x+14*math.Cos(t), y+14*math.Sin(t))
	}
}

func (d *drawer) point(x, y float64) {
	d.add(`<circle class="fig-pin" cx="%s" cy="%s" r="6"/>`, num(x), num(y))
}

// lines draws one of: a point, a line, a ray, a segment, or two lines that
// are parallel, perpendicular or just crossing.
func (d *drawer) lines(a Args) error {
	kind, err := choice(a, "kind", "point", "line", "ray", "segment", "parallel", "perpendicular", "intersecting")
	if err != nil {
		return err
	}
	turn, err := whole(a, "turn", 0, 359)
	if err != nil {
		return err
	}
	d.w, d.h, d.label = 300, 220, "a drawing of points and lines"
	const cx, cy = 150.0, 110.0
	t := float64(turn)
	line := func(x1, y1, x2, y2 float64, ends string) {
		ax, ay := rot(x1, y1, cx, cy, t)
		bx, by := rot(x2, y2, cx, cy, t)
		d.seg("fig-line", ax, ay, bx, by)
		switch ends {
		case "line":
			d.arrow(bx, by, ax, ay)
			d.arrow(ax, ay, bx, by)
		case "ray":
			d.point(ax, ay)
			d.arrow(ax, ay, bx, by)
		case "segment":
			d.point(ax, ay)
			d.point(bx, by)
		}
	}
	switch kind {
	case "point":
		d.point(cx, cy)
	case "line", "ray", "segment":
		line(50, 110, 250, 110, kind)
	case "parallel":
		line(50, 80, 250, 80, "line")
		line(50, 140, 250, 140, "line")
	case "perpendicular":
		line(50, 110, 250, 110, "line")
		line(150, 20, 150, 200, "line")
		// The square corner mark.
		p1x, p1y := rot(150+14, 110, cx, cy, t)
		p2x, p2y := rot(150+14, 110-14, cx, cy, t)
		p3x, p3y := rot(150, 110-14, cx, cy, t)
		d.add(`<path class="fig-line fig-thin" d="M %s %s L %s %s L %s %s"/>`, num(p1x), num(p1y), num(p2x), num(p2y), num(p3x), num(p3y))
	case "intersecting":
		line(50, 150, 250, 70, "line")
		line(70, 40, 230, 190, "line")
	}
	return nil
}

// angle draws angle a, and when b > 0 angle b beside it sharing a ray, each
// with an arc and its label from labels ("35°|?").
func (d *drawer) angle(a Args) error {
	av, err := whole(a, "a", 1, 359)
	if err != nil {
		return err
	}
	bv, err := whole(a, "b", 0, 358)
	if err != nil {
		return err
	}
	turn, err := whole(a, "turn", 0, 359)
	if err != nil {
		return err
	}
	if av+bv > 360 {
		return fmt.Errorf("the angles add to %d, more than a full turn", av+bv)
	}
	labels := strings.Split(a["labels"], "|")
	for len(labels) < 2 {
		labels = append(labels, "")
	}
	for _, l := range labels {
		if len([]rune(l)) > 6 {
			return fmt.Errorf("label %q is longer than 6 characters", l)
		}
	}
	d.w, d.h, d.label = 320, 260, "an angle"
	const cx, cy, r = 160.0, 150.0, 120.0
	base := float64(turn)
	ray := func(deg float64) (float64, float64) {
		// Angles open anticlockwise on screen, as in a textbook.
		rad := -(base + deg) * math.Pi / 180
		return cx + r*math.Cos(rad), cy + r*math.Sin(rad)
	}
	arc := func(from, to float64, rad float64, class, lbl string) {
		x1, y1 := cx+rad*math.Cos(-(base+from)*math.Pi/180), cy+rad*math.Sin(-(base+from)*math.Pi/180)
		x2, y2 := cx+rad*math.Cos(-(base+to)*math.Pi/180), cy+rad*math.Sin(-(base+to)*math.Pi/180)
		large := 0
		if to-from > 180 {
			large = 1
		}
		if to-from == 90 {
			// A right angle is marked with a square, not an arc.
			m := 18.0
			ux, uy := math.Cos(-(base+from)*math.Pi/180), math.Sin(-(base+from)*math.Pi/180)
			vx, vy := math.Cos(-(base+to)*math.Pi/180), math.Sin(-(base+to)*math.Pi/180)
			d.add(`<path class="fig-line fig-thin" d="M %s %s L %s %s L %s %s"/>`, num(cx+m*ux), num(cy+m*uy), num(cx+m*(ux+vx)), num(cy+m*(uy+vy)), num(cx+m*vx), num(cy+m*vy))
		} else {
			d.add(`<path class="%s" d="M %s %s A %s %s 0 %d 0 %s %s"/>`, class, num(x1), num(y1), num(rad), num(rad), large, num(x2), num(y2))
		}
		if lbl != "" {
			mid := -(base + (from+to)/2) * math.Pi / 180
			d.text(cx+(rad+26)*math.Cos(mid), cy+(rad+26)*math.Sin(mid)+7, "fig-small", lbl)
		}
	}
	x0, y0 := ray(0)
	xa, ya := ray(float64(av))
	d.seg("fig-line", cx, cy, x0, y0)
	d.seg("fig-line", cx, cy, xa, ya)
	arc(0, float64(av), 40, "fig-arc", strings.TrimSpace(labels[0]))
	if bv > 0 {
		xb, yb := ray(float64(av + bv))
		d.seg("fig-line", cx, cy, xb, yb)
		arc(float64(av), float64(av+bv), 56, "fig-arc fig-arc2", strings.TrimSpace(labels[1]))
		d.label = "two angles side by side"
	}
	d.point(cx, cy)
	return nil
}

// protractor draws a half protractor with both scales and an angle on it.
// side "right" puts the fixed ray on 0 at the right (read the inner scale);
// "left" puts it on 0 at the left (read the outer).
func (d *drawer) protractor(a Args) error {
	deg, err := whole(a, "degrees", 1, 179)
	if err != nil {
		return err
	}
	side, err := choice(a, "side", "right", "left")
	if err != nil {
		return err
	}
	const cx, cy, r = 200.0, 206.0, 160.0
	d.w, d.h, d.label = 400, 226, "an angle on a protractor"
	d.add(`<path class="fig-face" d="M %s %s A %s %s 0 0 1 %s %s Z"/>`, num(cx-r), num(cy), num(r), num(r), num(cx+r), num(cy))
	for i := 0; i <= 180; i += 5 {
		rad := float64(i) * math.Pi / 180
		in := r - 8
		if i%10 == 0 {
			in = r - 16
		}
		d.seg("fig-tick", cx+in*math.Cos(rad), cy-in*math.Sin(rad), cx+r*math.Cos(rad), cy-r*math.Sin(rad))
		if i%20 == 0 {
			// Inner scale counts from the right; outer from the left. Numbers
			// on the baseline sit just above it, clear of the rays.
			lift := 5.0
			if i == 0 || i == 180 {
				lift = -6
			}
			d.text(cx+(r-34)*math.Cos(rad), cy-(r-34)*math.Sin(rad)+lift, "fig-tiny", strconv.Itoa(i))
			d.text(cx+(r+18)*math.Cos(rad), cy-(r+18)*math.Sin(rad)+lift, "fig-tiny fig-outer", strconv.Itoa(180-i))
		}
	}
	at := float64(deg)
	if side == "left" {
		at = 180 - at
	}
	rad := at * math.Pi / 180
	base := 0.0
	if side == "left" {
		base = math.Pi
	}
	d.seg("fig-hand fig-hand-minute", cx, cy, cx+(r-4)*math.Cos(base), cy-(r-4)*math.Sin(base))
	d.seg("fig-hand fig-hand-minute", cx, cy, cx+(r-4)*math.Cos(rad), cy-(r-4)*math.Sin(rad))
	d.point(cx, cy)
	return nil
}

// symmetry draws a flat shape with a dashed line through its middle at an
// angle (0 is up and down).
func (d *drawer) symmetry(a Args) error {
	name, err := choice(a, "name", FlatShapes...)
	if err != nil {
		return err
	}
	axis, err := whole(a, "axis", 0, 179)
	if err != nil {
		return err
	}
	d.w, d.h, d.label = 240, 240, "a shape with a dashed line through it"
	if name == "circle" {
		d.add(`<circle class="fig-shape" cx="120" cy="120" r="88"/>`)
	} else {
		d.add(`<polygon class="fig-shape" points="%s"/>`, turned(flat[name], 0, 120, 120, 92))
	}
	x1, y1 := rot(120, 6, 120, 120, float64(axis))
	x2, y2 := rot(120, 234, 120, 120, float64(axis))
	d.seg("fig-mirror", x1, y1, x2, y2)
	return nil
}

// TriangleKinds lists the triangles the triangle figure draws.
var TriangleKinds = []string{"equilateral", "isosceles", "scalene", "right", "obtuse", "acute"}

// triangle draws a triangle of a kind, turned, with tick marks on equal
// sides and a square on a right angle.
func (d *drawer) triangle(a Args) error {
	kind, err := choice(a, "kind", TriangleKinds...)
	if err != nil {
		return err
	}
	turn, err := whole(a, "turn", 0, 359)
	if err != nil {
		return err
	}
	pts := map[string][][2]float64{
		"equilateral": {{0, -1}, {0.866, 0.5}, {-0.866, 0.5}},
		"isosceles":   {{0, -1.1}, {0.55, 0.6}, {-0.55, 0.6}},
		"scalene":     {{-0.2, -0.9}, {0.95, 0.6}, {-0.8, 0.45}},
		"right":       {{-0.7, -0.8}, {0.8, 0.6}, {-0.7, 0.6}},
		"obtuse":      {{-0.2, -0.3}, {1.0, 0.45}, {-0.95, 0.45}},
		"acute":       {{0.15, -0.95}, {0.85, 0.55}, {-0.75, 0.55}},
	}[kind]
	const cx, cy, s = 130.0, 125.0, 95.0
	var p [3][2]float64
	for i, q := range pts {
		p[i][0], p[i][1] = rot(cx+q[0]*s, cy+q[1]*s, cx, cy, float64(turn))
	}
	d.w, d.h, d.label = 260, 250, "a triangle"
	d.add(`<polygon class="fig-shape" points="%s,%s %s,%s %s,%s"/>`, num(p[0][0]), num(p[0][1]), num(p[1][0]), num(p[1][1]), num(p[2][0]), num(p[2][1]))
	tick := func(i, j, n int) {
		mx, my := (p[i][0]+p[j][0])/2, (p[i][1]+p[j][1])/2
		dx, dy := p[j][0]-p[i][0], p[j][1]-p[i][1]
		l := math.Hypot(dx, dy)
		nx, ny := -dy/l*8, dx/l*8
		for k := range n {
			off := (float64(k) - float64(n-1)/2) * 6
			ox, oy := mx+dx/l*off, my+dy/l*off
			d.seg("fig-line fig-thin", ox-nx, oy-ny, ox+nx, oy+ny)
		}
	}
	switch kind {
	case "equilateral":
		tick(0, 1, 1)
		tick(1, 2, 1)
		tick(2, 0, 1)
	case "isosceles":
		tick(0, 1, 1)
		tick(2, 0, 1)
	case "right":
		// The right angle is at the third point.
		c := p[2]
		ux, uy := p[0][0]-c[0], p[0][1]-c[1]
		vx, vy := p[1][0]-c[0], p[1][1]-c[1]
		lu, lv := math.Hypot(ux, uy), math.Hypot(vx, vy)
		m := 16.0
		ux, uy, vx, vy = ux/lu*m, uy/lu*m, vx/lv*m, vy/lv*m
		d.add(`<path class="fig-line fig-thin" d="M %s %s L %s %s L %s %s"/>`, num(c[0]+ux), num(c[1]+uy), num(c[0]+ux+vx), num(c[1]+uy+vy), num(c[0]+vx), num(c[1]+vy))
	}
	return nil
}
