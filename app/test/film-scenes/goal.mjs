// 06 · Goals that keep going.
export default async ({ start, ask, pause, startRec }) => {
  await start(["--yolo", "-H", "npipe:////./pipe/atlas-film-1"]);
  await pause(4500);
  await startRec();
  await pause(1200);
  await ask("/goal Add a table-driven test for ParseConfig covering a zero timeout, invalid JSON and an empty address, and make go test ./... pass.");
  await pause(3000);
};
