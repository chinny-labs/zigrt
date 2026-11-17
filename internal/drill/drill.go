// Package drill builds and marks free-practice problem sets: the four
// operations at K-5 levels, in across, stacked and missing-number styles
// (plan §5). It is pure: the same settings and seed always give the same
// problems, so a drill can be re-marked and a worksheet reprinted.
package drill

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
)

// Op is one of the four operations.
type Op string

const (
	Add Op = "add"
	Sub Op = "sub"
	Mul Op = "mul"
	Div Op = "div"
)

// Ops is every operation, in display order.
var Ops = []Op{Add, Sub, Mul, Div}

// Symbol is the sign printed for the operation.
func (o Op) Symbol() string {
	switch o {
	case Add:
		return "+"
	case Sub:
		return "−"
	case Mul:
		return "×"
	case Div:
		return "÷"
	}
	return "?"
}

// Name is the operation's name for labels.
func (o Op) Name() string {
	switch o {
	case Add:
		return "Addition"
	case Sub:
		return "Subtraction"
	case Mul:
		return "Multiplication"
	case Div:
		return "Division"
	}
	return string(o)
}

// Format is how a problem is laid out.
type Format string

const (
	Across  Format = "across"  // 7 + 5 = ?
	Stacked Format = "stacked" // column layout; long-division layout for ÷
	Missing Format = "missing" // 7 + ? = 12
)

// Formats is every layout, in display order.
var Formats = []Format{Across, Stacked, Missing}

// Label is the layout's name for buttons.
func (f Format) Label() string {
	switch f {
	case Across:
		return "Across"
	case Stacked:
		return "Stacked"
	case Missing:
		return "Missing"
	}
	return string(f)
}

// Range is an inclusive pair of bounds.
type Range [2]int

// Level is a difficulty preset. Add, Sub and Mul draw the two numbers from A
// and B. Div draws the divisor from D and either the quotient from Q or the
// dividend from N.
type Level struct {
	ID    string
	Label string
	A, B  Range
	Max   int // for Add: the largest allowed sum; 0 means no limit
	D, Q  Range
	N     Range // zero when the level is defined by quotient
}

// CustomLevel is the level ID that means "use the custom ranges".
const CustomLevel = "custom"

// Levels are the presets for each operation, easiest first.
var Levels = map[Op][]Level{
	Add: {
		{ID: "sum10", Label: "Sums to 10", A: Range{0, 10}, B: Range{0, 10}, Max: 10},
		{ID: "sum20", Label: "Sums to 20", A: Range{0, 20}, B: Range{0, 20}, Max: 20},
		{ID: "d2d1", Label: "2-digit + 1-digit", A: Range{10, 99}, B: Range{1, 9}},
		{ID: "d2d2", Label: "2-digit + 2-digit", A: Range{10, 99}, B: Range{10, 99}},
		{ID: "d3d3", Label: "3-digit + 3-digit", A: Range{100, 999}, B: Range{100, 999}},
		{ID: "d4d4", Label: "4-digit + 4-digit", A: Range{1000, 9999}, B: Range{1000, 9999}},
	},
	Sub: {
		{ID: "within10", Label: "Within 10", A: Range{0, 10}, B: Range{0, 10}},
		{ID: "within20", Label: "Within 20", A: Range{0, 20}, B: Range{0, 20}},
		{ID: "d2d1", Label: "2-digit − 1-digit", A: Range{10, 99}, B: Range{1, 9}},
		{ID: "d2d2", Label: "2-digit − 2-digit", A: Range{10, 99}, B: Range{10, 99}},
		{ID: "d3d3", Label: "3-digit − 3-digit", A: Range{100, 999}, B: Range{100, 999}},
		{ID: "d4d4", Label: "4-digit − 4-digit", A: Range{1000, 9999}, B: Range{1000, 9999}},
	},
	Mul: {
		{ID: "facts5", Label: "Times tables to 5", A: Range{1, 5}, B: Range{1, 5}},
		{ID: "facts10", Label: "Times tables to 10", A: Range{1, 10}, B: Range{1, 10}},
		{ID: "facts12", Label: "Times tables to 12", A: Range{1, 12}, B: Range{1, 12}},
		{ID: "d2d1", Label: "2-digit × 1-digit", A: Range{10, 99}, B: Range{2, 9}},
		{ID: "d3d1", Label: "3-digit × 1-digit", A: Range{100, 999}, B: Range{2, 9}},
		{ID: "d2d2", Label: "2-digit × 2-digit", A: Range{10, 99}, B: Range{10, 99}},
	},
	Div: {
		{ID: "facts5", Label: "Division facts to 5", D: Range{1, 5}, Q: Range{1, 5}},
		{ID: "facts10", Label: "Division facts to 10", D: Range{1, 10}, Q: Range{1, 10}},
		{ID: "facts12", Label: "Division facts to 12", D: Range{1, 12}, Q: Range{1, 12}},
		{ID: "d2d1", Label: "2-digit ÷ 1-digit", D: Range{2, 9}, N: Range{10, 99}},
		{ID: "d3d1", Label: "3-digit ÷ 1-digit", D: Range{2, 9}, N: Range{100, 999}},
		{ID: "d3d2", Label: "3-digit ÷ 2-digit", D: Range{10, 99}, N: Range{100, 999}},
	},
}

