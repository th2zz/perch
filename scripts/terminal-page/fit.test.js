/*
 * Node tests for the Android WebView terminal page fit arithmetic.
 *
 * Run: scripts/check-terminal-page.sh   (or: node --test scripts/terminal-page/)
 */

const test = require("node:test");
const assert = require("node:assert");
const fit = require("../../app/app/src/main/assets/terminal/fit.js");

/**
 * Representative portrait phone metrics and wide desktop pane geometry (183x58).
 */
const PHONE = { boxWidth: 411, boxHeight: 820 };
const MEASURED = { charWidth: 7.8, lineHeight: 15.6 };
const DESKTOP_PANE = { cols: 183, rows: 58 };

function METRIC_WIDTH(size, cols) {
  // The page's own assumption, and the one its re-measure loop absorbs: type scales linearly.
  return MEASURED.charWidth * (size / fit.BASE_FONT) * cols + fit.PADDING;
}

test("the initial font and pinch floor remain readable on a wide desktop pane", () => {
  const initial = fit.fitFontSize({ ...PHONE, ...MEASURED, ...DESKTOP_PANE });
  assert.ok(initial.size >= 13);
  for (const zoom of [0, 0.01, 0.25, 1, 2, 4]) {
    assert.ok(fit.zoomFontSize(initial.size, zoom) >= 13);
  }
});

test("a wide pane keeps readable type and can extend beyond the glass", () => {
  const got = fit.fitFontSize({ ...PHONE, ...MEASURED, cols: 100, rows: 40 });

  assert.equal(got.reason, "fitted");
  assert.equal(got.size, fit.MIN_FONT);
  assert.ok(
    METRIC_WIDTH(got.size, 100) > PHONE.boxWidth,
    "wide panes must pan instead of shrinking below the readable floor"
  );
});

test("a pane wider than the phone stops at the readable floor and says so", () => {
  const got = fit.fitFontSize({ ...PHONE, ...MEASURED, ...DESKTOP_PANE });
  const width = METRIC_WIDTH(got.size, DESKTOP_PANE.cols);

  assert.equal(got.reason, "fitted");
  assert.equal(got.size, fit.MIN_FONT);
  assert.ok(
    width > PHONE.boxWidth,
    `if this ever fits (${width.toFixed(1)}px against ${PHONE.boxWidth}px), the floor can come down ` +
      "and the test should say so"
  );
});

test("it never enlarges type, and never goes below the floor", () => {
  // A narrow pane (a shell, or a pane somebody just split) must draw at the normal size rather than
  // blowing the type up to fill the glass: the screen should look the same everywhere.
  const narrow = fit.fitFontSize({ ...PHONE, ...MEASURED, cols: 40, rows: 20 });
  assert.equal(narrow.size, fit.BASE_FONT);

  // A pane too wide for any readable type lands on the floor, not on zero.
  const enormous = fit.fitFontSize({ ...PHONE, ...MEASURED, cols: 2000, rows: 200 });
  assert.equal(enormous.size, fit.MIN_FONT);
});

test("a bottomless drawer is always in half points and never NaN", () => {
  const cases = [];
  for (let cols of [1, 20, 40, 80, 183, 400, 2000]) {
    for (let boxWidth of [80, 200, 411, 1200]) {
      cases.push(fit.fitFontSize({ boxWidth, boxHeight: 800, ...MEASURED, cols, rows: 40 }));
    }
  }
  cases.push(fit.fitFontSize({ ...PHONE, ...MEASURED, cols: NaN, rows: NaN }));
  cases.push(fit.fitFontSize({ ...PHONE, charWidth: 0, lineHeight: 0, cols: 183, rows: 58 }));

  for (const got of cases) {
    assert.ok(Number.isFinite(got.size), `a font size of ${got.size} would draw nothing`);
    assert.ok(got.size >= fit.MIN_FONT, `${got.size} is below the readable floor`);
    assert.ok(got.size <= fit.BASE_FONT, `${got.size} is larger than the base font`);
    assert.equal(got.size * 2, Math.round(got.size * 2), `${got.size} is not a half point`);
  }
});

