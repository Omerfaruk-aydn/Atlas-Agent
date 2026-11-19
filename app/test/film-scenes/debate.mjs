// 05 · Debate: models that argue before they agree.
export default async ({ start, ask, pause, startRec }) => {
  await start(["--yolo", "-H", "npipe:////./pipe/atlas-film-2"]);
  await pause(4500);
  await startRec();
  await pause(1200);
  await ask("Use debate with two subagents, 2 rounds: should this service keep its configuration in JSON or move to YAML? Give me the verdict in three lines.");
  await pause(3000);
};
