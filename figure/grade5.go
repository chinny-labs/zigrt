// SPDX-License-Identifier: Apache-2.0

package figure

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The figures grade 5 adds: two boxes joined side by side (volume of a
// composite solid), and several points on a grid (graphing a pattern).

// joinedPrisms draws two boxes of the same depth side by side on the
// ground: the first a long and h1 tall, the second b long and h2 tall, both
// w deep, with every edge a student needs labelled.
func (d *drawer) joinedPrisms(a Args) error {
	al, all, err := positive(a, "a", 100)
	if err != nil {
		return err
	}
	bl, bll, err := positive(a, "b", 100)
	if err != nil {
		return err
	}
	h1, h1l, err := positive(a, "h1", 100)
	if err != nil {
		return err
	}
	h2, h2l, err := positive(a, "h2", 100)
	if err != nil {
		return err
	}
	w, wl, err := positive(a, "w", 100)
	if err != nil {
		return err
	}
	u := unit(a)
	lbl := func(s string) string { return strings.TrimSpace(s + " " + u) }
	scale := 200 / math.Max(al+bl, math.Max(h1, h2)*1.2)
	A, B, H1, H2 := al*scale, bl*scale, h1*scale, h2*scale
	dp := math.Min(w*scale*0.45, 60)
	x0, ground := 76.0, 40+math.Max(H1, H2)+dp
	d.w, d.h = x0+A+B+dp+90, ground+50
	d.label = "two boxes joined side by side, with their edges labelled"
	pts := func(p ...float64) string {
		var s []string
		for i := 0; i+1 < len(p); i += 2 {
			s = append(s, num(p[i])+","+num(p[i+1]))
		}
		return strings.Join(s, " ")
	}
	box := func(x, l, h float64, sideShown bool) {
		top := ground - h
		d.add(`<polygon class="fig-shape" points="%s"/>`, pts(x, top, x+l, top, x+l, ground, x, ground))
		d.add(`<polygon class="fig-shape fig-top" points="%s"/>`, pts(x, top, x+dp, top-dp, x+l+dp, top-dp, x+l, top))
		if sideShown {
			d.add(`<polygon class="fig-shape fig-side" points="%s"/>`, pts(x+l, top, x+l+dp, top-dp, x+l+dp, ground-dp, x+l, ground))
		}
	}
	// Draw the taller box last only if it is the second one, so faces overlap
	// the right way: the second box's front covers the first's side.
	box(x0, A, H1, H1 > H2)
	box(x0+A, B, H2, true)
	if H1 > H2 {
		// The first box's side shows above the second box.
		top := ground - H1
		d.add(`<polygon class="fig-shape fig-side" points="%s"/>`, pts(x0+A, top, x0+A+dp, top-dp, x0+A+dp, ground-H2-dp, x0+A, ground-H2))
	}
	d.text(x0+A/2, ground+28, "", lbl(all))
	d.text(x0+A+B/2, ground+28, "", lbl(bll))
	d.add(`<text class="fig-text" x="%s" y="%s" text-anchor="end">%s</text>`, num(x0-8), num(ground-H1/2+7), lbl(h1l))
	d.add(`<text class="fig-text" x="%s" y="%s">%s</text>`, num(x0+A+B+dp+8), num(ground-dp-H2/2+7), lbl(h2l))
	d.add(`<text class="fig-text" x="%s" y="%s">%s</text>`, num(x0+A+B+dp/2+10), num(ground-dp/2+18), lbl(wl))
	return nil
}

// plot draws points on a first-quadrant grid of the given size, joined in
// order when join is 1. points is "1,3|2,5|3,7".
func (d *drawer) plot(a Args) error {
	size, err := whole(a, "size", 2, 20)
	if err != nil {
		return err
	}
	join, err := whole(a, "join", 0, 1)
	if err != nil {
		return err
	}
	var ps [][2]int
	for _, p := range strings.Split(a["points"], "|") {
		xs, ys, ok := strings.Cut(strings.TrimSpace(p), ",")
		x, ex := strconv.Atoi(strings.TrimSpace(xs))
		y, ey := strconv.Atoi(strings.TrimSpace(ys))
		if !ok || ex != nil || ey != nil || x < 0 || y < 0 || x > size || y > size {
			return fmt.Errorf("point %q must be x,y on the grid from 0 to %d", p, size)
		}
		ps = append(ps, [2]int{x, y})
	}
	if len(ps) < 1 || len(ps) > 12 {
		return fmt.Errorf("%d points; plot 1 to 12", len(ps))
	}
	cell := 260.0 / float64(size)
	const ox, oy = 36.0, 290.0
	d.w, d.h = 320, 320
	d.label = fmt.Sprintf("a grid from 0 to %d with %d points", size, len(ps))
	for i := 0; i <= size; i++ {
		p := float64(i) * cell
		class := "fig-grid"
		if i == 0 {
			class = "fig-line"
		}
		d.seg(class, ox+p, oy, ox+p, oy-260)
		d.seg(class, ox, oy-p, ox+260, oy-p)
		if size <= 10 || i%2 == 0 {
			d.text(ox+p, oy+22, "fig-small", strconv.Itoa(i))
			if i > 0 {
				d.text(ox-16, oy-p+6, "fig-small", strconv.Itoa(i))
			}
		}
	}
	at := func(p [2]int) (float64, float64) { return ox + float64(p[0])*cell, oy - float64(p[1])*cell }
	if join == 1 && len(ps) > 1 {
		var s []string
		for _, p := range ps {
			x, y := at(p)
			s = append(s, num(x)+","+num(y))
		}
		d.add(`<polyline class="fig-line fig-plotline" points="%s"/>`, strings.Join(s, " "))
	}
	for _, p := range ps {
		x, y := at(p)
		d.add(`<circle class="fig-mark" cx="%s" cy="%s" r="7"/>`, num(x), num(y))
	}
	return nil
}
