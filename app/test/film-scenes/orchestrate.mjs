// 01 · Orchestrator: several workers, one verified result.
export default async ({ start, ask, pause, startRec }) => {
  await start(["--yolo", "-H", "npipe:////./pipe/atlas-film-orch"]);
  await pause(4500);
  await startRec();
  await pause(1200);
  await ask("Add a Validate method to Config that rejects negative retries and timeouts with tests, and write a README section for the config package. Split it across workers.", { timeout: 1200000 });
  await pause(3000);
};
