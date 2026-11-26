(function (root, factory) {
  if (typeof module === "object" && module.exports) {
    module.exports = factory();
  } else {
    root.PerchFit = factory();
  }
})(typeof self !== "undefined" ? self : this, function () {
  /**
   * The font size the page draws at when the whole screen fits, and the floor it will not go below.
   *
   */
  var BASE_FONT = 13;
  var MIN_FONT = 13;

  var MIN_BOX = 40;

  /** The page's own padding (index.html: `#screen { padding: 6px 4px 0 6px }`), in CSS pixels. */
  var PADDING = 8;

  var MAX_ZOOM = 4;
  var MAX_FONT = 32;

  /** Keeps a zoom factor inside the range above, and refuses to be told `NaN` by a gesture. */
  function clampZoom(zoom) {
    if (!isFinite(zoom) || zoom < 1) {
      return 1;
    }
    return Math.min(zoom, MAX_ZOOM);
  }

  /**
   * The zoom a pinch gesture is asking for.
   *
   * Measured from where the gesture *started*, not from frame to frame: computing each frame against
   * the previous one accumulates rounding, and a pinch that goes out and comes back would not land
   * where it started. `startDistance` is the gap between the two fingers when they touched down.
   */
  function zoomFromPinch(startDistance, distance, startZoom) {
    var base = clampZoom(startZoom);
    if (!(startDistance > 0) || !isFinite(distance) || distance <= 0) {
      return base;
    }
    return clampZoom(base * (distance / startDistance));
  }

  /**
   * The font size a zoomed screen should draw at.
   *
   * Zoom multiplies the *fitted* size rather than the base font: the fit is what makes the whole width
   * visible, so "1×" means "all of it, small" and 2× means "half of it, twice as big". The result is
   * bounded by [MIN_FONT] and [MAX_FONT] and rounded to a half point, exactly like [fitFontSize] — and
   * never NaN, for the same reason: a font size that is not a number draws nothing at all.
   */
  function zoomFontSize(fitted, zoom) {
    var size = fitted * clampZoom(zoom);
    if (!isFinite(size)) {
      return fitted;
    }
    if (size < MIN_FONT) {
      size = MIN_FONT;
    }
    if (size > MAX_FONT) {
      size = MAX_FONT;
    }
    return Math.round(size * 2) / 2;
  }

  /**
   * Where to pan so that whatever is under the reader's fingers stays under them while zooming.
   *
   * Zooming without this anchors at the left edge of the glass: pinch in on the right-hand half of a
   * line and the thing you were looking at slides out of view, which reads as the page fighting you.
   * `anchorX` is the point that should not move (the middle of the glass for a pinch, which is where
   * both thumbs meet), and `ratio` is how much larger the drawing is about to become.
   *
   * The arithmetic is one line; it is here with the other arithmetic because it is the difference
   * between a gesture that feels like a magnifier and one that feels broken, and because a page cannot
   * be tested on this machine any more than the rest of it can.
   */
  function panAfterZoom(panOffset, anchorX, ratio) {
    // A pan that is not a number would put the terminal off the glass with nothing drawn, so it is
    // treated as "the left edge" rather than propagated.
    if (!isFinite(panOffset)) {
      return 0;
    }
    if (!isFinite(anchorX) || !isFinite(ratio) || ratio <= 0) {
      return panOffset;
    }
    return (panOffset + anchorX) * ratio - anchorX;
  }

  /** Whether there is enough glass to draw a terminal into at all. */
  function hasRoom(boxWidth, boxHeight) {
    return isFinite(boxWidth) && isFinite(boxHeight) && boxWidth > MIN_BOX && boxHeight > MIN_BOX;
  }

  /**
   * How many columns and rows this glass holds, for a terminal the phone *owns*.
   *
   * The pane viewer's question is "how small must the type be for somebody else's 183-column screen to
   * fit", and this is the opposite question — a shell on the computer has no shape of its own until the
   * phone gives it one, so the phone measures its own glass and tells the host. That is what makes a
   * terminal feel like a terminal on a phone rather than a photograph of one: readable type, the width
   * exact, and a real resize (and SIGWINCH) whenever the phone turns or the reader pinches.
   *
   * The floors are not cosmetic. A terminal of twelve columns is not a terminal, and the bridge refuses
   * shapes outside 20x5 (the pane stream's lesson: a viewport reported before layout squashed somebody's
   * running codex to two rows). Returning a floor rather than a cropped grid keeps "what we asked for"
   * and "what the shell believes" the same number.
   */
  function fitGrid(input) {
    var cols = Math.floor((input.boxWidth - PADDING) / input.charWidth);
    var rows = Math.floor((input.boxHeight - PADDING) / input.lineHeight);
    if (!isFinite(cols) || !isFinite(rows) || cols <= 0 || rows <= 0) {
      return { cols: 80, rows: 24, reason: "unmeasured" };
    }
    return {
      cols: Math.min(Math.max(cols, 20), 500),
      rows: Math.min(Math.max(rows, 5), 200),
      reason: "fitted"
    };
  }

  function fitFontSize(input) {
    var boxWidth = input.boxWidth;
    var boxHeight = input.boxHeight;
    var charWidth = input.charWidth;
    var lineHeight = input.lineHeight;
    var cols = input.cols;
    var rows = input.rows;

    if (!hasRoom(boxWidth, boxHeight)) {
      return { size: BASE_FONT, reason: "no room" };
    }
    if (!(charWidth > 0) || !(lineHeight > 0) || !(cols > 0) || !(rows > 0)) {
      return { size: BASE_FONT, reason: "unmeasured" };
    }

    var wanted = Math.min(
      (boxWidth - PADDING) / (charWidth * cols),
      (boxHeight - PADDING) / (lineHeight * rows)
    );
    var size = BASE_FONT * wanted;
    if (!isFinite(size)) {
      return { size: BASE_FONT, reason: "unmeasured" };
    }
    if (size > BASE_FONT) {
      size = BASE_FONT;
    }
    if (size < MIN_FONT) {
      size = MIN_FONT;
    }
    // Half points: sub-pixel type renders differently on every device, and rounding is what makes
    // the same shape come out the same twice.
    return { size: Math.round(size * 2) / 2, reason: "fitted" };
  }

  return {
    BASE_FONT: BASE_FONT,
    MIN_FONT: MIN_FONT,
    MIN_BOX: MIN_BOX,
    PADDING: PADDING,
    MAX_ZOOM: MAX_ZOOM,
    MAX_FONT: MAX_FONT,
    hasRoom: hasRoom,
    fitFontSize: fitFontSize,
    clampZoom: clampZoom,
    zoomFromPinch: zoomFromPinch,
    zoomFontSize: zoomFontSize,
    panAfterZoom: panAfterZoom,
    fitGrid: fitGrid
  };
});
