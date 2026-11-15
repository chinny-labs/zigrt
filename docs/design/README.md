# Design references

Snapshots of the two design sources Zigrt is built from. The live originals are the source of truth; refresh these copies when they change.

| What | Live source | In this repo |
| --- | --- | --- |
| Design plan (architecture, learning model, phasing) | [Zigrt: Design & Architecture](https://claude.ai/artifact/SRqR1CSEiCvG4e8QByRRS8) | [`plan.md`](plan.md) |
| Design board (chosen direction: "Building blocks") | [Zigrt UI directions](https://claude.ai/artifact/CW3vGkUnWarDscu3rHxRSX) | [`board/`](board/) sources, [`screens/`](screens/) renders |

The live canvas also holds two directions that were not chosen ("Night climb" and "Blueprint"). Only the chosen one is copied here.

## Screens

Five screens exist so far. The rest of the screens in section 6 of the plan are still to be designed in this style.

| Board | Render | Notes |
| --- | --- | --- |
| Student · Home (phone) | [Main.png](screens/Main.png) | Tower, today's climb, next step, other tasks, bottom nav |
| Student · Lesson practice (phone) | [A-Practice.png](screens/A-Practice.png) | Stacked problem, answer box, number pad |
| Student · Home (tablet, landscape) | [A-Tablet-Home.png](screens/A-Tablet-Home.png) | Two columns, nav moves to the top bar |
| Student · Lesson practice (tablet, landscape) | [A-Tablet-Practice.png](screens/A-Tablet-Practice.png) | Number pad beside the problem |
| Student · Home (desktop) | [A-Desktop-Home.png](screens/A-Desktop-Home.png) | Fluid page; stacks to one column when narrow |

Names, numbers and task titles in the boards are placeholder data.

The `board/*.dc.html` files are the canvas's Design Component sources. They need the canvas runtime to render exactly; `tools/` holds a minimal stand-in (`support.js`) and a Playwright script used to make the PNGs:

```sh
mkdir -p /tmp/board && cp docs/design/board/* docs/design/tools/support.js /tmp/board/
node docs/design/tools/render.mjs /tmp/board docs/design/screens   # needs the playwright package
```

## Tokens ("Building blocks")

Taken from the boards. The app's stylesheet (`internal/web/static/app.css`) should define these as CSS custom properties. Only the light theme is designed; dark values are still to do.

| Token | Light | Use |
| --- | --- | --- |
| `--zg-ground` | `#F2F6FF` | Page background |
| `--zg-surface` | `#FFFFFF` | Cards, keys, nav |
| `--zg-track` | `#DCE4FA` | Progress tracks, key edge shadow, muted text on blue |
| `--zg-block-idle` | `#E4E9F7` | Tower tiers still to come, hairlines |
| `--zg-ink` | `#1B2559` | Text |
| `--zg-ink-muted` | `#4A5480` | Secondary text, inactive nav |
| `--zg-blue` | `#2F5BEA` | Primary action, mastered tiers, wordmark |
| `--zg-blue-edge` | `#1D3FB3` | The pressable bottom edge under a primary button |
| `--zg-sun` | `#FFC533` | In progress: the current tier, the active step, XP chips |
| `--zg-sun-tint` | `#FFE9A8` | Unfilled part of the current tier |
| `--zg-sun-wash` | `#FFF3CC` | Answer box fill |
| `--zg-sun-ink` | `#B87400` | Icons drawn in the sun colour on white |
| `--zg-sun-mark` | `#E09A00` | The raised last letter of the wordmark |

- **Meaning of colour:** blue is done or do-this, sun is in progress, grey-blue is still to come. Status always comes with words as well ("done", "you are here"), never colour alone.
- **Type:** Fredoka (display, digits, buttons; 600/700) over Nunito (body and labels; 700/800). Phone scale: problem digits 64, display 26, card title 20, body 16, label 13 to 15. Tablet and desktop scale the display to 32 to 42 and problem digits to 104. The boards load both families from Google Fonts; the app must self-host them, because its CSP allows no CDN assets.
- **Radius:** tower blocks 9 (phone) to 14, small cards and rows 18 to 20, buttons 22 to 26, cards 24 to 28, pills fully round.
- **Depth:** no drop shadows. A pressable control has a solid 4 to 6 px bottom edge in a darker shade, like a block standing on a table.
- **Touch targets:** at least 44 px. Number-pad keys are 60 px tall on a phone and 84 px on a tablet; the Check button is 64 and 84.
- **Wordmark:** "zigrt" in Fredoka 700, each letter set 3 to 4 px higher than the one before it, the last letter in `--zg-sun-mark`.
- **Tower:** stacked tiers, widest at the bottom, one per grade band. The current tier is slightly taller, outlined in sun, and filled from the left by the share of its topics mastered.
- **Layout:** phone is one column with a bottom nav bar. From tablet width up, the nav becomes a pill bar in the header, home splits into two columns (tower and tasks), and practice puts the number pad beside the problem.