test("a box that is not there yet is not a screen to draw", () => {
  // Every one of these is a real moment: before layout, during a rotation, an invisible view.
  for (const box of [
    { boxWidth: 0, boxHeight: 0 },
    { boxWidth: 12, boxHeight: 800 },
    { boxWidth: 800, boxHeight: 12 },
    { boxWidth: NaN, boxHeight: NaN },
    { boxWidth: undefined, boxHeight: undefined }
  ]) {
    const got = fit.fitFontSize({ ...box, ...MEASURED, ...DESKTOP_PANE });
    assert.equal(got.reason, "no room", `${JSON.stringify(box)} should have been refused`);
    assert.equal(got.size, fit.BASE_FONT, "a refused box keeps the default rather than shrinking to the floor");
    assert.equal(fit.hasRoom(box.boxWidth, box.boxHeight), false);
  }
});

test("no metrics yet means wait, not guess", () => {
  const got = fit.fitFontSize({ ...PHONE, charWidth: 0, lineHeight: 0, ...DESKTOP_PANE });
  assert.equal(got.reason, "unmeasured");
  assert.equal(got.size, fit.BASE_FONT);
});

test("a bigger glass never gives smaller type", () => {
  let previous = 0;
  for (const boxWidth of [200, 300, 411, 600, 900]) {
    const got = fit.fitFontSize({ boxWidth, boxHeight: 820, ...MEASURED, ...DESKTOP_PANE });
    assert.ok(got.size >= previous, `${boxWidth}px wide gave ${got.size}px after ${previous}px`);
    previous = got.size;
  }
});

// ------------------------------------------------- a shell the phone owns (2026-10-15)

/*
 * The other terminal: a shell on the computer, whose shape the phone decides.
 *
 * These are the numbers that replace the *host's* geometry with the phone's own — the one place in this
 * product where the phone's measurements are the truth. They matter more than the pane fit does, because a
 * wrong number here is sent to the computer and becomes the size of a real terminal somebody is working in.
 */

test("a shell gets the phone's own grid, measured rather than assumed", () => {
  const grid = fit.fitGrid({ boxWidth: 411, boxHeight: 820, ...MEASURED });

  assert.equal(grid.reason, "fitted");
  assert.equal(grid.cols, Math.floor((411 - fit.PADDING) / MEASURED.charWidth));
  assert.equal(grid.rows, Math.floor((820 - fit.PADDING) / MEASURED.lineHeight));
  // A terminal somebody could use: the bridge refuses anything outside 20x5, so a floor here keeps "what
  // we asked for" and "what the shell believes" the same number.
  assert.ok(grid.cols >= 20 && grid.rows >= 5);
});

test("pinching a shell's type means fewer columns, not smaller type", () => {
  // This is what makes a terminal on a phone feel like one: the type stays readable and the shell reflows
  // for it. Doubling the type halves the columns, and the app sends that to the computer.
  const base = fit.fitGrid({ boxWidth: 411, boxHeight: 820, ...MEASURED });
  const bigger = fit.fitGrid({ boxWidth: 411, boxHeight: 820, charWidth: 15.6, lineHeight: 31.2 });

  assert.ok(bigger.cols < base.cols, `${bigger.cols} columns at a bigger font, was ${base.cols}`);
  assert.ok(bigger.rows < base.rows);
});

test("a glass with no room yet does not invent a terminal", () => {
  for (const box of [
    { boxWidth: 0, boxHeight: 0 },
    { boxWidth: NaN, boxHeight: 800 },
    { boxWidth: 411, boxHeight: undefined }
  ]) {
    const grid = fit.fitGrid({ ...box, ...MEASURED });
    assert.equal(grid.reason, "unmeasured", `${JSON.stringify(box)} should not have produced a grid`);
    // The floors the bridge would accept, so an unmeasured phone asks for something a terminal can be.
    assert.equal(grid.cols, 80);
    assert.equal(grid.rows, 24);
  }
});

test("a grid never goes below what a terminal needs, however small the glass", () => {
  // A 60x100 box is what a keyboard opening on a small phone leaves: the arithmetic would give a handful of
  // columns, and the bridge would refuse the resize outright, leaving the shell at its old size.
  const grid = fit.fitGrid({ boxWidth: 60, boxHeight: 100, ...MEASURED });
  assert.equal(grid.cols, 20);
  assert.equal(grid.rows, 5);
});
