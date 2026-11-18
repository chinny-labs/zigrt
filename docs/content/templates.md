# Writing content

How a content pack is laid out, how to write a problem template, and how to check your work with `zigrt-content`. The design is in the plan (sections 4.1 to 4.8) and ADRs 9 and 10. `content/testdata/hardest` in the app repo is a complete small pack: the 20 hardest K-5 topics, written to test this format.

Every template example on this page is parsed and checked by the app's tests, so they stay correct.

## The pack

```text
manifest.yaml        id, title, version (2027.02.1), min_app_version, licence, locales, courses
topics.yaml          strands, every topic in teaching order, transitions
topics/<id>.yaml     one lesson per topic: its knowledge points and their templates
figures/<name>.svg   static pictures with no arguments (optional)
released-ids.json    every ID ever released (the release job writes it)
```

A topic in `topics.yaml`:

```yaml
- id: long-division-2d            # never renamed or reused once released
  title: Long division by 2 digits
  strand: division
  grade: "5"                      # K, 1 to 5; quote the numbers
  requires: [long-division-1d, mult-2d-2d]
  encompasses: [{topic: long-division-1d, weight: 1.0}]
  xp: 15
  standards: [5.NBT.B.6]          # shown to parents only
  fluency_seconds: 0              # optional
```

A lesson, `topics/<id>.yaml`, has 1 to 4 knowledge points. IDs nest: `topic.kpN`, then `topic.kpN.tN`.

```yaml
id: long-division-2d
kps:
  - id: long-division-2d.kp1
    explain: >-
      Round the divisor to the nearest ten to guess each digit.
    example: long-division-2d.kp1.t1   # optional; the first template by default
    templates:
      - id: long-division-2d.kp1.t1
        # ...
```

Explanations are short: the longest sentence allowed is 10 words in kindergarten, then 12, 14, 16, 18 and 20 by grade 5. Nothing in a pack may link outside it.

## A template

```yaml
id: add-2d-2d-regroup.kp2.t1
vars:
  a: {int: [10, 99]}
  b: {int: [10, 99]}
where:
  - (a % 10) + (b % 10) >= 10      # forces regrouping in the ones
  - a + b <= 99
prompt: "{a} + {b}"
layout: stacked
op: "+"
parts: [a, b]
answer: {type: integer, value: a + b}
solution:
  - "Add the ones: {a % 10} + {b % 10} = {(a % 10) + (b % 10)}. Write {(a + b) % 10}, carry 1."
  - "Add the tens: {a // 10} + {b // 10} + 1 = {(a + b) // 10}."
  - "{a} + {b} = {a + b}"
```

A problem is a template plus a seed. The same seed always gives the same problem.

### Variables

Drawn in the order written; a variable can use the ones before it.

| Write | Gives |
| --- | --- |
| `{int: [1, 9]}` | a whole number from 1 to 9 |
| `{int: [5, 55], step: 5}` | 5, 10, ... 55 |
| `{int: [1, d - 1]}` | bounds can be expressions |
| `{choose: [2, 4, 5, 10]}` | one of the values (each an expression) |
| `{words: [Mika, Theo, June]}` | one of the words, as text |
| `a * b`, or `{let: a * b}` | a computed value |

### Conditions

`where` lists conditions that must all be true. Each is checked as soon as the variables it reads have values, so a later variable can rely on it. When one is false the template draws again, up to 100 times.

```yaml
id: guarded-division
vars:
  b: {int: [0, 9]}
  q: 36 / b
where: ["b != 0"]          # checked before q is worked out
prompt: "36 ÷ {b}"
answer: {type: fraction, value: q}
min_variety: 9
solution: ["36 ÷ {b} = {q}"]
```

An error while evaluating (dividing by zero, a missing variable) is a bug, not bad luck: it fails at once.

Filtering is slow when few draws pass. Build the number from its parts instead: "a 9 followed by 5 or more" passes one draw in 20, but `n: hi * 10 * place + 9 * place + lo` with `lo: {int: [place / 2, place - 1]}` always works.

### Expressions

Every number is an exact fraction: `1/3 + 1/6` is `1/2`, and `0.1 + 0.2 == 0.3` is true.

| | |
| --- | --- |
| `+ - * /` | `/` is exact: `7 / 2` is 7/2 |
| `//`, `%` | whole-number division rounding down, and its remainder: `17 // 5` is 3, `17 % 5` is 2 |
| `^` | a whole power from 0 to 12 |
| `== != < <= > >=` | comparisons; write `a < b && b < c`, not `a < b < c` |
| `&& \|\| !` | and, or, not |
| `c ? x : y` | x when c is true, else y |
| `'text'` or `"text"` | text |

Functions (`zigrt-content funcs` prints this list):

