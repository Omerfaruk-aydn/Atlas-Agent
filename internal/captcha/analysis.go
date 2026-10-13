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
