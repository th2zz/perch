#!/usr/bin/env bash
set -euo pipefail

if ! command -v agent-browser >/dev/null 2>&1; then
    echo "agent-browser is unavailable; terminal viewport check needs Chrome"
    exit 2
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BROWSER_SESSION="perch-viewport-$$"
trap 'agent-browser --session "$BROWSER_SESSION" close >/dev/null 2>&1 || true' EXIT

agent-browser --session "$BROWSER_SESSION" --engine chrome open \
    "file://$REPO_ROOT/app/app/src/main/assets/terminal/index.html" >/dev/null
agent-browser --session "$BROWSER_SESSION" eval --stdin <<'JS'
(() => {
  PerchTerminal.setGlass(394, 647);
  PerchTerminal.showHostScreen(154, 49);
  const glass = document.getElementById("screen");
  const surface = document.querySelector(".xterm");
  const target = document.querySelector(".xterm-screen");
  function touch(type, points) {
    const touches = points.map(([clientX, clientY], identifier) =>
      new Touch({identifier, target, clientX, clientY}));
    target.dispatchEvent(new TouchEvent(type, {
      touches, targetTouches: touches, changedTouches: touches,
      bubbles: true, cancelable: true
    }));
  }
  glass.scrollLeft = 70;
  touch("touchstart", [[130, 200], [230, 200]]);
  touch("touchmove", [[90, 200], [270, 200]]);
  touch("touchend", []);
  touch("touchstart", [[100, 220]]);
  touch("touchmove", [[390, 220]]);
  touch("touchend", []);
  const left = surface.getBoundingClientRect().left;
  const shape = JSON.parse(PerchTerminal.size());
  if (glass.scrollLeft !== 0 || left < 0) {
    throw new Error("left edge remains clipped: x=" + left + " scrollLeft=" + glass.scrollLeft);
  }
  if (shape.cols !== 154 || shape.rows !== 49) {
    throw new Error("viewing changed the host grid: " + JSON.stringify(shape));
  }
  return {left, scrollLeft: glass.scrollLeft, shape};
})();
JS

