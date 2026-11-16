// Smoke test against a running Zigrt (see run-image.sh): first-run setup,
// adding a student, the PIN pad, a wrong PIN, and the student's home.
//   BASE_URL=http://localhost:18080 SETUP_TOKEN=... node smoke.mjs
import { chromium } from 'playwright';

const base = process.env.BASE_URL ?? 'http://localhost:18080';
const token = process.env.SETUP_TOKEN;
if (!token) throw new Error('SETUP_TOKEN is not set');

const browser = await chromium.launch();
const page = await (await browser.newContext({ viewport: { width: 390, height: 844 } })).newPage();
const problems = [];
// The CSP blocks inline styles and scripts; a violation shows up here.
page.on('console', (m) => { if (m.type() === 'error' && !/status of 4\d\d/.test(m.text())) problems.push(m.text()); });
page.on('pageerror', (e) => problems.push(String(e)));
const expectText = async (text) => page.getByText(text).first().waitFor({ timeout: 5000 });
const submit = () => page.locator('main button[type=submit]').click();

await page.goto(base + '/');
await page.waitForURL('**/setup');
await page.fill('#setup_token', 'not-the-token');
await page.fill('#display_name', 'Sam');
await page.fill('#email', 'sam@example.com');
await page.fill('#password', 'correct horse battery');
await submit();
await expectText("That setup token doesn't match");

await page.fill('#setup_token', token);
await page.fill('#password', 'correct horse battery');
await submit();
await page.waitForURL('**/parent/students/new');
await page.fill('#display_name', 'Mika');
await page.fill('#pin', '2468');
await submit();
await page.waitForURL('**/parent');
await expectText('Mika is added');
await page.getByRole('button', { name: 'Sign out' }).click();

await expectText("Who's climbing today?");
await page.getByRole('link', { name: 'Mika' }).click();
for (const k of ['1', '1', '1', '1']) await page.click(`[data-key="${k}"]`);
await page.getByRole('button', { name: 'Go' }).click();
await expectText("That PIN isn't right.");
for (const k of ['2', '4', '6', '8']) await page.click(`[data-key="${k}"]`);
await page.getByRole('button', { name: 'Go' }).click();
await expectText('Hi Mika. Ready to climb?');

// The self-hosted fonts loaded, so nothing fell back to a system face.
const fonts = await page.evaluate(async () => {
  await document.fonts.ready;
  return [...document.fonts].filter((f) => f.status === 'loaded').map((f) => f.family.replaceAll('"', ''));
});
for (const f of ['Fredoka', 'Nunito']) if (!fonts.includes(f)) problems.push(`font ${f} did not load`);

await browser.close();
if (problems.length) {
  console.error(problems.join('\n'));
  process.exit(1);
}
console.log('smoke test passed');
