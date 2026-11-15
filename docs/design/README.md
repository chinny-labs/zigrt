# Design references

Snapshots of the two design sources Zigrt is built from. The live originals are the source of truth; refresh these copies when they change.

| What | Live source | In this repo |
| --- | --- | --- |
| Design plan (architecture, learning model, phasing) | [Zigrt: Design & Architecture](https://claude.ai/artifact/SRqR1CSEiCvG4e8QByRRS8) | [`plan.md`](plan.md) |
| Design board (chosen direction: "Building blocks") | [Zigrt UI directions](https://claude.ai/artifact/CW3vGkUnWarDscu3rHxRSX) | [`board/`](board/) sources, [`screens/`](screens/) renders |

The live canvas also holds two directions that were not chosen ("Night climb" and "Blueprint"). Only the chosen one is copied here.

## Screens

Every screen in section 6 of the plan has a board. Names, numbers and task titles in the boards are placeholder data.

### Student

| # | Board | Render | Notes |
| --- | --- | --- | --- |
| S1 | Who's learning? | [S01-Who.png](screens/S01-Who.png) | A tile per student, quiet parent sign-in link |
| S2 | PIN pad | [S02-Pin.png](screens/S02-Pin.png) | 72 px keys, dots for entered digits |
| S3 | Home | [Main.png](screens/Main.png) | Tower, today's climb, next step, other tasks, bottom nav |
| S4 | Diagnostic intro | [S04-DiagnosticIntro.png](screens/S04-DiagnosticIntro.png) | What it is, how long, that not knowing is fine |
| S5 | Diagnostic question | [S05-DiagnosticQuestion.png](screens/S05-DiagnosticQuestion.png) | Comparison answer type; "I haven't learned this yet"; progress without a score |
| S6 | Lesson explanation | [S06-LessonExplain.png](screens/S06-LessonExplain.png) | Short explanation and the worked-example stepper |
| S7 | Lesson practice | [A-Practice.png](screens/A-Practice.png) | Integer answer: stacked problem, answer box, number pad |
| S7 | Answer: fraction | [S07b-Fraction.png](screens/S07b-Fraction.png) | Top and bottom boxes, "Next box" key, fraction-bar figure |
| S7 | Answer: decimal | [S07c-Decimal.png](screens/S07c-Decimal.png) | Point key; digits line up on the point |
| S7 | Answer: quotient and remainder | [S07d-Remainder.png](screens/S07d-Remainder.png) | "Groups" and "Left over" boxes |
| S7 | Answer: multiple choice | [S07e-Choice.png](screens/S07e-Choice.png) | Up to four large picture buttons; the comparison type is on S5 |
| S8 | Feedback, right | [S08a-FeedbackRight.png](screens/S08a-FeedbackRight.png) | Blue sheet, "Next" |
| S8 | Feedback, not yet | [S08b-FeedbackWrong.png](screens/S08b-FeedbackWrong.png) | Sun-coloured, never red; the worked solution with the missed step marked |
| S9 | Task complete | [S09-Complete.png](screens/S09-Complete.png) | The new block landing on the tower, XP, streak, next task |
| S9 | Milestone earned | [S09b-Milestone.png](screens/S09b-Milestone.png) | The one full-blue screen: a tier is finished |
| S10 | Lesson paused | [S10-Paused.png](screens/S10-Paused.png) | Shown after three misses in one step |
| S11 | Quiz results | [S11-QuizResults.png](screens/S11-QuizResults.png) | Each problem with "You got it" or "See how" |
| S12 | Tower (progress) | [S12-Tower.png](screens/S12-Tower.png) | Tiers, strand faces, topic blocks in five states, one topic's detail |
| S12 | Tower milestone states | [S12b-Milestones.png](screens/S12b-Milestones.png) | Spec board: tier and face states, the sun cap and flag |
| S13 | Drill builder | [S13-DrillBuilder.png](screens/S13-DrillBuilder.png) | Operations, level, style, count, presets |
| S13 | Drill from my blocks | [S13b-TopicPicker.png](screens/S13b-TopicPicker.png) | Pick placed topics to build a drill from |
| S14 | Drill | [S14-Drill.png](screens/S14-Drill.png) | Across layout, count, sound toggle |
| S15 | Drill results | [S15-DrillResults.png](screens/S15-DrillResults.png) | Score, time, misses, "Drill just these" |
| S16 | Worksheet preview | [S16-Worksheet.png](screens/S16-Worksheet.png) | The sheet in black on white, print options |

### Parent

| # | Board | Render | Notes |
| --- | --- | --- | --- |
| P0 | First-run setup | [P0-Setup.png](screens/P0-Setup.png) | Setup token, first parent account, time zone |
| P1 | Sign-in | [P1-SignIn.png](screens/P1-SignIn.png) | Email and password; pointer to first-run setup |
| P2 | Dashboard | [P2-Dashboard.png](screens/P2-Dashboard.png) | A card per student; "Stuck on" flag |
| P3 | One student | [P3-Student.png](screens/P3-Student.png) | Stuck explanation, XP per day, activity feed |
| P4 | Student settings | [P4-StudentSettings.png](screens/P4-StudentSettings.png) | PIN, course, daily goal, rest days, fluency timing, overrides |
| P5 | Household settings | [P5-Household.png](screens/P5-Household.png) | Parents, time zone, email, content pack, backups, API tokens |
| P6 | Notifications | [P6-Notifications.png](screens/P6-Notifications.png) | New items outlined; inline actions |
| P7 | API tokens | [P7-ApiTokens.png](screens/P7-ApiTokens.png) | New token shown once, tokens in use, make a token |

### Tablet and desktop

| Board | Render | Notes |
| --- | --- | --- |
| Tablet · Home | [A-Tablet-Home.png](screens/A-Tablet-Home.png) | Two columns, nav moves to the top bar |
| Tablet · Practice | [A-Tablet-Practice.png](screens/A-Tablet-Practice.png) | Number pad beside the problem |
| Tablet · Feedback, not yet | [A-Tablet-Feedback.png](screens/A-Tablet-Feedback.png) | Problem on the left, worked steps on the right |
| Tablet · Tower | [A-Tablet-Tower.png](screens/A-Tablet-Tower.png) | Tiers on the left, the chosen face's blocks on the right |
| Tablet · Every other screen | [A-Tablet-Column.png](screens/A-Tablet-Column.png) | The rule: the phone layout as a centred 440 px column (PIN pad shown) |
| Desktop · Home | [A-Desktop-Home.png](screens/A-Desktop-Home.png) | Fluid page; stacks to one column when narrow |
| Desktop · Parent dashboard | [A-Desktop-Dashboard.png](screens/A-Desktop-Dashboard.png) | Student cards in a row; parent nav in the header |
| Desktop · One student | [A-Desktop-Student.png](screens/A-Desktop-Student.png) | Stuck banner and chart beside the activity feed |

Layout rule for wide screens: home, practice (and the diagnostic, review, quiz and drill screens that share its layout), feedback, the tower, the parent dashboard and the one-student page have their own two-column layouts, drawn above. Every other screen is its phone layout in a centred column at most 440 px wide.

### Dark theme

| Board | Render |
| --- | --- |
| Home | [S03-HomeDark.png](screens/S03-HomeDark.png) |
| Practice | [D-Practice.png](screens/D-Practice.png) |
| Feedback, not yet | [D-FeedbackWrong.png](screens/D-FeedbackWrong.png) |
| Tower | [D-Tower.png](screens/D-Tower.png) |
| Parent dashboard | [D-Dashboard.png](screens/D-Dashboard.png) |

Dark mode swaps token values only, so the remaining screens follow from these five.

The `board/*.dc.html` files are the canvas's Design Component sources. They need the canvas runtime to render exactly; `tools/` holds a minimal stand-in (`support.js`) and a Playwright script used to make the PNGs:

```sh
mkdir -p /tmp/board && cp docs/design/board/* docs/design/tools/support.js /tmp/board/
node docs/design/tools/render.mjs /tmp/board docs/design/screens   # needs the playwright package
```

## Tokens ("Building blocks")

Taken from the boards. The app's stylesheet (`internal/web/static/app.css`) should define these as CSS custom properties; dark mode swaps the values only.

| Token | Light | Dark | Use |
| --- | --- | --- | --- |
| `--zg-ground` | `#F2F6FF` | `#0E1433` | Page background |
| `--zg-surface` | `#FFFFFF` | `#1A2250` | Cards, keys, nav |
| `--zg-track` | `#DCE4FA` | `#2A3470` | Progress tracks, key edge, hairlines |
| `--zg-block-idle` | `#E4E9F7` | `#2A3470` | Tower tiers and topic blocks still to come |
| `--zg-ink` | `#1B2559` | `#F1F4FF` | Text |
| `--zg-ink-muted` | `#4A5480` | `#B9C2EE` | Secondary text, inactive nav |
| `--zg-blue` | `#2F5BEA` | `#6C8CFF` | Primary action, mastered tiers and blocks, wordmark |
| `--zg-on-blue` | `#FFFFFF` | `#0B1233` | Text on blue |
| `--zg-blue-edge` | `#1D3FB3` | `#3F5FD6` | The pressable bottom edge under a primary button |
| `--zg-sun` | `#FFC533` | `#FFC533` | In progress: the current tier, the active step, XP chips |
| `--zg-on-sun` | `#1B2559` | `#1B2559` | Text on sun and its tints, in both themes |
| `--zg-sun-tint` | `#FFE9A8` | `#FFE9A8` | Unfilled part of the current tier |
| `--zg-sun-wash` | `#FFF3CC` | `#FFF3CC` | Answer box, "not yet" feedback, "stuck" banners |
| `--zg-sun-ink` | `#B87400` | `#FFC533` | Sun-coloured icons and labels on a surface; the "needs mending" ring |
| `--zg-sun-mark` | `#E09A00` | `#FFC533` | The raised last letter of the wordmark |

The sun tints keep their light values in dark mode, with dark text on them, so the answer box, the "not yet" banner and the "stuck" chip read the same in both themes. In dark mode the pressable edge under a surface-coloured key is `--zg-track`, and the selected tier or card is outlined in `--zg-ink`.

- **Meaning of colour:** blue is done or do-this, sun is in progress or needs attention, grey-blue is still to come. There is no red and no green: a wrong answer, a paused lesson and a stuck student are all sun. Status always comes with words as well ("done", "you are here"), never colour alone.
- **Type:** Fredoka (display, digits, buttons; 600/700) over Nunito (body and labels; 700/800). Phone scale: problem digits 64, display 26, card title 20, body 16, label 13 to 15. Tablet and desktop scale the display to 32 to 42 and problem digits to 104. The boards load both families from Google Fonts; the app must self-host them, because its CSP allows no CDN assets.
- **Radius:** tower blocks 9 (phone) to 14, small cards and rows 18 to 20, buttons 22 to 26, cards 24 to 28, pills fully round.
- **Depth:** no drop shadows. A pressable control has a solid 4 to 6 px bottom edge in a darker shade, like a block standing on a table.
- **Touch targets:** at least 44 px. Number-pad keys are 60 px tall on a phone and 84 px on a tablet; PIN keys are 72; the Check button is 64 and 84.
- **Wordmark:** "zigrt" in Fredoka 700, each letter set 3 to 4 px higher than the one before it, the last letter in `--zg-sun-mark`.
- **Tower:** stacked tiers, widest at the bottom, one per grade band. The current tier is slightly taller, outlined in sun, and filled from the left by the share of its topics mastered.
- **Milestones:** the only rewards. A finished tier gets a sun cap along its top edge and a flag; a finished face gets a sun outline and a flag on its chip. A milestone is never taken away, even when a block inside needs mending.
- **Answer inputs:** the box being typed in is sun wash with a sun outline and a caret; a filled box that is not active is white with an ink-muted outline. Two-box answers (fraction, remainder) swap "Clear" for a "Next box" key; decimals swap it for a point key.
- **Topic blocks (S12):** placed is solid blue; needs mending is blue with a sun-ink ring; building is sun; ready is white with a blue outline; later is grey-blue. Each state also has a word in the key.
- **Parent screens:** the same tokens, with white cards and list rows; pressable edges only on buttons.
- **Layout:** phone is one column with a bottom nav bar. From tablet width up, the nav becomes a pill bar in the header, home splits into two columns (tower and tasks), and practice puts the number pad beside the problem.