// CustomLabels name the two custom ranges for each operation.
var CustomLabels = map[Op][2]string{
	Add: {"First number", "Second number"},
	Sub: {"Start with", "Take away"},
	Mul: {"First number", "Second number"},
	Div: {"Divide by", "Answer"},
}

// Limits on what a set may ask for.
const (
	MaxNumber = 99999
	MaxCount  = 100
)

// Counts are the set sizes the builder offers.
var Counts = []int{10, 20, 30, 50}

// OpSettings is one operation's part of the settings.
type OpSettings struct {
	On         bool   `json:"on"`
	Level      string `json:"level"`
	CustomA    Range  `json:"custom_a,omitempty"` // omitempty marks it optional in schemas; an array is always encoded
	CustomB    Range  `json:"custom_b,omitempty"`
	Remainders bool   `json:"remainders,omitempty"` // Div only
}

// Settings is everything the builder chooses.
type Settings struct {
	Ops     map[Op]OpSettings `json:"ops"`
	Formats []Format          `json:"formats"`
	Count   int               `json:"count"`
}

var defaultCustom = map[Op][2]Range{
	Add: {{0, 20}, {0, 20}},
	Sub: {{0, 20}, {0, 20}},
	Mul: {{1, 12}, {7, 7}},
	Div: {{2, 9}, {1, 10}},
}

// Default is what the builder shows the first time: 20 addition problems,
// sums to 20, written across.
func Default() Settings {
	s := Settings{Ops: map[Op]OpSettings{}, Formats: []Format{Across}, Count: 20}
	for _, op := range Ops {
		s.Ops[op] = OpSettings{On: op == Add, Level: Levels[op][1].ID, CustomA: defaultCustom[op][0], CustomB: defaultCustom[op][1]}
	}
	return s
}

// ErrNothingChosen means the settings turn on no operation or no layout.
var ErrNothingChosen = errors.New("choose at least one kind of problem and one layout")

// Normalize returns settings that are safe to generate from: unknown levels
// and layouts are replaced by defaults, ranges are ordered and clamped, and
// the count is one the builder offers. It fails only when nothing is chosen.
func Normalize(in Settings) (Settings, error) {
	out := Default()
	anyOn := false
	for _, op := range Ops {
		src, ok := in.Ops[op]
		if !ok {
			dst := out.Ops[op]
			dst.On = false
			out.Ops[op] = dst
			continue
		}
		dst := out.Ops[op]
		dst.On = src.On
		anyOn = anyOn || src.On
		if src.Level == CustomLevel || findLevel(op, src.Level) != nil {
			dst.Level = src.Level
		}
		floor := 0
		if op == Div {
			floor = 1 // never divide by zero
		}
		dst.CustomA = cleanRange(src.CustomA, floor)
		dst.CustomB = cleanRange(src.CustomB, 0)
		dst.Remainders = op == Div && src.Remainders
		out.Ops[op] = dst
	}
	out.Formats = nil
	for _, f := range Formats { // keep display order, drop unknowns and repeats
		for _, g := range in.Formats {
			if f == g {
				out.Formats = append(out.Formats, f)
				break
			}
		}
	}
	if !anyOn || len(out.Formats) == 0 {
		return out, ErrNothingChosen
	}
	out.Count = Counts[1]
	if in.Count >= 1 && in.Count <= MaxCount {
		out.Count = in.Count
	}
	return out, nil
}

func cleanRange(r Range, floor int) Range {
	lo, hi := clamp(r[0], floor), clamp(r[1], floor)
	if lo > hi {
		lo, hi = hi, lo
	}
	return Range{lo, hi}
}

func clamp(n, floor int) int { return min(MaxNumber, max(floor, n)) }

func findLevel(op Op, id string) *Level {
	for i := range Levels[op] {
		if Levels[op][i].ID == id {
			return &Levels[op][i]
		}
	}
	return nil
}

// LevelLabel is the level's name, or "Custom numbers".
func LevelLabel(op Op, id string) string {
	if l := findLevel(op, id); l != nil {
		return l.Label
	}
	return "Custom numbers"
}

