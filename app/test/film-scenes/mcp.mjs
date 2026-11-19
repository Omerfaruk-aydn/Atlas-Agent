// 18 · MCP in plain words: add a server from chat, then use its tools.
export default async ({ start, ask, pause, startRec }) => {
  await start(["--yolo", "-H", "npipe:////./pipe/atlas-film-mcp"]);
  await pause(4500);
  await startRec();
  await pause(1200);
  await ask("Add an MCP server called everything: command npx, args -y server-everything from the modelcontextprotocol scope, stdio. Use your config tool.");
  await pause(1500);
  await ask("Now call the echo tool of the everything server with the text hello atlas.");
  await pause(3500);
};
