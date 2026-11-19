// 11 · Safe by default: a risky command waits for a human.
export default async ({ start, type, enter, key, settle, pause, startRec, screen }) => {
  await start(["-H", "npipe:////./pipe/atlas-film-safe"]);
  await pause(4500);
  await startRec();
  await pause(1200);
  await type("Delete the build directory with rm -rf build, then tell me what is left.");
  await pause(500);
  await enter();
  const s = await settle("Permission Required", { quiet: 2500, timeout: 180000 });
  await pause(3500);
  await key("d"); await pause(1600);
  await pause(8000);
  for (let i = 0, quiet = 0; i < 240 && quiet < 6; i++) {
    quiet = /ctrl\+c stop/.test(await screen()) ? 0 : quiet + 1;
    await pause(1000);
  }
  await pause(3000);
};
