// Takes the README and website screenshots from the demo household and a
// few lesson pages from the pack preview. `just screenshots` sets it up:
//
//   BASE_URL=http://127.0.0.1:18082 PASSWORD=... PREVIEW=/tmp/x/preview OUT=../docs/screenshots node screenshots.mjs
//
// Desktop shots are 1000×800 and phone shots 390×844, both at 2×. Set
// CHROMIUM_PATH to use a browser other than Playwright's own.
import { chromium } from 'playwright';
import { mkdir } from 'node:fs/promises';
import { pathToFileURL } from 'node:url';

const base = process.env.BASE_URL ?? 'http://127.0.0.1:18082';
const password = process.env.PASSWORD;
const preview = process.env.PREVIEW;
const out = process.env.OUT ?? '../docs/screenshots';
if (!password || !preview) throw new Error('PASSWORD and PREVIEW must be set');
await mkdir(out, { recursive: true });

const browser = await chromium.launch(process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {});
const desktop = { viewport: { width: 1000, height: 800 }, deviceScaleFactor: 2 };
const phone = { viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true };

async function shot(p, name, path) {
  if (path) await p.goto(path.startsWith('file:') ? path : base + path);
  await p.evaluate(() => document.fonts.ready);
  await p.mouse.move(0, 0); // no hover state left over from the last click
  await p.screenshot({ path: `${out}/${name}.png` });
  console.log(`· ${name}.png`);
}

async function parent(opts, scheme = 'light') {
  const p = await (await browser.newContext({ ...opts, colorScheme: scheme })).newPage();
  await p.goto(base + '/parent/login');
  await p.fill('#email', 'parent@demo.zigrt.invalid');
  await p.fill('#password', password);
  await p.locator('main button[type=submit]').click();
  await p.waitForURL('**/parent');
  return p;
}

async function student(name, pin, scheme = 'light') {
  const p = await (await browser.newContext({ ...phone, colorScheme: scheme })).newPage();
  await p.goto(base + '/');
  await p.getByRole('link', { name }).click();
  for (const k of pin) await p.click(`[data-key="${k}"]`);
  await p.getByRole('button', { name: 'Go' }).click();
  await p.getByText(`Hi ${name}.`).waitFor();
  return p;
}

const studentPath = async (p, name) => {
  await p.goto(base + '/parent');
  return new URL(await p.locator('main a', { hasText: name }).first().getAttribute('href'), base).pathname;
};

try {
  // The parent side, on a desktop.
  const sam = await parent(desktop);
  await shot(sam, 'parent-dashboard', '/parent');
  const ada = await studentPath(sam, 'Ada');
  await shot(sam, 'parent-student', ada);
  await shot(sam, 'parent-tower', ada + '/tower');
  // Starting a child at a grade: just the card.
  await sam.goto(base + ada + '/settings');
  await sam.evaluate(() => document.fonts.ready);
  await sam.locator('section.card', { has: sam.locator('h2', { hasText: 'Start at a grade' }) }).screenshot({ path: `${out}/start-grade.png` });
  console.log('· start-grade.png');
  await shot(await parent(desktop, 'dark'), 'parent-dashboard-dark', '/parent');

  // Signing in and the student side, on a phone.
  const who = await (await browser.newContext(phone)).newPage();
  await shot(who, 'who', '/');
  await who.getByRole('link', { name: 'Ada' }).click();
  await who.locator('[data-key="2"]').waitFor();
  await shot(who, 'pin-pad');

  const kid = await student('Ada', '2468');
  await shot(kid, 'student-home', '/');
  await shot(kid, 'student-tower', '/tower');
  // Today's next step, up to its first problem.
  await kid.goto(base + '/');
  await kid.locator('.taskbtn').first().click();
  await kid.waitForURL(/\/tasks\//);
  for (let i = 0; i < 6 && !(await kid.locator('form[action$="/answer"]').count()); i++) {
    await kid.locator('main form[action$="/next"] button').first().click();
    await kid.waitForLoadState();
  }
  await shot(kid, 'lesson-problem');
  await shot(kid, 'drill-builder', '/drills');
  await shot(await student('Ben', '1357', 'dark'), 'student-home-dark', '/');

  // Pictures from higher grades, from the pack preview.
  const pics = await (await browser.newContext(phone)).newPage();
  for (const [name, page] of [
    ['figure-ten-frame', 'make-10/kp1-explain.html'],
    ['figure-coins', 'dollars-and-cents/kp1-explain.html'],
    ['figure-number-line', 'same-size-fractions/kp1-explain.html'],
    ['figure-area-model', 'mult-area-model/kp1-explain.html'],
    ['figure-protractor', 'measure-angles/kp1-explain.html'],
  ]) {
    await shot(pics, name, pathToFileURL(`${preview}/${page}`).href);
  }
} finally {
  await browser.close();
}
