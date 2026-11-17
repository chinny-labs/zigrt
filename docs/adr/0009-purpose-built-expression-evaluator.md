# 9. Templates use a purpose-built expression language over exact fractions

Date: 2026-11-17 · Status: accepted

## Context

Problem templates (plan section 4.2) compute numbers, test conditions and fill in text. The plan left one question open: use `cel-go`, or write a small evaluator with exact fractions as a native type. It asked for the 20 hardest K-5 topics to be written against both before deciding.

The 20 topics are in `content/testdata/hardest`: borrowing across zeros, long division by one and two digits, remainders in word problems, equivalent, compared, added, multiplied and divided fractions, mixed numbers with regrouping, fractions as decimals, adding and multiplying decimals, making change, elapsed time, unit conversion, volume and coordinates. The same expressions were tried in cel-go v0.33.2 (the module has moved to `cel.dev/cel-go`):

| | cel-go | Purpose-built |
| --- | --- | --- |
| `1/3 + 1/6` | `0`: `/` on integers truncates | `1/2` |
| Fractions | an opaque type with named functions, `add(rat(1, 3), rat(1, 6))`. `+` cannot be overloaded for a new type ("singleton function incompatible with specialized overloads") | native: every number is an exact fraction |
| Decimals and money | doubles: `0.1 + 0.2 == 0.3` is false, `1.1 * 3.0` is 3.3000000000000003 | exact: true, and 3.3 |
| Mixed arithmetic | `a + 0.5` is a type error (int plus double) | works |
| Step budget | yes (cost limit) | yes |
| Size of a minimal program | 17.2 MB, 70 non-standard packages | 2.8 MB, none |

Nine of the 24 templates need fractions or decimals. In cel-go each would be written with `rat(...)` and named functions, and an author who forgets once gets integer division or a float with no error.

## Decision

Templates use a small purpose-built language, `problem/expr` (about 1,100 lines with its functions).

- Every number is an exact rational (`math/big.Rat`). `/` is exact division, `//` is whole-number division rounding down, `%` the remainder that goes with it, `^` a whole power up to 12.
- Values are numbers, true/false and text. There are no loops, no lists, no user functions and no I/O.
- Every evaluation has a step budget (10,000 operations per problem) and a size limit (50-bit numerators and denominators), so a pack cannot make the server do unbounded work. Unknown functions and wrong argument counts are errors when the pack loads.
- The functions are those the 20 topics needed: `gcd`, `lcm`, `min`, `max`, `abs`, `floor`, `ceil`, `round` (to a unit, halves up), `num`, `den`, `digit`, `isint`, `in`, `pick`, and for text `dec`, `frac`, `mixed`, `group`, `pad`, `plural`, `text`, `join`. `zigrt-content funcs` lists them.

Writing the topics also settled the template format:

- `vars` are drawn in the order written: `{int: [lo, hi], step: s}`, `{choose: [...]}`, `{words: [...]}`, or an expression. Bounds can use earlier variables.
- Each `where` condition is checked as soon as the variables it reads exist, so a later variable can rely on it (`b != 0`, then `a / b`). A false condition redraws (up to 100 times); an evaluation error is a bug in the template and fails at once.
- Worked solution lines can be conditional, `{when: ..., text: ...}`, so a step that does not apply (a zero-hour jump, a fraction that is already over the common denominator) is left out rather than shown as "0 hours". The last line, which states the answer, is never conditional.
- Text is a string, which is English, or a map from locale to string. `{expression}` fills a value in; `{{` and `}}` are literal braces.
- Figures are a fixed catalogue drawn by the app from named arguments (`fraction-bar`, `number-line`, `clock`, `rectangle`, `prism`, `coordinate`, `array`), because a figure such as "n of d parts shaded" repeats a shape a varying number of times, which a static SVG with placeholders cannot do. A pack may add static SVGs with no arguments in `figures/`.

## Consequences

- The plan's own example template wrote `{a / 10}` for the tens digit. In this language that is 5.3, not 5; it is `a // 10`. The plan is corrected.
- Prefer building a number from its parts to filtering random draws: "a 9 followed by 5 or more" accepts about one draw in 20, and failed the generate gate until rewritten.
- In YAML flow lists an expression with a comma must be quoted: `parts: ["dec(a, 2)", b]`.
- The language is ours to maintain. New functions are added when a topic needs one, with a test, and old ones never change meaning, since released templates depend on them.
- Lessons store a template ID and seed (plan section 8), so a released template must keep generating the same problems. Text can be corrected; changing what a template generates means a new template ID. A gate that checks this against the previous pack is follow-up work for the curriculum repo.
