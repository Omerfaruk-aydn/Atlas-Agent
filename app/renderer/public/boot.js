// Runs before first paint. A separate file, because the gateway's CSP allows
// no inline scripts. Sets the theme and motion attributes everything keys off.
(() => {
  const boot = window.atlas?.boot?.() ?? null;
  const root = document.documentElement;
  const dark = boot ? boot.dark : matchMedia("(prefers-color-scheme: dark)").matches;
  root.dataset.colorScheme = dark ? "dark" : "light";
  const osReduce = matchMedia("(prefers-reduced-motion: reduce)").matches;
  const motion = boot?.motion ?? "system";
  root.dataset.motion = motion === "reduce" || (motion === "system" && osReduce) ? "reduce" : "full";
  window.__atlasBoot = boot;
})();
