// 16 · Pull request review on a real PR.
export default async ({ start, ask, pause, startRec }) => {
  await start(["--yolo", "-H", "npipe:////./pipe/atlas-film-pr"]);
  await pause(4500);
  await startRec();
  await pause(1200);
  await ask("Review pull request " + (process.env.FILM_PR || "1") + " of this repository with github_pr_view. Give me priorities and a verdict in a short list.");
  await pause(3500);
};
