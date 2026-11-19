// 07 · Configure it by talking to it.
export default async ({ start, ask, pause, startRec }) => {
  await start(["--yolo", "-H", "npipe:////./pipe/atlas-film-1"]);
  await pause(4500);
  await startRec();
  await pause(1200);
  await ask("Put Claude Haiku 5.5 on the research role and tell me what changed.");
  await pause(3000);
};