agent-browser --session "$BROWSER_SESSION" set viewport 394 647 >/dev/null
agent-browser --session "$BROWSER_SESSION" eval --stdin <<'JS'
(async () => {
  const glass = document.getElementById("screen");
  const target = document.querySelector(".xterm-screen");
  const view = document.getElementById("terminal-viewport") || glass;
  function touch(type, points) {
    const touches = points.map(([clientX, clientY], identifier) =>
      new Touch({identifier, target, clientX, clientY}));
    target.dispatchEvent(new TouchEvent(type, {
      touches, targetTouches: touches, changedTouches: touches,
      bubbles: true, cancelable: true
    }));
  }
  function resetZoom() {
    touch("touchstart", [[80, 180]]); touch("touchend", []);
    touch("touchstart", [[80, 180]]); touch("touchend", []);
  }
  async function write(text) {
    PerchTerminal.write(btoa(text));
    await new Promise(resolve => setTimeout(resolve, 80));
  }
  function bottomVisible(label) {
    const row = document.querySelector(".xterm-rows").lastElementChild.getBoundingClientRect();
    const box = glass.getBoundingClientRect();
    if (row.top < box.top || row.bottom > box.bottom + 1) {
      throw new Error(label + ": input row unreachable/outside glass " + JSON.stringify({row: row.toJSON(), box: box.toJSON(), scrollTop: view.scrollTop}));
    }
    view.scrollLeft = 70;
    if (glass.scrollLeft !== 0 || view.scrollLeft !== 0) {
      throw new Error(label + ": horizontal scroll offset was introduced");
    }
  }
  let cases = 0;
  for (const alternate of [false, true]) {
    for (const height of [647, 363]) {
      for (const zoomed of [false, true]) {
        for (const entry of ["first", "reentry"]) {
          PerchTerminal.reset(); resetZoom();
          PerchTerminal.setGlass(394, height);
          PerchTerminal.showHostScreen(154, 49);
          const mode = alternate ? "\x1b[?1049h" : "";
          await write(mode + "\x1b[2J\x1b[HHEADER\x1b[49;1HINPUT_BOTTOM > ");
          const label = JSON.stringify({alternate, height, zoomed, entry});
          bottomVisible(label + " initial");
          if (zoomed) {
            touch("touchstart", [[130, 200], [230, 200]]);
            touch("touchmove", [[80, 200], [280, 200]]);
            touch("touchend", []);
            await new Promise(resolve => setTimeout(resolve, 30));
            bottomVisible(label + " zoom");
          }
          view.scrollTop = 0;
          const top = document.querySelector(".xterm-rows").firstElementChild.getBoundingClientRect().top;
          if (top < 0 || top >= height) throw new Error(label + " cannot reach top");
          await write("\x1b[5;1HUPDATE\x1b[49;16H");
          if (view.scrollTop !== 0) throw new Error(label + " output interrupted reading");
          view.scrollTop = view.scrollHeight;
          bottomVisible(label + " scrolled");
          view.scrollTop = 0;
          PerchTerminal.focus();
          bottomVisible(label + " focus");
          PerchTerminal.setGlass(394, height === 647 ? 363 : 647);
          bottomVisible(label + " keyboard transition");
          PerchTerminal.setGlass(394, 647);
          view.scrollTop = 0;
          PerchTerminal.setGlass(394, 363);
          bottomVisible(label + " keyboard after reading top");
          const shape = JSON.parse(PerchTerminal.size());
          if (shape.cols !== 154 || shape.rows !== 49) throw new Error("host grid changed");
          cases++;
        }
      }
    }
  }
  PerchTerminal.reset(); resetZoom(); PerchTerminal.setGlass(394, 363);
  await write("\x1b[2J\x1b[HSHELL_TOP > ");
  if (view.scrollTop !== 0) throw new Error("top-row shell prompt hidden");
  PerchTerminal.reset();
  await write("\x1b[2J\x1b[HHEADER\x1b[49;1HINPUT_BOTTOM > ");
  bottomVisible("before real wheel");
  return {cases, shape: JSON.parse(PerchTerminal.size()), bottom: view.scrollTop};
})();
JS

agent-browser --session "$BROWSER_SESSION" mouse move 150 180 >/dev/null
agent-browser --session "$BROWSER_SESSION" mouse wheel -10000 >/dev/null
agent-browser --session "$BROWSER_SESSION" eval '(async () => { await new Promise(resolve => setTimeout(resolve, 120)); const view = document.getElementById("terminal-viewport"); if (view.scrollTop !== 0) throw new Error("native wheel cannot reach top"); return {top: view.scrollTop}; })()'
agent-browser --session "$BROWSER_SESSION" mouse wheel 10000 >/dev/null
agent-browser --session "$BROWSER_SESSION" eval '(async () => { await new Promise(resolve => setTimeout(resolve, 120)); const row = document.querySelector(".xterm-rows").lastElementChild.getBoundingClientRect(); if (row.bottom > document.getElementById("screen").clientHeight + 1) throw new Error("native wheel cannot reach input"); return {inputBottom: row.bottom}; })()'

agent-browser --session "$BROWSER_SESSION" eval --stdin <<'JS'
(() => {
  const sizes = [];
  window.PerchAndroid = {onSize: (cols, rows) => sizes.push({cols, rows})};
  PerchTerminal.ownSize();
  for (const height of [647, 363, 647]) {
    PerchTerminal.setGlass(394, height);
    const screen = document.querySelector(".xterm-screen").getBoundingClientRect();
    if (screen.bottom > height + 1 || screen.right > 395) {
      throw new Error("owned shell no longer fits its glass: " + JSON.stringify(screen.toJSON()));
    }
  }
  if (sizes.length < 3 || sizes[0].rows === sizes[1].rows) {
    throw new Error("owned shell stopped reporting its resize");
  }
  return {ownedShellSizes: sizes};
})();
JS
