// 10 · LSP refactor: a rename that goes through the language server.
export default async ({ start, ask, pause, startRec }) => {
  await start(["--yolo", "-H", "npipe:////./pipe/atlas-film-rename"]);
  await pause(4500);
  await startRec();
  await pause(1200);
  await ask("Rename ParseConfig to LoadConfig everywhere with the language server, then run go build ./... to prove it still compiles.");
  await pause(3500);
};
