// SPDX-License-Identifier: Apache-2.0

// Package figure draws the figures problem templates name (ADR 9) as SVG:
// fraction bars, number lines, clocks, rectangles, boxes, coordinate grids,
// arrays, and the early-years pictures (dots, ten frames, shapes, solids,
// comparisons, a balance, positions), from the arguments a template
// computed. It also cleans the
// static SVGs a content pack ships.
//
// The SVG carries no style attributes and no colours: every shape has a
// class (fig-line, fig-fill, fig-mark, fig-text, ...) that the page's CSS
// colours, so figures follow light and dark mode and the strict
// Content-Security-Policy. The output is safe to place in HTML as is: the
// only text in it is numbers and a unit word, escaped.
package figure

import (
	"fmt"
	"html"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

// Args are a figure's arguments as the template wrote them: numbers in
// lowest terms ("3", "1/6") or text ("cm").
type Args map[string]string

// Catalogue lists every figure Draw knows, with its arguments.
var Catalogue = map[string][]string{
	"array":        {"rows", "cols"},
	"clock":        {"hour", "minute"},
	"coordinate":   {"size", "x", "y"},
	"fraction-bar": {"parts", "shaded"},
	"number-line":  {"from", "to", "step", "mark"},
	"rectangle":    {"width", "height", "unit"},
	"prism":        {"length", "width", "height", "unit"},
	"dots":         {"count", "arrange"},
	"dot-groups":   {"a", "b", "style"},
	"ten-frame":    {"frames", "filled"},
	"shape":        {"name", "turn"},
	"shape-mix":    {"circles", "triangles", "squares"},
	"solid":        {"name"},
	"compare":      {"a", "b", "c", "kind"},
	"balance":      {"tip"},
	"position":     {"place"},
	"base-ten":     {"hundreds", "tens", "ones"},
	"ruler":        {"start", "length", "max", "unit"},
	"table":        {"rows", "style"},
	"cut-shape":    {"name", "parts", "equal", "shaded"},
}

// Draw returns a figure as an SVG element, or an error when its arguments
// cannot be drawn (too many parts, a mark off the line). The content gate
// draws every generated figure, so a pack that passes never fails here.
func Draw(name string, a Args) (string, error) {
	var d drawer
	var err error
	switch name {
	case "array":
		err = d.array(a)
	case "clock":
		err = d.clock(a)
	case "coordinate":
		err = d.coordinate(a)
	case "fraction-bar":
		err = d.fractionBar(a)
	case "number-line":
		err = d.numberLine(a)
	case "rectangle":
		err = d.rectangle(a)
	case "prism":
		err = d.prism(a)
	case "dots":
		err = d.dots(a)
	case "dot-groups":
		err = d.dotGroups(a)
	case "ten-frame":
		err = d.tenFrame(a)
	case "shape":
		err = d.shape(a)
	case "shape-mix":
		err = d.shapeMix(a)
	case "solid":
		err = d.solid(a)
	case "compare":
		err = d.compare(a)
	case "balance":
		err = d.balance(a)
	case "position":
		err = d.position(a)
	case "base-ten":
		err = d.baseTen(a)
	case "ruler":
		err = d.ruler(a)
	case "table":
		err = d.table(a)
	case "cut-shape":
		err = d.cutShape(a)
	default:
		return "", fmt.Errorf("figure: there is no figure %q", name)
	}
	if err != nil {
		return "", fmt.Errorf("figure %s: %w", name, err)
	}
	return d.String(), nil
}

// drawer builds one SVG.
type drawer struct {
	b      strings.Builder
	w, h   float64
	label  string
	shapes strings.Builder
}

func (d *drawer) String() string {
	fmt.Fprintf(&d.b, `<svg class="fig" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %s %s" role="img" aria-label="%s">`,
		num(d.w), num(d.h), html.EscapeString(d.label))
	d.b.WriteString(d.shapes.String())
	d.b.WriteString(`</svg>`)
	return d.b.String()
}

func (d *drawer) add(format string, args ...any) { fmt.Fprintf(&d.shapes, format, args...) }

func (d *drawer) text(x, y float64, class, s string) {
	d.add(`<text class="fig-text %s" x="%s" y="%s" text-anchor="middle">%s</text>`, class, num(x), num(y), html.EscapeString(s))
}

// num writes a coordinate with at most two decimals.
func num(f float64) string {
	return strconv.FormatFloat(math.Round(f*100)/100, 'f', -1, 64)
}

// --- reading arguments ------------------------------------------------------

func rat(a Args, key string) (*big.Rat, error) {
	s, ok := a[key]
	if !ok {
		return nil, fmt.Errorf("no %s", key)
	}
	r, ok := new(big.Rat).SetString(strings.TrimSpace(s))
	if !ok {
		return nil, fmt.Errorf("%s is %q, not a number", key, s)
	}
	return r, nil
}

func whole(a Args, key string, lo, hi int64) (int, error) {
	r, err := rat(a, key)
	if err != nil {
		return 0, err
	}
	if !r.IsInt() || r.Num().Int64() < lo || r.Num().Int64() > hi {
		return 0, fmt.Errorf("%s is %s; it must be a whole number from %d to %d", key, r.RatString(), lo, hi)
	}
	return int(r.Num().Int64()), nil
}

func positive(a Args, key string, hi float64) (float64, string, error) {
	r, err := rat(a, key)
	if err != nil {
		return 0, "", err
	}
	f, _ := r.Float64()
	if f <= 0 || f > hi {
		return 0, "", fmt.Errorf("%s is %s; it must be more than 0 and at most %s", key, r.RatString(), num(hi))
	}
	return f, label(r), nil
}

// label writes a number as a figure shows it: whole, decimal when it ends
// within two places, else a fraction.
func label(r *big.Rat) string {
	if r.IsInt() {
		return r.Num().String()
	}
	for _, places := range []int{1, 2} {
		scaled := new(big.Rat).Mul(r, new(big.Rat).SetInt64(int64(math.Pow10(places))))
		if scaled.IsInt() {
			return strings.TrimRight(r.FloatString(places), "0")
		}
	}
	return r.RatString()
}

func unit(a Args) string {
	u := strings.TrimSpace(a["unit"])
	if len(u) > 12 {
		u = u[:12]
	}
	return u
}

// --- the figures ------------------------------------------------------------

func (d *drawer) array(a Args) error {
	rows, err := whole(a, "rows", 1, 12)
	if err != nil {
		return err
	}
	cols, err := whole(a, "cols", 1, 12)
	if err != nil {
		return err
	}
	const gap = 28.0
	d.w, d.h = float64(cols)*gap+12, float64(rows)*gap+12
	d.label = fmt.Sprintf("%d %s of %d %s", rows, plural(rows, "row", "rows"), cols, plural(cols, "dot", "dots"))
	for r := range rows {
		for c := range cols {
			d.add(`<circle class="fig-dot" cx="%s" cy="%s" r="9"/>`, num(20+float64(c)*gap), num(20+float64(r)*gap))
		}
	}
	return nil
}

func (d *drawer) fractionBar(a Args) error {
	parts, err := whole(a, "parts", 1, 24)
	if err != nil {
		return err
	}
	shaded, err := whole(a, "shaded", 0, int64(parts))
	if err != nil {
		return err
	}
	const width, height, gap = 320.0, 56.0, 4.0
	d.w, d.h = width+8, height+8
	d.label = fmt.Sprintf("a bar cut into %d equal parts, %d shaded", parts, shaded)
	cell := (width - gap*float64(parts-1)) / float64(parts)
	for i := range parts {
		class := "fig-empty"
		if i < shaded {
			class = "fig-fill"
		}
		d.add(`<rect class="%s" x="%s" y="4" width="%s" height="%s" rx="8"/>`, class, num(4+float64(i)*(cell+gap)), num(cell), num(height))
	}
	return nil
}

func (d *drawer) numberLine(a Args) error {
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
	mark, err := rat(a, "mark")
	if err != nil {
		return err
	}
	if to.Cmp(from) <= 0 || step.Sign() <= 0 {
		return fmt.Errorf("from %s, to %s, step %s: the line must run forwards", from.RatString(), to.RatString(), step.RatString())
	}
	span := new(big.Rat).Sub(to, from)
	n := new(big.Rat).Quo(span, step)
	if !n.IsInt() || n.Num().Int64() > 40 {
		return fmt.Errorf("step %s does not fit from %s to %s in at most 40 jumps", step.RatString(), from.RatString(), to.RatString())
	}
	if mark.Cmp(from) < 0 || mark.Cmp(to) > 0 {
		return fmt.Errorf("mark %s is off the line", mark.RatString())
	}
	ticks := int(n.Num().Int64())
	const left, right, y = 24.0, 376.0, 34.0
	d.w, d.h = 400, 72
	d.label = "a number line from " + label(from) + " to " + label(to) + ", with a dot"
	pos := func(v *big.Rat) float64 {
		f, _ := new(big.Rat).Quo(new(big.Rat).Sub(v, from), span).Float64()
		return left + f*(right-left)
	}
	d.add(`<line class="fig-line" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(left-12), num(y), num(right+12), num(y))
	// Label the ends, the whole numbers between, and every tick when there
	// are few enough to read.
	every := ticks <= 10
	for i := 0; i <= ticks; i++ {
		v := new(big.Rat).Add(from, new(big.Rat).Mul(step, new(big.Rat).SetInt64(int64(i))))
		x := pos(v)
		major := v.IsInt() || i == 0 || i == ticks
		h := 7.0
		if major {
			h = 11
		}
		d.add(`<line class="fig-line" x1="%s" y1="%s" x2="%s" y2="%s"/>`, num(x), num(y-h), num(x), num(y+h))
		if major && (v.IsInt() || every) || every && step.IsInt() {
			d.text(x, y+30, "fig-small", label(v))
		}
	}
	d.add(`<circle class="fig-mark" cx="%s" cy="%s" r="8"/>`, num(pos(mark)), num(y))
	return nil
}

func (d *drawer) clock(a Args) error {
	hour, err := whole(a, "hour", 1, 12)
	if err != nil {
		return err
	}
	minute, err := whole(a, "minute", 0, 59)
	if err != nil {
		return err
	}
	const c, r = 110.0, 100.0
	d.w, d.h = 220, 220
	d.label = "a clock face"
	d.add(`<circle class="fig-face" cx="110" cy="110" r="%s"/>`, num(r))
	for i := 1; i <= 12; i++ {
		ang := float64(i) * math.Pi / 6
		x, y := c+math.Sin(ang)*(r-22), c-math.Cos(ang)*(r-22)
		d.text(x, y+7, "fig-small", strconv.Itoa(i))
	}
	for i := range 60 {
		ang := float64(i) * math.Pi / 30
		in := r - 6
		if i%5 == 0 {
			in = r - 10
		}
		d.add(`<line class="fig-tick" x1="%s" y1="%s" x2="%s" y2="%s"/>`,
			num(c+math.Sin(ang)*in), num(c-math.Cos(ang)*in), num(c+math.Sin(ang)*(r-2)), num(c-math.Cos(ang)*(r-2)))
	}
	hourAng := (float64(hour%12) + float64(minute)/60) * math.Pi / 6
	minAng := float64(minute) * math.Pi / 30
	d.add(`<line class="fig-hand fig-hand-hour" x1="110" y1="110" x2="%s" y2="%s"/>`, num(c+math.Sin(hourAng)*46), num(c-math.Cos(hourAng)*46))
	d.add(`<line class="fig-hand fig-hand-minute" x1="110" y1="110" x2="%s" y2="%s"/>`, num(c+math.Sin(minAng)*64), num(c-math.Cos(minAng)*64))
	d.add(`<circle class="fig-pin" cx="110" cy="110" r="6"/>`)
	return nil
}

func (d *drawer) coordinate(a Args) error {
	size, err := whole(a, "size", 2, 20)
	if err != nil {
		return err
	}
	x, err := whole(a, "x", 0, int64(size))
	if err != nil {
		return err
	}
	y, err := whole(a, "y", 0, int64(size))
	if err != nil {
		return err
	}
	cell := 260.0 / float64(size)
	const ox, oy = 36.0, 290.0
	d.w, d.h = 320, 320
	d.label = fmt.Sprintf("a grid from 0 to %d with one point", size)
	for i := 0; i <= size; i++ {
		p := float64(i) * cell
		class := "fig-grid"
		if i == 0 {
			class = "fig-line"
		}
		d.add(`<line class="%s" x1="%s" y1="%s" x2="%s" y2="%s"/>`, class, num(ox+p), num(oy), num(ox+p), num(oy-260))
		d.add(`<line class="%s" x1="%s" y1="%s" x2="%s" y2="%s"/>`, class, num(ox), num(oy-p), num(ox+260), num(oy-p))
		if size <= 10 || i%2 == 0 {
			d.text(ox+p, oy+22, "fig-small", strconv.Itoa(i))
			if i > 0 {
				d.text(ox-16, oy-p+6, "fig-small", strconv.Itoa(i))
			}
		}
	}
	d.add(`<circle class="fig-mark" cx="%s" cy="%s" r="8"/>`, num(ox+float64(x)*cell), num(oy-float64(y)*cell))
	return nil
}

func (d *drawer) rectangle(a Args) error {
	w, wl, err := positive(a, "width", 1000)
	if err != nil {
		return err
	}
	h, hl, err := positive(a, "height", 1000)
	if err != nil {
		return err
	}
	u := unit(a)
	// Keep the shape's proportions, within limits that stay readable.
	ratio := math.Min(math.Max(h/w, 0.25), 1.6)
	rw := 240.0
	rh := rw * ratio
	d.w, d.h = rw+120, rh+70
	d.label = fmt.Sprintf("a rectangle %s by %s %s", wl, hl, u)
	d.add(`<rect class="fig-shape" x="40" y="20" width="%s" height="%s" rx="4"/>`, num(rw), num(rh))
	d.text(40+rw/2, rh+52, "", strings.TrimSpace(wl+" "+u))
	d.text(rw+80, 20+rh/2+7, "", strings.TrimSpace(hl+" "+u))
	return nil
}

func (d *drawer) prism(a Args) error {
	l, ll, err := positive(a, "length", 1000)
	if err != nil {
		return err
	}
	w, wl, err := positive(a, "width", 1000)
	if err != nil {
		return err
	}
	h, hl, err := positive(a, "height", 1000)
	if err != nil {
		return err
	}
	u := unit(a)
	// A box in oblique projection, scaled so the largest edge fits.
	scale := 170 / math.Max(l, math.Max(h, w*0.6))
	fl, fh, dp := l*scale, h*scale, w*scale*0.45
	x0, y0 := 30.0, 30+dp
	d.w, d.h = fl+dp+130, fh+dp+80
	d.label = fmt.Sprintf("a box %s long, %s wide and %s tall, in %s", ll, wl, hl, u)
	pts := func(p ...float64) string {
		var s []string
		for i := 0; i+1 < len(p); i += 2 {
			s = append(s, num(p[i])+","+num(p[i+1]))
		}
		return strings.Join(s, " ")
	}
	d.add(`<polygon class="fig-shape" points="%s"/>`, pts(x0, y0, x0+fl, y0, x0+fl, y0+fh, x0, y0+fh))
	d.add(`<polygon class="fig-shape fig-top" points="%s"/>`, pts(x0, y0, x0+dp, y0-dp, x0+fl+dp, y0-dp, x0+fl, y0))
	d.add(`<polygon class="fig-shape fig-side" points="%s"/>`, pts(x0+fl, y0, x0+fl+dp, y0-dp, x0+fl+dp, y0+fh-dp, x0+fl, y0+fh))
	d.text(x0+fl/2, y0+fh+30, "", strings.TrimSpace(ll+" "+u))
	// Height beside the back edge; width (depth) below the lower diagonal,
	// where no face is in the way.
	d.text(x0+fl+dp+44, y0+fh/2-dp/2+7, "", strings.TrimSpace(hl+" "+u))
	d.text(x0+fl+dp/2+40, y0+fh-dp/2+24, "", strings.TrimSpace(wl+" "+u))
	return nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Names lists the catalogue's figures, sorted.
func Names() []string {
	var out []string
	for k := range Catalogue {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
