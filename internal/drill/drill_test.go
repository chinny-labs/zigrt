package drill

import (
	"errors"
	"reflect"
	"testing"
)

func all(count int, formats ...Format) Settings {
	s := Default()
	for _, op := range Ops {
		o := s.Ops[op]
		o.On = true
		s.Ops[op] = o
	}
	s.Formats, s.Count = formats, count
	return s
}

func TestDeterministic(t *testing.T) {
	s := all(50, Across, Stacked, Missing)
	a, b := Generate(s, 42), Generate(s, 42)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("the same seed gave different problems")
	}
	if reflect.DeepEqual(a, Generate(s, 43)) {
		t.Fatal("different seeds gave the same problems")
	}
	if len(a) != 50 {
		t.Fatalf("got %d problems", len(a))
	}
}

// TestInvariants checks every level of every operation, with and without
// remainders, across many seeds.
func TestInvariants(t *testing.T) {
	for _, op := range Ops {
		levels := append([]Level{{ID: CustomLevel}}, Levels[op]...)
		for _, lv := range levels {
			for _, rem := range []bool{false, true} {
				if rem && op != Div {
					continue
				}
				s := Default()
				for _, o := range Ops {
					os := s.Ops[o]
					os.On = o == op
					os.Level, os.Remainders = lv.ID, rem
					s.Ops[o] = os
				}
				s.Formats, s.Count = []Format{Across, Stacked, Missing}, 30
				s, err := Normalize(s)
				if err != nil {
					t.Fatal(err)
				}
				for seed := range uint64(40) {
					for _, p := range Generate(s, seed) {
						check(t, op, lv, p)
					}
				}
			}
		}
	}
}

