// 21 · Headless in CI: one JSON object with result, tokens and cost.
export default async ({ start, type, enter, settle, pause, startRec }) => {
  await start(["-NoLogo", "-NoProfile", "-NoExit", "-Command", "$env:Path='D:\Atlas;'+$env:Path; function prompt {'PS shop> '}; Clear-Host"], "powershell.exe");
  await pause(3000);
  await startRec();
  await pause(800);
  await type("atlas-agent run --output-format json --yolo -m claude/claude-haiku-5-5 'Name the exported function in internal/config/parse.go and say what it returns.' | ConvertFrom-Json | Format-List");
  await pause(500);
  await enter();
  await settle("PS shop> $", { quiet: 5000, timeout: 240000 });
  await pause(3500);
};
