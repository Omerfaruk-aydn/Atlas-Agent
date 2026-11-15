// Runs after boot.js and before the editor's modules read the language. The
// design studio speaks English by default, so the agent driving it sees the
// same labels every time; ?lang=tr opens it in Turkish.
(() => {
  const want = new URLSearchParams(location.search).get("lang") === "tr" ? "tr" : "en";
  const boot = window.__atlasBoot ?? { theme: "system", dark: document.documentElement.dataset.colorScheme === "dark", motion: "system", ui: {} };
  window.__atlasBoot = { ...boot, ui: { ...(boot.ui ?? {}), lang: want } };
  document.documentElement.lang = want;
})();
