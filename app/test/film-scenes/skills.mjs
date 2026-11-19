// 19 · Skills: a project checklist is found and followed.
export default async ({ start, ask, pause, startRec }) => {
  await start(["--yolo", "-H", "npipe:////./pipe/atlas-film-skills"]);
  await pause(4500);
  await startRec();
  await pause(1200);
  await ask("Use the release-checklist skill of this project and follow it.");
  await pause(3500);
};