func check(t *testing.T, op Op, lv Level, p Problem) {
	t.Helper()
	fail := func(why string) { t.Fatalf("%s %s: %s: %+v", op, lv.ID, why, p) }
	if p.Op != op {
		fail("wrong operation")
	}
	if p.X < 0 || p.Y < 0 || p.Z < 0 || p.Rem < 0 {
		fail("a negative number")
	}
	switch op {
	case Add:
		if p.X+p.Y != p.Z {
			fail("the sum is wrong")
		}
		if lv.Max > 0 && p.Z > lv.Max {
			fail("the sum is over the level's limit")
		}
	case Sub:
		if p.X-p.Y != p.Z {
			fail("the difference is wrong")
		}
	case Mul:
		if p.X*p.Y != p.Z {
			fail("the product is wrong")
		}
	case Div:
		if p.Y == 0 {
			fail("division by zero")
		}
		if p.Y*p.Z+p.Rem != p.X || p.Rem >= p.Y {
			fail("the quotient or remainder is wrong")
		}
		if !p.HasRem && p.Rem != 0 {
			fail("a remainder with remainders off")
		}
		if lv.N != (Range{}) && !p.HasRem && (p.X < lv.N[0] || p.X > lv.N[1]) {
			fail("the dividend is outside the level")
		}
	}
	switch p.Format {
	case Missing:
		if p.Blank == BlankZ {
			fail("a missing-number problem with nothing missing")
		}
		if p.HasRem {
			fail("a missing-number problem with a remainder")
		}
		// Exactly one number may fit the blank.
		if (op == Mul && p.Blank == BlankX && p.Y == 0) || (op == Mul && p.Blank == BlankY && p.X == 0) ||
			(op == Div && p.Blank == BlankY && p.Z == 0) {
			fail("a missing number with more than one answer")
		}
	default:
		if p.Blank != BlankZ {
			fail("a blank in an across or stacked problem")
		}
	}
	// The right answer always checks as right, and a wrong one as wrong.
	rem := ""
	if p.AsksRemainder() {
		rem = itoa(p.Rem)
	}
	if !p.Check(itoa(p.Answer()), rem) {
		fail("the right answer was marked wrong")
	}
	if p.Check(itoa(p.Answer()+1), rem) {
		fail("a wrong answer was marked right")
	}
	if w := p.AnswerWidth(); w < len(itoa(p.Answer())) {
		fail("the answer box is narrower than the answer")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for ; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	return s
}

func TestCheck(t *testing.T) {
	p := Problem{Op: Div, X: 17, Y: 5, Z: 3, Rem: 2, HasRem: true, Format: Across, Blank: BlankZ}
	for _, c := range []struct {
		main, rem string
		want      bool
	}{
		{"3", "2", true}, {"3", "", false}, {"3", "1", false}, {"4", "2", false},
		{"", "2", false}, {"3 ", "2", false}, {"-3", "2", false}, {"3.0", "2", false}, {"03", "02", true},
		{"99999999999", "2", false},
	} {
		if got := p.Check(c.main, c.rem); got != c.want {
			t.Errorf("Check(%q, %q) = %v", c.main, c.rem, got)
		}
	}
	exact := Problem{Op: Div, X: 15, Y: 5, Z: 3, HasRem: true, Format: Across, Blank: BlankZ}
	if !exact.Check("3", "") || !exact.Check("3", "0") || exact.Check("3", "1") {
		t.Error("a zero remainder should accept a blank or 0")
	}
	if got := p.AnswerText(); got != "3 R2" {
		t.Errorf("AnswerText = %q", got)
	}
	if got := exact.AnswerText(); got != "3" {
		t.Errorf("AnswerText with no remainder = %q", got)
	}
	if got := p.Equation(); got != "17 ÷ 5 = 3 R2" {
		t.Errorf("Equation = %q", got)
	}
	miss := Problem{Op: Add, X: 7, Y: 5, Z: 12, Format: Missing, Blank: BlankY}
	if miss.Answer() != 5 || !miss.Check("5", "") || miss.Check("12", "") {
		t.Error("missing-number answer")
	}
}

func TestNormalize(t *testing.T) {
	if _, err := Normalize(Settings{}); !errors.Is(err, ErrNothingChosen) {
		t.Fatalf("empty settings: %v", err)
	}
	in := Settings{
		Ops: map[Op]OpSettings{
			Add: {On: true, Level: "no-such-level"},
			Div: {On: true, Level: CustomLevel, CustomA: Range{0, -4}, CustomB: Range{900000, 3}, Remainders: true},
			Mul: {On: false, Level: "facts12", Remainders: true},
		},
		Formats: []Format{"sideways", Missing, Across, Across},
		Count:   100000,
	}
	s, err := Normalize(in)
	if err != nil {
		t.Fatal(err)
	}
	if s.Ops[Add].Level != Levels[Add][1].ID {
		t.Errorf("unknown level became %q", s.Ops[Add].Level)
	}
	if d := s.Ops[Div]; d.CustomA != (Range{1, 1}) || d.CustomB != (Range{3, MaxNumber}) || !d.Remainders {
		t.Errorf("custom division ranges %+v", d)
	}
	if s.Ops[Mul].Remainders || s.Ops[Sub].On {
		t.Errorf("remainders leaked onto ×, or − was turned on: %+v", s.Ops)
	}
	if !reflect.DeepEqual(s.Formats, []Format{Across, Missing}) {
		t.Errorf("formats %v", s.Formats)
	}
	if s.Count != Counts[1] {
		t.Errorf("count %d", s.Count)
	}
	for _, p := range Generate(s, 7) {
		if p.Op == Div && p.Y == 0 {
			t.Fatal("division by zero from custom ranges")
		}
	}
}

func TestNoRepeatsWhenThereIsRoom(t *testing.T) {
	s := Default() // sums to 20: far more than 20 distinct problems
	seen := map[[2]int]bool{}
	for _, p := range Generate(s, 3) {
		k := [2]int{p.X, p.Y}
		if seen[k] {
			t.Fatalf("%d + %d appeared twice", p.X, p.Y)
		}
		seen[k] = true
	}
}

func TestMixIsEven(t *testing.T) {
	counts := map[Op]int{}
	for _, p := range Generate(all(20, Across), 9) {
		counts[p.Op]++
	}
	for _, op := range Ops {
		if counts[op] != 5 {
			t.Fatalf("mix %v", counts)
		}
	}
}

func TestColumnsAndDescribe(t *testing.T) {
	small := []Problem{{Op: Add, X: 7, Y: 5, Z: 12, Format: Across, Blank: BlankZ}}
	big := []Problem{{Op: Mul, X: 9999, Y: 99, Z: 989901, Format: Across, Blank: BlankZ}}
	if Columns(small) != 4 || Columns(big) >= Columns(small) {
		t.Errorf("columns: small %d, big %d", Columns(small), Columns(big))
	}
	long := Problem{Op: Div, X: 144, Y: 12, Z: 12, Format: Stacked, Blank: BlankZ}
	if !long.NeedsRoom() || small[0].NeedsRoom() {
		t.Error("NeedsRoom")
	}
	s := Default()
	d := s.Ops[Div]
	d.On, d.Remainders = true, true
	s.Ops[Div] = d
	if got := Describe(s); got != "Addition (Sums to 20), Division (Division facts to 10, remainders)" {
		t.Errorf("Describe = %q", got)
	}
}
