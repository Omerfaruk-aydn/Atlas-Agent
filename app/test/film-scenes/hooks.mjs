// 13 · Hooks can veto: a PreToolUse hook protects the migrations folder.
export default async ({ start, ask, pause, startRec }) => {
  await start(["--yolo", "-H", "npipe:////./pipe/atlas-film-hooks"]);
  await pause(4500);
  await startRec();
  await pause(1200);
  await ask("Add a comment at the top of migrations/001_init.sql saying it was reviewed.");
  await pause(3500);
};
