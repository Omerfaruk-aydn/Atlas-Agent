"use strict";

// Runs in every page of the panel browser, in its own isolated world, in the
// top frame only. The page cannot see it, call it or read what it holds. It
// does three things and nothing more:
//   1. tells the app whether the page shows a login form (so the key button
//      can offer to fill it),
//   2. reports a login when the person submits one (a trusted submit event),
//   3. fills a login when the app, after the person asked, sends one.
// It never fills by itself, never into fields the person cannot see, and the
// secret it is handed comes from the main process only for the page's own
// exact origin.

const { ipcRenderer } = require("electron");

function visible(el) {
  if (!(el instanceof HTMLElement) || el.disabled || el.readOnly) return false;
  const box = el.getBoundingClientRect();
  if (box.width < 4 || box.height < 4) return false;
  const style = getComputedStyle(el);
  return style.visibility !== "hidden" && style.display !== "none" && Number(style.opacity) > 0.05;
}

function passwordFields(root = document) {
  return [...root.querySelectorAll('input[type="password"]')].filter(visible);
}

/** The user name field of a password field: the text-like input before it in the same form. */
function userField(password) {
  const scope = password.form ?? document;
  const inputs = [...scope.querySelectorAll("input")].filter((i) => ["text", "email", "tel", ""].includes((i.getAttribute("type") ?? "").toLowerCase()) && visible(i));
  let found = null;
  for (const input of inputs) {
    if (input.compareDocumentPosition(password) & Node.DOCUMENT_POSITION_FOLLOWING) found = input;
  }
  return found;
}

let lastLogin = null;
function announce() {
  const login = passwordFields().length > 0;
  if (login !== lastLogin) {
    lastLogin = login;
    ipcRenderer.send("atlas:vault-form", { login });
  }
}

let timer = 0;
function later() {
  clearTimeout(timer);
  timer = setTimeout(announce, 400);
}

function setValue(input, value) {
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
  setter ? setter.call(input, value) : (input.value = value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
  input.dispatchEvent(new Event("change", { bubbles: true }));
}

// A script can call form.requestSubmit(), and the browser then fires a trusted
// submit event. A login is only reported when a real press, click or Enter of
// the person came just before it, seen here in the isolated world.
let lastGesture = -Infinity;
for (const type of ["pointerdown", "mousedown", "keydown", "touchstart"]) {
  window.addEventListener(
    type,
    (event) => {
      if (event.isTrusted) lastGesture = performance.now();
    },
    true,
  );
}
const GESTURE_WINDOW_MS = 2000;

document.addEventListener(
  "submit",
  (event) => {
    if (!event.isTrusted || !(event.target instanceof HTMLFormElement)) return;
    if (performance.now() - lastGesture > GESTURE_WINDOW_MS || navigator.userActivation?.isActive === false) return;
    const password = passwordFields(event.target)[0];
    if (!password || !password.value) return;
    const user = userField(password);
    ipcRenderer.send("atlas:vault-submit", { username: user?.value ?? "", password: password.value });
  },
  true,
);

ipcRenderer.on("atlas:vault-fill", (_event, login) => {
  if (typeof login?.password !== "string") return;
  const password = passwordFields()[0];
  if (!password) {
    ipcRenderer.send("atlas:vault-filled", { ok: false });
    return;
  }
  const user = userField(password);
  if (user && typeof login.username === "string") setValue(user, login.username);
  setValue(password, login.password);
  ipcRenderer.send("atlas:vault-filled", { ok: true });
});

window.addEventListener("DOMContentLoaded", () => {
  announce();
  new MutationObserver(later).observe(document.documentElement, { childList: true, subtree: true, attributes: true, attributeFilter: ["style", "class", "hidden", "type"] });
});
window.addEventListener("load", announce);

// Addresses. Same rules as logins: this only says whether the page asks for an
// address (so the app can offer its fill button), and fills one when the app,
// after the person pressed the button, sends it. Never by itself, never into
// a field the person cannot see.
const ADDRESS_TOKENS = {
  name: ["name", "given-name", "family-name"],
  organization: ["organization"],
  street: ["address-line1", "street-address"],
  street2: ["address-line2"],
  city: ["address-level2"],
  region: ["address-level1"],
  postal: ["postal-code"],
  country: ["country", "country-name"],
  phone: ["tel", "tel-national"],
  email: ["email"],
};
const ADDRESS_NAME_HINTS = {
  name: /^(full[-_ ]?name|name)$/i,
  organization: /^(company|organi[sz]ation)$/i,
  street: /(address|street)(?!.*2)/i,
  street2: /(address|street).*2|apt|suite/i,
  city: /^(city|town)$/i,
  region: /^(state|region|province)$/i,
  postal: /(zip|post(al)?[-_ ]?code)/i,
  country: /^country/i,
  phone: /^(phone|tel|mobile)/i,
  email: /^e-?mail$/i,
};

function addressKind(input) {
  const tokens = (input.getAttribute("autocomplete") ?? "").toLowerCase().split(/\s+/);
  for (const [kind, names] of Object.entries(ADDRESS_TOKENS)) if (names.some((n) => tokens.includes(n))) return kind;
  if (tokens.includes("off") || tokens.includes("new-password")) return "";
  const label = `${input.name ?? ""} ${input.id ?? ""}`.trim();
  for (const [kind, re] of Object.entries(ADDRESS_NAME_HINTS)) if (label.split(/\s+/).some((part) => re.test(part))) return kind;
  return "";
}

function addressFields() {
  const found = [];
  for (const input of document.querySelectorAll("input, select")) {
    if (input instanceof HTMLInputElement && !["text", "email", "tel", "search", ""].includes((input.getAttribute("type") ?? "").toLowerCase())) continue;
    if (!visible(input)) continue;
    const kind = addressKind(input);
    if (kind) found.push({ input, kind });
  }
  return found;
}

let lastAddress = null;
function announceAddress() {
  // A form that asks for at least two kinds of field (a lone "email" box is a newsletter, not an address).
  const kinds = new Set(addressFields().map((f) => f.kind));
  kinds.delete("email");
  const address = kinds.size >= 2;
  if (address !== lastAddress) {
    lastAddress = address;
    ipcRenderer.send("atlas:addr-form", { address });
  }
}

ipcRenderer.on("atlas:addr-fill", (_event, record) => {
  if (!record || typeof record !== "object") return;
  for (const { input, kind } of addressFields()) {
    let value = "";
    if (kind === "name") {
      const token = (input.getAttribute("autocomplete") ?? "").toLowerCase();
      const parts = String(record.name ?? "").trim().split(/\s+/);
      value = token.includes("given-name") ? (parts[0] ?? "") : token.includes("family-name") ? parts.slice(1).join(" ") : String(record.name ?? "");
    } else value = String(record[kind] ?? "");
    if (!value) continue;
    if (input instanceof HTMLSelectElement) {
      const option = [...input.options].find((o) => o.value.toLowerCase() === value.toLowerCase() || o.text.toLowerCase() === value.toLowerCase());
      if (option) {
        input.value = option.value;
        input.dispatchEvent(new Event("input", { bubbles: true }));
        input.dispatchEvent(new Event("change", { bubbles: true }));
      }
    } else setValue(input, value);
  }
  ipcRenderer.send("atlas:addr-filled", { ok: true });
});

window.addEventListener("DOMContentLoaded", () => {
  announceAddress();
  new MutationObserver(() => setTimeout(announceAddress, 400)).observe(document.documentElement, { childList: true, subtree: true, attributes: true, attributeFilter: ["style", "class", "hidden", "type"] });
});
window.addEventListener("load", announceAddress);
