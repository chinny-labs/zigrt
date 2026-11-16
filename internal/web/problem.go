package web

import (
	"html/template"
	"strconv"
	"strings"

	"github.com/chinny-labs/zigrt/internal/drill"
)

// HTML draws the problem in its layout: across, stacked, or long division.
// In "drill" mode the blank is a slot the drill script types into; in
// "sheet" mode it is a ruled box to write in. Everything written here is a
// number, an operator sign or a fixed class name, so no escaping is needed.
func (p problemView) HTML() template.HTML {
	var b strings.Builder
	label := template.HTMLEscapeString(p.spoken())
	switch {
	case p.Format == drill.Stacked && p.Op == drill.Div:
		b.WriteString(`<div class="pr pr-ld" role="group" aria-label="` + label + `"><span></span><span class="ld-ans">`)
		p.slots(&b)
		b.WriteString(`</span><span class="ld-divisor">` + num(p.Y) + `</span><span class="ld-dividend">` + num(p.X) + `</span></div>`)
	case p.Format == drill.Stacked:
		b.WriteString(`<div class="pr pr-vt" role="group" aria-label="` + label + `"><span></span>` + num(p.X) +
			`<span class="op">` + p.Op.Symbol() + `</span>` + num(p.Y) + `<span class="bar"></span><span></span><span class="vt-ans">`)
		p.slots(&b)
		b.WriteString(`</span></div>`)
	default:
		b.WriteString(`<div class="pr pr-hz" role="group" aria-label="` + label + `">`)
		p.part(&b, drill.BlankX, p.X)
		b.WriteString(`<span class="op">` + p.Op.Symbol() + `</span>`)
		p.part(&b, drill.BlankY, p.Y)
		b.WriteString(`<span class="op">=</span>`)
		p.part(&b, drill.BlankZ, p.Z)
		b.WriteString(`</div>`)
	}
	return template.HTML(b.String()) //nolint:gosec // built from integers and fixed strings only
}

func num(n int) string { return `<span class="n">` + strconv.Itoa(n) + `</span>` }

func (p problemView) part(b *strings.Builder, key drill.Blank, n int) {
	if p.Blank == key {
		p.slots(b)
		return
	}
	b.WriteString(num(n))
}

// slots writes the answer box, plus a remainder box when the problem has one.
func (p problemView) slots(b *strings.Builder) {
	box := func(name string, width int) {
		w := "w" + strconv.Itoa(min(max(width, 2), 8))
		if p.Mode == "drill" {
			b.WriteString(`<span class="slot ` + w + `" data-slot="` + name + `"></span>`)
			return
		}
		b.WriteString(`<span class="blank ` + w + `"></span>`)
	}
	box("main", p.AnswerWidth())
	if p.AsksRemainder() {
		b.WriteString(`<span class="rem-label">R</span>`)
		box("rem", p.RemWidth())
	}
}

// spoken is the problem as a screen reader should say it.
func (p problemView) spoken() string {
	word := map[drill.Op]string{drill.Add: "plus", drill.Sub: "minus", drill.Mul: "times", drill.Div: "divided by"}[p.Op]
	say := func(key drill.Blank, n int) string {
		if p.Blank == key {
			return "blank"
		}
		return strconv.Itoa(n)
	}
	return say(drill.BlankX, p.X) + " " + word + " " + say(drill.BlankY, p.Y) + " equals " + say(drill.BlankZ, p.Z)
}
