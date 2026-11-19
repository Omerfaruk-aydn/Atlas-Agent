// 17 · Worktrees: two tasks, two branches, one repository.
export default async ({ start, ask, pause, startRec }) => {
  await start(["--yolo", "-H", "npipe:////./pipe/atlas-film-wt"]);
  await pause(4500);
  await startRec();
  await pause(1200);
  await ask("Make two git worktrees, on branches feat/docs and fix/addr. Add a comment above ParseConfig in the first and make ParseConfig reject an empty address in the second. Then show git worktree list.");
  await pause(3500);
};