| | |
| --- | --- |
| `gcd(a, b, ...)`, `lcm(a, b, ...)` | greatest common factor, least common multiple |
| `min`, `max`, `abs` | |
| `floor(x)`, `ceil(x)` | round down, round up |
| `round(x)`, `round(x, 100)`, `round(x, 0.1)` | to a whole number, the nearest hundred, the nearest tenth; halves round up |
| `num(x)`, `den(x)` | top and bottom in lowest terms |
| `digit(4706, 100)` | the hundreds digit, 7; `digit(3.25, 0.1)` is 2 |
| `isint(x)` | true for a whole number |
| `in(x, a, b, c)` | true when x is one of them |
| `pick(i, a, b, c)` | a when i is 0, b when 1, and so on |
| `dec(x)`, `dec(x, 2)` | write a decimal; at least 2 places, as in money |
| `frac(x)`, `mixed(x)` | write 7/4, or 1 3/4 |
| `group(12345)` | write 12,345 |
| `pad(5, 2)` | write 05, as in 3:05 |
| `plural(n, 'apple', 'apples')` | the right word for n |
| `text(x)`, `join(a, b, ...)` | turn a value into text, put texts together |

Each problem has a budget of 10,000 steps, and no number may grow past about 15 digits.

### Text

`{expression}` in a prompt, an option or a solution line fills in a value. A whole number shows plainly; any other number shows as a fraction in lowest terms unless you use `dec`, `mixed` or `group`. Write `{{` and `}}` for braces.

Text can be a plain string, which is English, or one string per locale:

```yaml
id: apples
vars:
  who: {words: [Mika, Theo, June]}
  n: {int: [2, 9]}
  m: {int: [1, 9]}
prompt:
  en: "{who} has {n} apples and picks {m} more. How many now?"
  fr: "{who} a {n} pommes et en cueille {m} de plus. Combien maintenant ?"
answer: {type: integer, value: n + m}
min_variety: 20
solution:
  - en: "{n} + {m} = {n + m}. {who} has {n + m} {plural(n + m, 'apple', 'apples')}."
    fr: "{n} + {m} = {n + m}."
```

In YAML flow lists (`[...]`), quote an expression that contains a comma: `parts: ["dec(a, 2)", b]`.

### Layouts

| `layout` | Draws | Needs |
| --- | --- | --- |
| `text` (the default) | the prompt alone, as in a word problem | |
| `across`, `stacked` | 47 + 25 written across, or in columns | `op` (`+ - * /`) and two `parts` |
| `missing` | a number sentence with a box | `op`, three `parts` (the last is the result) and `blank` (0, 1 or 2) |
| `long-division` | the long division bracket | `parts: [dividend, divisor]` |
| `figure` | a picture | `figure` and `figure_args` |

Figures the app draws, with the arguments each takes:

| `figure` | `figure_args` |
| --- | --- |
| `fraction-bar` | `parts`, `shaded` |
| `number-line` | `from`, `to`, `step`, `mark` |
| `clock` | `hour`, `minute` |
| `rectangle` | `width`, `height`, `unit` |
| `prism` | `length`, `width`, `height`, `unit` |
| `coordinate` | `size`, `x`, `y` |
| `array` | `rows`, `cols` |
| `dots` | `count` (0 to 20), `arrange` (`line`, `scatter`, or `dice` for 1 to 6) |
| `dot-groups` | `a`, `b` (0 to 10), `style`: `apart` (two groups to put together), `rows` (lined up to compare) or `crossed` (`a` dots, the last `b` crossed out) |
| `ten-frame` | `frames` (1 or 2), `filled` |
| `shape` | `name` (circle, hexagon, octagon, parallelogram, pentagon, rectangle, rhombus, square, trapezoid, triangle), `turn` (degrees) |
| `shape-mix` | `circles`, `triangles`, `squares` (0 to 8 each, 1 to 15 in all), jumbled |
| `solid` | `name` (box, cone, cube, cylinder, pyramid, sphere) |
| `compare` | `a`, `b`, `c` (1 to 10; `c` 0 for two), `kind`: `length` (bars named A, B, C) or `height` (towers) |
| `balance` | `tip`: `left`, `right` or `level` (a box on the left, a ball on the right) |
| `position` | `place`: `above`, `below`, `left`, `right` or `inside` (a ball and a box) |
| `base-ten` | `hundreds` (0 to 9), `tens`, `ones` (0 to 19 each): flats, rods and cubes |
| `ruler` | `start`, `length` (whole, halves or quarters), `max` (2 to 15), `unit` (`cm`, `in`, any word, or `cubes` for a row of unit cubes from 0): a pencil above a ruler |
| `table` | `rows` (`"Cats:4\|Dogs:7"`, 2 to 5 rows, counts 0 to 25), `style`: `number` or `tally` |
| `cut-shape` | `name` (`circle`, `square`, `rectangle`), `parts` (2 to 8), `equal` (1, or 0 for unequal parts, 2 to 4 of them), `shaded` |

Any other name must be a static picture in `figures/<name>.svg`, which takes no arguments. Packs are untrusted, so a static figure is shown only as cleaned: drawing elements (`svg`, `g`, `path`, `rect`, `circle`, `ellipse`, `line`, `polyline`, `polygon`, `text`, `tspan`, `title`, `desc`) with geometry and paint attributes. A script, style, link, `use`, image, event handler or `url(...)` fails the subresources gate. A file is at most 64 KB.

