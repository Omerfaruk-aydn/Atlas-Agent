// 04 · Subagents keep your context small.
export default async ({ start, ask, pause, startRec, settle }) => {
  await start(["--yolo", "-H", "npipe:////./pipe/atlas-film-1"]);
  await pause(4500);
  await startRec();
  await pause(1200);
  await ask("Use a subagent to find out how configuration is parsed in this project and report back in two sentences.");
  await pause(3000);
};
