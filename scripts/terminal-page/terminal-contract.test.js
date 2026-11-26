
const test = require("node:test");
const assert = require("node:assert");
const fs = require("node:fs");
const path = require("node:path");

const PAGE = path.join(__dirname, "../../app/app/src/main/assets/terminal/index.html");
// Every Kotlin file in the terminal feature. *Every* one, not the screen that happened to be written
// first: there are two terminals now (a pane herdr owns, and a shell of the phone's own), both drawn by
// this page, and a name checked in one file while the other calls something else is exactly the failure
// this test exists for.
const APP_DIR = path.join(__dirname, "../../app/app/src/main/java/dev/perch/console/features/terminal");

const page = fs.readFileSync(PAGE, "utf8");
const app = fs
  .readdirSync(APP_DIR)
  .filter((name) => name.endsWith(".kt"))
  .map((name) => fs.readFileSync(path.join(APP_DIR, name), "utf8"))
  .join("\n");

/** Everything `PerchTerminal.<name>` the app calls. */
function namesTheAppCalls() {
  return [...new Set([...app.matchAll(/PerchTerminal\.(\w+)\(/g)].map((m) => m[1]))].sort();
}

/** Everything the page hangs off `window.PerchTerminal`. */
function namesThePageOffers() {
  const block = page.match(/window\.PerchTerminal = \{([\s\S]*?)\n\s*\};/);
  assert.ok(block, "the page no longer defines window.PerchTerminal; the app would draw nothing");
  return [...new Set([...block[1].matchAll(/^\s*(\w+):/gm)].map((m) => m[1]))].sort();
}

/** Everything the page calls on `PerchAndroid`. */
function namesThePageCalls() {
  return [...new Set([...page.matchAll(/PerchAndroid\.(\w+)\(/g)].map((m) => m[1]))].sort();
}

/** The @JavascriptInterface methods the app exposes to the page. */
function namesTheAppOffers() {
  const methods = [...app.matchAll(/@JavascriptInterface\s+fun (\w+)\(/g)].map((m) => m[1]);
  assert.ok(methods.length > 0, "TerminalPage has no @JavascriptInterface methods any more");
  return [...new Set(methods)].sort();
}

test("every PerchTerminal call the app makes exists in the page", () => {
  const called = namesTheAppCalls();
  const offered = namesThePageOffers();

  assert.ok(called.length >= 3, `the app calls PerchTerminal only ${called.length} times; check the parse`);
  const missing = called.filter((name) => !offered.includes(name));
  assert.deepEqual(
    missing,
    [],
    `the app calls PerchTerminal.${missing.join(", ")} and the page offers ${offered.join(", ")}; ` +
      "a call to a function that is not there is dropped in silence"
  );
});

test("the page can reach every callback the app offers", () => {
  const called = namesThePageCalls();
  const offered = namesTheAppOffers();

  const missing = called.filter((name) => !offered.includes(name));
  assert.deepEqual(
    missing,
    [],
    `the page calls PerchAndroid.${missing.join(", ")} and the app offers ${offered.join(", ")} ` +
      "(annotated with @JavascriptInterface)"
  );
});

test("the page still has the four things the app relies on", () => {
  // Named individually because each one is a different failure: `ready` missing means the stream
  // never opens (black screen), `write` missing means bytes are thrown away, `showHostScreen` missing
  // means the phone's own grid is used, and `reset` is what a reconnect needs.
  //
  // `ownSize` and `setGlass` are here for the same reason, added the day the second terminal arrived: the
  // shell screen calls both, and a page that lost either would draw a terminal at a made-up shape while
  // every test above still passed.
  for (const name of ["ready", "write", "showHostScreen", "ownSize", "setGlass", "reset"]) {
    assert.ok(
      namesThePageOffers().includes(name),
      `the page no longer offers PerchTerminal.${name}`
    );
  }
});