Word arguments (`arrange`, `style`, `name`, `kind`, `tip`, `place`, `unit`, `rows`) are text: write them quoted (`arrange: "'dice'"`) or from a `choose` variable. A shape's or solid's picture never names it, so "What shape is this?" doesn't give itself away.

Each catalogue figure has limits it can draw: a fraction bar of 1 to 24 parts, an array up to 12 by 12, a number line of at most 40 jumps with its mark on the line, a clock from 1:00 to 12:59, a grid of size 2 to 20 with the point on it. The `figures` gate draws every generated figure, so a template that strays outside fails `check`.

```yaml
id: shade-a-fraction
vars:
  d: {choose: [2, 3, 4, 5, 6, 8, 10, 12]}
  n: {int: [1, d - 1]}
prompt: "What fraction is shaded?"
layout: figure
figure: fraction-bar
figure_args: {parts: d, shaded: n}
answer: {type: fraction, value: n / d, simplest: false}
min_variety: 20
solution: ["{n} of {d} parts are shaded: {n}/{d}"]
```

### Answers

| `type` | Fields | Accepts |
| --- | --- | --- |
| `integer` | `value` | 347, or 12,345 with separators |
| `decimal` | `value` (its decimal must end) | 3.75, 3.750 |
| `fraction` | `value`, `simplest` (default true) | 7/4 or 1 3/4. When `simplest` is true, 6/8 for 3/4 is not marked right; the student is told to simplify |
| `quotient_remainder` | `value`, `remainder` | 12 r 3; the remainder box may be left empty when it is 0 |
| `comparison` | `compare: [left, right]` | <, = or > |
| `choice` | `right`, and 1 to 3 `distractors` (text) | the option picked; options are shuffled per seed |

Make distractors real mistakes, so a wrong answer tells the student something:

```yaml
id: carry-mistake
vars:
  a: {int: [15, 49]}
  b: {int: [15, 49]}
where: ["a % 10 + b % 10 >= 10"]
prompt: "{a} + {b} = ?"
answer:
  type: choice
  right: "{a + b}"
  distractors:
    - "{a + b - 10}"          # forgot to carry
    - "{a + b + 10}"
solution: ["{a} + {b} = {a + b}"]
```

When two options would read the same, the template draws again.

### Worked solutions

One line per step. The last line must state the answer. A step that does not always apply takes a `when`:

```yaml
id: elapsed-minutes
vars:
  h1: {int: [1, 9]}
  m1: {int: [5, 55], step: 5}
  dur: {int: [40, 170], step: 5}
  end: h1 * 60 + m1 + dur
  h2: end // 60
  m2: end % 60
where: ["h2 > h1 && h2 <= 12"]
prompt: "A film starts at {h1}:{pad(m1, 2)} and ends at {h2}:{pad(m2, 2)}. How many minutes long is it?"
answer: {type: integer, value: dur}
solution:
  - "From {h1}:{pad(m1, 2)} to {h1 + 1}:00 is {60 - m1} minutes."
  - when: h2 - h1 > 1
    text: "From {h1 + 1}:00 to {h2}:00 is {60 * (h2 - h1 - 1)} minutes."
  - when: m2 > 0
    text: "From {h2}:00 to {h2}:{pad(m2, 2)} is {m2} minutes."
  - "The film lasts {dur} minutes."
```

## Checking your work

```sh
zigrt-content lint                      # structure, IDs, edges, template syntax (fast)
zigrt-content lint long-division-2d     # only the problems that mention one topic
zigrt-content sample long-division-2d.kp1.t1 -seeds 1-20
zigrt-content check                     # every gate, as CI runs it
zigrt-content graph -strand fractions   # the graph by grade; flags unreachable topics
zigrt-content funcs                     # the expression functions
zigrt-content build -out dist           # the pack file and its checksum
```

To see lessons as a child will, with the app's own layouts, figures and inputs, render them to pages and open `preview/index.html` (ADR 13):

```sh
zigrt preview -pack . add-2d-2d-regroup tell-time-5min   # some topics, or none for all
```

Read `sample` output, not just `check`. The gates catch broken templates; only reading catches "From 7:00 to 7:00 is 0 hours".

The gates (plan section 4.6):

| Gate | Fails when |
| --- | --- |
| structure | a cycle, an edge to a missing topic, a topic without a lesson, a misspelt key, a bad ID |
| ids | an ID in `released-ids.json` is gone without a `transitions` entry |
| generate | a seed finds no problem, or 1,000 seeds give fewer than 30 different problems (`min_variety` lowers this for fixed questions) |
| answers | a non-whole integer answer, a decimal that never ends, a negative answer, the right answer not marked right or a wrong one marked right |
| solutions | the last line does not state the answer |
| figures | a generated figure cannot be drawn (too many parts, a mark off the line) |
| readability | an explanation sentence too long for the grade |
| subresources | a link outside the pack, or a static figure that does not clean |

In the curriculum repo the tool is pinned with a `tool` line in `go.mod` (ADR 10), so it is run as `go tool zigrt-content check`.
