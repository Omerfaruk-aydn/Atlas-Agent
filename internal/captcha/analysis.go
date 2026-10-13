package captcha

// GapDetectScript finds the puzzle gap in a GeeTest slider.
// Key insight: the piece's own shadow at x=0-N must be EXCLUDED.
// The gap is a dark region to the RIGHT of the piece.
const GapDetectScript = `(() => {
const out = {found: false, method: "", reason: "", gapX: -1, gapRatio: 0, gapCSSX: -1, dragDistance: -1, sliderCSSX: -1, canvasWidth: 0, canvasHeight: 0, profile: [], pieceRange: null};

const bgEl = document.querySelector('.geetest_canvas_bg, .geetest_canvas_bg canvas, [class*="canvas_bg"]');
const sliceEl = document.querySelector('.geetest_canvas_slice, .geetest_canvas_slice canvas, [class*="canvas_slice"]');
const sliderEl = document.querySelector('.geetest_slider_button, [class*="geetest_slider_button"]');

const readPixels = (el) => {
  if (!el) return null;
  let c = el;
  if (c.tagName !== 'CANVAS') c = c.querySelector('canvas');
  if (!c || c.tagName !== 'CANVAS') return null;
  try {
    return {d: c.getContext('2d').getImageData(0, 0, c.width, c.height).data, w: c.width, h: c.height, el: c};
  } catch (e) {
    try {
      const t = document.createElement('canvas');
      t.width = c.width; t.height = c.height;
      t.getContext('2d').drawImage(c, 0, 0);
      return {d: t.getContext('2d').getImageData(0, 0, t.width, t.height).data, w: t.width, h: t.height, el: c};
    } catch (e2) { return null; }
  }
};

const bg = readPixels(bgEl);
if (!bg) { out.reason = "no readable canvas"; return out; }

const {d: px, w: W, h: H} = bg;
out.canvasWidth = W;
out.canvasHeight = H;

// ── Step 1: Find piece position from slice canvas ──────────────────
let pieceMinX = -1, pieceMaxX = -1;
if (sliceEl) {
  const sl = readPixels(sliceEl);
  if (sl) {
    for (let x = 0; x < sl.w; x++) {
      let hasPiece = false;
      for (let y = 0; y < sl.h; y += 3) {
        if (sl.d[(y * sl.w + x) * 4 + 3] > 50) { hasPiece = true; break; }
      }
      if (hasPiece) {
        if (pieceMinX < 0) pieceMinX = x;
        pieceMaxX = x;
      }
    }
  }
}
const pieceW = pieceMaxX >= 0 ? (pieceMaxX - pieceMinX + 1) : Math.floor(W * 0.25);
out.pieceRange = pieceMinX >= 0 ? {min: pieceMinX, max: pieceMaxX, width: pieceW} : null;

// ── Step 2: Luminance profile ──────────────────────────────────────
const y0 = Math.floor(H * 0.25), y1 = Math.floor(H * 0.75);
const yStep = Math.max(1, Math.floor((y1 - y0) / 30));
const profile = [];
for (let x = 0; x < W; x++) {
  let sum = 0, n = 0;
  for (let y = y0; y < y1; y += yStep) {
    const i = (y * W + x) * 4;
    sum += px[i] * 0.299 + px[i+1] * 0.587 + px[i+2] * 0.114;
    n++;
  }
  profile.push(Math.round(n > 0 ? sum / n : 255));
}
out.profile = profile;

// ── Step 3: Find gap — dark region RIGHT of piece, width ≈ pieceW ──
// CRITICAL: Skip the piece's own area (pieceMinX to pieceMaxX + margin)
// The gap shadow is separate from the piece shadow.
const searchStart = pieceMaxX >= 0 ? pieceMaxX + 20 : Math.floor(W * 0.40);
const searchEnd = W - pieceW - 5;

// Score each window of pieceW columns: lower mean = darker = more likely gap
let bestX = -1, bestScore = 999;
for (let x = searchStart; x <= searchEnd; x++) {
  let sum = 0;
  for (let dx = 0; dx < pieceW; dx++) {
    sum += profile[x + dx] || 255;
  }
  const avg = sum / pieceW;
  if (avg < bestScore) {
    bestScore = avg;
    bestX = x; // left edge of gap
  }
}

// Compute global mean for threshold
let globalSum = 0;
for (let i = 0; i < W; i++) globalSum += profile[i];
const globalMean = globalSum / W;

// Only accept if the gap is meaningfully darker than average
if (bestX > 0 && bestScore < globalMean - 5) {
  out.found = true;
  out.gapX = bestX + Math.floor(pieceW / 2); // center of gap
  out.gapRatio = out.gapX / W;
  out.method = "dark-window-right-of-piece";

  // Convert to CSS coordinates
  const bgRect = (bg.el || bgEl).getBoundingClientRect();
  const scaleX = bgRect.width / W;
  out.gapCSSX = bgRect.left + out.gapX * scaleX + window.scrollX;

  if (sliderEl) {
    const sr = sliderEl.getBoundingClientRect();
    out.sliderCSSX = sr.left + sr.width / 2 + window.scrollX;
    // Drag distance: from slider start to gap center, scaled to track
    // The slider track maps to the canvas width
    out.dragDistance = (out.gapX - (pieceMinX >= 0 ? pieceMinX : 0)) * scaleX;
  } else {
    out.dragDistance = (out.gapX - (pieceMinX >= 0 ? pieceMinX : 0)) * (bg.el || bgEl).getBoundingClientRect().width / W;
  }

  // Clamp
  const maxDrag = (bg.el || bgEl).getBoundingClientRect().width * 0.95;
  if (out.dragDistance > maxDrag) out.dragDistance = maxDrag;
  if (out.dragDistance < 5) out.dragDistance = pieceW * scaleX;
} else {
  out.reason = "no dark gap right of piece (best=" + bestScore.toFixed(1) + " mean=" + globalMean.toFixed(1) + ")";
  // Fallback: report best guess anyway
  if (bestX > 0) {
    out.found = true;
    out.gapX = bestX + Math.floor(pieceW / 2);
    out.gapRatio = out.gapX / W;
    out.method = "weak-dark-window";
    const bgRect = (bg.el || bgEl).getBoundingClientRect();
    const scaleX = bgRect.width / W;
    out.dragDistance = (out.gapX - (pieceMinX >= 0 ? pieceMinX : 0)) * scaleX;
  }
}

return out;
})()`

