// Compile and execute hidden TypeScript checks outside the task workspace.
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
let ts;
try { ts = require('typescript'); }
catch {
  try { ts = require('../web/node_modules/typescript'); }
  catch { throw new Error('Install the pinned checker with npm --prefix bench install --ignore-scripts'); }
}
const files = fs.readdirSync(process.cwd()).filter(name => name.endsWith('.ts'));
const tests = files.filter(name => /^atlas_.*\.test\.ts$/.test(name));
if (!tests.length) throw new Error('No hidden TypeScript checks found');
const output = fs.mkdtempSync(path.join(os.tmpdir(), 'atlas-ts-check-'));
try {
  const program = ts.createProgram(files, {
    target: ts.ScriptTarget.ES2022,
    module: ts.ModuleKind.CommonJS,
    strict: true,
    skipLibCheck: true,
    noEmitOnError: true,
    outDir: output,
    types: [],
  });
  const result = program.emit();
  const diagnostics = ts.getPreEmitDiagnostics(program).concat(result.diagnostics);
  if (diagnostics.length) {
    process.stderr.write(ts.formatDiagnosticsWithColorAndContext(diagnostics, {
      getCurrentDirectory: () => process.cwd(),
      getCanonicalFileName: name => name,
      getNewLine: () => '\n',
    }));
    process.exitCode = 1;
  } else {
    for (const test of tests) require(path.join(output, test.replace(/\.ts$/, '.js')));
    process.stdout.write(`Passed ${tests.length} hidden TypeScript check files\n`);
  }
} finally {
  fs.rmSync(output, { recursive: true, force: true });
}
