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
