// Evaluates Astryx theme sources with stubbed imports and prints their tokens as JSON.
const fs = require("fs");
const path = require("path");
const vm = require("vm");
const ts = require(process.argv[2]);
const dir = path.join(__dirname, "astryx");
const load = (file, mods) => {
  const src = fs.readFileSync(file, "utf8");
  const js = ts.transpileModule(src, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText;
  const module = { exports: {} };
  vm.runInNewContext(js, { module, exports: module.exports, require: (m) => mods(m) });
  return module.exports;
};
const core = { defineTheme: (x) => x, defineSyntaxTheme: (x) => x, lightDark: (a, b) => [a, b] };
const out = {};
for (const t of ["neutral", "butter", "chocolate", "gothic", "matcha", "stone", "y2k"]) {
  let refs = {};
  const refFile = path.join(dir, `${t}.refs.ts`);
  if (fs.statSync(refFile).size > 100) refs = load(refFile, () => ({}));
  const theme = load(path.join(dir, `${t}.ts`), (m) => {
    if (m.includes("core")) return core;
    if (m.includes("icons")) return {};
    if (m.includes("PaletteRefs")) return refs;
    throw new Error("unknown import " + m);
  });
  const def = Object.values(theme).find((v) => v && v.tokens);
  out[t] = { typography: def.typography, tokens: def.tokens };
}
process.stdout.write(JSON.stringify(out, null, 1));