// spec resolves an operation's settings into the ranges to draw from.
func spec(op Op, s OpSettings) Level {
	if s.Level == CustomLevel {
		if op == Div {
			return Level{D: Range{max(1, s.CustomA[0]), max(1, s.CustomA[1])}, Q: s.CustomB}
		}
		return Level{A: s.CustomA, B: s.CustomB}
	}
	if l := findLevel(op, s.Level); l != nil {
		return *l
	}
	return Levels[op][0]
}

// Blank says which part of "x op y = z" the student fills in.
type Blank string

const (
	BlankX Blank = "x"
	BlankY Blank = "y"
	BlankZ Blank = "z"
)

// Problem is "X op Y = Z", with Rem the remainder when HasRem is set
// (division with remainders on). Blank is the part left out.
type Problem struct {
	Op     Op     `json:"op"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	Z      int    `json:"z"`
	Rem    int    `json:"rem,omitempty"`
	HasRem bool   `json:"has_rem,omitempty"`
	Format Format `json:"format"`
	Blank  Blank  `json:"blank"`
}

// Generate builds the set for normalized settings and a seed. Repeats are
// avoided where the level has enough distinct problems.
func Generate(s Settings, seed uint64) []Problem {
	rng := rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15)) //nolint:gosec // practice problems, not secrets
	var on []Op
	for _, op := range Ops {
		if s.Ops[op].On {
			on = append(on, op)
		}
	}
	if len(on) == 0 || len(s.Formats) == 0 || s.Count < 1 {
		return nil
	}
	ops := spread(rng, on, s.Count)
	formats := spread(rng, s.Formats, s.Count)
	type key struct {
		op   Op
		x, y int
	}
	seen := map[key]bool{}
	out := make([]Problem, 0, s.Count)
	for i := range s.Count {
		op := ops[i]
		os := s.Ops[op]
		lv := spec(op, os)
		var p Problem
		for range 40 {
			p = numbers(rng, op, lv, os.Remainders)
			if !seen[key{op, p.X, p.Y}] {
				break
			}
		}
		seen[key{op, p.X, p.Y}] = true
		out = append(out, applyFormat(rng, p, formats[i], s.Formats))
	}
	return out
}

// spread repeats items to fill n slots, then shuffles, so a mix stays even.
func spread[T any](rng *rand.Rand, items []T, n int) []T {
	out := make([]T, n)
	for i := range out {
		out[i] = items[i%len(items)]
	}
	rng.Shuffle(n, func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

func pick(rng *rand.Rand, r Range) int { return r[0] + rng.IntN(r[1]-r[0]+1) }

func numbers(rng *rand.Rand, op Op, lv Level, remainders bool) Problem {
	p := Problem{Op: op}
	switch op {
	case Add:
		a, b := pick(rng, lv.A), pick(rng, lv.B)
		for tries := 0; lv.Max > 0 && a+b > lv.Max && tries < 200; tries++ {
			a, b = pick(rng, lv.A), pick(rng, lv.B)
		}
		if lv.Max > 0 && a+b > lv.Max {
			b = max(0, lv.Max-a)
		}
		p.X, p.Y, p.Z = a, b, a+b
	case Sub:
		a, b := pick(rng, lv.A), pick(rng, lv.B)
		if b > a { // never go negative
			a, b = b, a
		}
		p.X, p.Y, p.Z = a, b, a-b
	case Mul:
		a, b := pick(rng, lv.A), pick(rng, lv.B)
		p.X, p.Y, p.Z = a, b, a*b
	case Div:
		if lv.N != (Range{}) { // defined by the size of the dividend
			for range 100 {
				d := pick(rng, lv.D)
				if remainders {
					n := pick(rng, lv.N)
					p.X, p.Y, p.Z, p.Rem, p.HasRem = n, d, n/d, n%d, true
					return p
				}
				qMin, qMax := (lv.N[0]+d-1)/d, lv.N[1]/d
				if qMin > qMax {
					continue
				}
				q := pick(rng, Range{qMin, qMax})
				p.X, p.Y, p.Z = d*q, d, q
				return p
			}
		}
		d := pick(rng, lv.D)
		q := Range{1, 10}
		if lv.Q != (Range{}) {
			q = lv.Q
		}
		z := pick(rng, q)
		p.X, p.Y, p.Z = d*z, d, z
		if remainders {
			p.Rem, p.HasRem = rng.IntN(d), true
			p.X += p.Rem
		}
	}
	return p
}

// applyFormat sets the layout and the blank. A missing-number problem must
// have exactly one right answer, so some fall back to another layout.
func applyFormat(rng *rand.Rand, p Problem, f Format, chosen []Format) Problem {
	p.Format, p.Blank = f, BlankZ
	if f != Missing {
		return p
	}
	if p.HasRem && p.Rem != 0 { // a remainder can't be shown with a missing number
		p.Format = Across
		for _, c := range chosen {
			if c == Stacked {
				p.Format = Stacked
			}
		}
		return p
	}
	var options []Blank
	if p.Op != Mul || p.Y != 0 { // ? × 0 = 0 has every answer
		options = append(options, BlankX)
	}
	if (p.Op != Mul || p.X != 0) && (p.Op != Div || p.Z != 0) {
		options = append(options, BlankY)
	}
	if len(options) == 0 {
		p.Format = Across
		return p
	}
	p.HasRem, p.Rem = false, 0
	p.Blank = options[rng.IntN(len(options))]
	return p
}

// Answer is the number that goes in the blank.
func (p Problem) Answer() int {
	switch p.Blank {
	case BlankX:
		return p.X
	case BlankY:
		return p.Y
	}
	return p.Z
}

// AsksRemainder reports whether the problem has a remainder box.
func (p Problem) AsksRemainder() bool { return p.Blank == BlankZ && p.HasRem }

// Check marks an entry. main and rem are what was typed; an empty remainder
// counts as 0. Anything that is not a plain whole number is wrong.
func (p Problem) Check(main, rem string) bool {
	m, ok := parse(main)
	if !ok || m != p.Answer() {
		return false
	}
	if !p.AsksRemainder() {
		return true
	}
	if rem == "" {
		return p.Rem == 0
	}
	r, ok := parse(rem)
	return ok && r == p.Rem
}

func parse(s string) (int, bool) {
	if s == "" || len(s) > 7 {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// AnswerText is the answer as printed in a key; a zero remainder is left off.
func (p Problem) AnswerText() string {
	if p.AsksRemainder() && p.Rem != 0 {
		return fmt.Sprintf("%d R%d", p.Z, p.Rem)
	}
	return strconv.Itoa(p.Answer())
}

// Equation is the whole fact, for showing after a miss.
func (p Problem) Equation() string {
	rhs := strconv.Itoa(p.Z)
	if p.HasRem && p.Rem != 0 {
		rhs = fmt.Sprintf("%d R%d", p.Z, p.Rem)
	}
	return fmt.Sprintf("%d %s %d = %s", p.X, p.Op.Symbol(), p.Y, rhs)
}

func digits(n int) int { return len(strconv.Itoa(n)) }

// AnswerWidth is a stable width, in digits, for the answer box, so its size
// neither changes while typing nor gives away the answer's length.
func (p Problem) AnswerWidth() int {
	switch p.Blank {
	case BlankX:
		return max(2, digits(p.X))
	case BlankY:
		return max(2, digits(p.Y))
	}
	dx, dy := digits(p.X), digits(p.Y)
	switch p.Op {
	case Add:
		return max(2, max(dx, dy)+1)
	case Mul:
		return max(2, dx+dy)
	}
	return max(2, dx)
}

// RemWidth is the width, in digits, of the remainder box.
func (p Problem) RemWidth() int { return max(2, digits(p.Y)) }

// Columns is how many problems fit across a printed page.
func Columns(ps []Problem) int {
	cols := 5
	for _, p := range ps {
		if p.Format == Stacked {
			w := max(digits(p.X), digits(p.Y), digits(p.Z)) + 1
			if p.Op == Div {
				w = digits(p.X) + digits(p.Y) + 1
			}
			switch {
			case w <= 5:
			case w <= 7:
				cols = min(cols, 4)
			default:
				cols = min(cols, 3)
			}
			continue
		}
		chars := digits(p.X) + digits(p.Y) + digits(p.Z) + 7
		if p.HasRem {
			chars += 4
		}
		switch {
		case chars <= 13:
			cols = min(cols, 4)
		case chars <= 21:
			cols = min(cols, 3)
		default:
			cols = min(cols, 2)
		}
	}
	return cols
}

// NeedsRoom reports whether a stacked problem needs working space below it
// on paper: long multiplication and long division.
func (p Problem) NeedsRoom() bool {
	if p.Format != Stacked {
		return false
	}
	return (p.Op == Mul && p.Y >= 10) || (p.Op == Div && p.X >= 100)
}

// Describe summarises the settings in a line, for history and sheet footers.
func Describe(s Settings) string {
	out := ""
	for _, op := range Ops {
		os := s.Ops[op]
		if !os.On {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += op.Name() + " (" + LevelLabel(op, os.Level)
		if op == Div && os.Remainders {
			out += ", remainders"
		}
		out += ")"
	}
	return out
}