// RotateAngleScript reads the exact rotation angle from CSS transform.
const RotateAngleScript = `(() => {
const out = {found: false, estimatedAngle: 0, declaredAngle: null, confidence: 0, reason: "", clickDegrees: 15};

const candidates = [
  document.querySelector('[class*="rotate-captcha"] img, [class*="rotate-captcha"] canvas'),
  document.querySelector('[class*="image-rotate"] img, [class*="image-rotate"] canvas'),
  ...document.querySelectorAll('img, canvas')
].filter(Boolean);

let imgEl = null;
for (const el of candidates) {
  const r = el.getBoundingClientRect();
  if (r.width > 80 && r.height > 80 && r.width < 500 && Math.abs(r.width - r.height) < r.width * 0.4) {
    imgEl = el;
    break;
  }
}
if (!imgEl) return {...out, found: false, reason: "no rotate image found"};

const readMatrix = (el) => {
  const style = getComputedStyle(el);
  const t = el.style.transform || style.transform || style.webkitTransform || '';
  const m = t.match(/matrix\(([^)]+)\)/);
  if (m) {
    const v = m[1].split(',').map(s => parseFloat(s.trim()));
    if (v.length >= 4) return Math.atan2(v[1], v[0]) * 180 / Math.PI;
  }
  const m2 = t.match(/rotate\(([-\d.]+)deg\)/);
  if (m2) return parseFloat(m2[1]);
  if (el.parentElement && el.parentElement !== document.body) return readMatrix(el.parentElement);
  return null;
};

const angle = readMatrix(imgEl);
if (angle !== null) {
  out.found = true;
  out.estimatedAngle = Math.round(angle);
  out.declaredAngle = Math.round(angle * 10) / 10;
  out.confidence = 1.0;
  out.reason = "css-transform";
  return out;
}

for (const attr of ['data-angle', 'data-rotation', 'data-degrees']) {
  const val = imgEl.getAttribute(attr);
  if (val) {
    const n = parseFloat(val);
    if (!isNaN(n)) {
      out.found = true;
      out.estimatedAngle = Math.round(n);
      out.declaredAngle = n;
      out.confidence = 0.95;
      out.reason = "data-attribute";
      return out;
    }
  }
}

return {...out, found: false, reason: "no transform found"};
})()`
