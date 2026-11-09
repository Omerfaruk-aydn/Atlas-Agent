// A stand-in for `atlas-agent app serve`: the same routes and stream, no model.
// The end-to-end tests drive it through the __test routes, so a run is exact
// and repeatable, and nothing here needs a provider or spends tokens.

import fs from "node:fs";
import http from "node:http";
import path from "node:path";

const TYPES = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".woff2": "font/woff2", ".svg": "image/svg+xml" };
// Keep in step with contentSecurityPolicy in internal/webui/api.go.
const CSP =
  "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self' blob: data:; worker-src 'self' blob:; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'";

const now = () => Math.floor(Date.now() / 1000);

export function session(id, title, extra = {}) {
  return { id, parent_session_id: "", title, message_count: 2, prompt_tokens: 12000, completion_tokens: 800, cost: 0.02, created_at: now(), updated_at: now(), ...extra };
}

export function userMessage(id, sessionId, text) {
  return { id, role: "user", session_id: sessionId, parts: [{ type: "text", data: { text } }], model: "", provider: "", created_at: now(), updated_at: now() };
}

export function assistantMessage(id, sessionId, parts, finished = true) {
  const all = finished ? [...parts, { type: "finish", data: { reason: "end_turn", time: now() } }] : parts;
  return { id, role: "assistant", session_id: sessionId, parts: all, model: "test-model", provider: "test", created_at: now(), updated_at: now() };
}

const catalog = {
  providers: [
    {
      id: "test",
      name: "Test Provider",
      models: [
        { id: "test-model", name: "Test Model", context_window: 200000, can_reason: true, reasoning_levels: ["low", "high"], default_reasoning_effort: "low", supports_images: true, cost_per_1m_in: 1, cost_per_1m_out: 2 },
        { id: "other-model", name: "Other Model", context_window: 100000, can_reason: false, supports_images: false },
      ],
    },
  ],
  selected: { large: { provider: "test", model: "test-model", reasoning_effort: "low" } },
  active: { provider: "test", model: "test-model", reasoning_effort: "low" },
};

export async function startFakeGateway({ renderer }) {
  const state = {
    sessions: [],
    messages: {},
    prompts: [],
    reviews: [],
    reviewScript: null,
    reviewPolls: 0,
    permissionAnswers: [],
    questionAnswers: [],
    streams: new Set(),
    seq: 0,
    login: null,
    logins: [],
    modes: [],
    mcp: [],
    commands: [],
    speechRequests: [],
    forks: [],
    sideChats: [],
    noSideChat: false,
    twoStepSideChat: false,
    iconHits: 0,
    rewindPreview: { files_to_write: 0, files_to_delete: 0 },
    // Design files by name: { doc, rev }. A save bumps rev, as a write does on disk.
    designs: {},
    designRev: 0,
    designSaves: 0,
  };
  const designInfo = (name) => ({ name, rev: state.designs[name].rev, size: JSON.stringify(state.designs[name].doc).length, modified_at: now() });

  const emit = (type, payload) => {
    const data = JSON.stringify({ seq: ++state.seq, type, payload });
    for (const res of state.streams) res.write(`id: ${state.seq}\ndata: ${data}\n\n`);
  };

  const json = (res, status, value) => {
    res.writeHead(status, { "Content-Type": "application/json", "Content-Security-Policy": CSP });
    res.end(JSON.stringify(value));
  };

  const body = (req) =>
    new Promise((resolve) => {
      let raw = "";
      req.on("data", (c) => (raw += c));
      req.on("end", () => resolve(raw ? JSON.parse(raw) : {}));
    });

  const serveFile = (res, url) => {
    const rel = url === "/" ? "index.html" : url.slice(1);
    const file = path.join(renderer, path.normalize(rel));
    if (!file.startsWith(renderer) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) {
      res.writeHead(404).end();
      return;
    }
    res.writeHead(200, { "Content-Type": TYPES[path.extname(file)] ?? "application/octet-stream", "Content-Security-Policy": CSP });
    fs.createReadStream(file).pipe(res);
  };

  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, "http://x");
    const p = url.pathname;
    const m = /^\/api\/workspaces\/ws(\/.*)?$/.exec(p);
    const rawSid = /\/sessions\/([^/]+)/.exec(p)?.[1];
    // A sub-agent's session id holds "$$", which the page sends escaped.
    const sid = rawSid === undefined ? undefined : decodeURIComponent(rawSid);

    // Pages for the built-in browser tests.
    if (p === "/__test/page") {
      res.writeHead(200, { "Content-Type": "text/html" });
      return res.end('<!doctype html><title>Atlas test page</title><body style="font:16px sans-serif"><h1>Hello from the test page</h1><a id="pop" target="_blank" href="/__test/popup">open a popup</a><a id="next" href="/__test/second">second page</a></body>');
    }
    if (p === "/__test/second") {
      res.writeHead(200, { "Content-Type": "text/html" });
      return res.end("<!doctype html><title>Second page</title><body>Second</body>");
    }
    if (p === "/__test/redirect") {
      res.writeHead(302, { Location: "file:///C:/Windows/win.ini" });
      return res.end();
    }
    if (p === "/__test/fail") {
      res.destroy();
      return;
    }
    if (p === "/__test/icon.png") {
      state.iconHits++;
      res.writeHead(200, { "Content-Type": "image/png" });
      return res.end(Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==", "base64"));
    }
    if (p === "/__test/favicon") {
      res.writeHead(200, { "Content-Type": "text/html" });
      return res.end(`<!doctype html><title>Icon page</title><link rel="icon" href="http://127.0.0.1:${server.address().port}/__test/icon.png">hello`);
    }
    if (p === "/__test/keys") {
      res.writeHead(200, { "Content-Type": "text/html" });
      return res.end('<!doctype html><title>Keys page</title><textarea id="t"></textarea><script>addEventListener("keydown",e=>{document.documentElement.dataset.keys=(document.documentElement.dataset.keys||"")+[e.ctrlKey?"C":"",e.altKey?"A":"",e.shiftKey?"S":"",e.key].join("")+"|"});t.focus()</script>');
    }
    if (p === "/__test/paint") {
      res.writeHead(200, { "Content-Type": "text/html" });
      // Part of it never changes (a fixed column), part of it repaints every frame, like a video.
      return res.end('<!doctype html><title>Paint page</title><body style="margin:0;height:100vh;background:#336699"><div style="position:fixed;left:0;top:0;width:90px;height:100vh;background:#cc3366"></div><div id="t" style="position:fixed;left:110px;top:0;right:0;height:100vh"></div><script>let n=0;const t=document.getElementById("t");const tick=()=>{t.style.background="hsl("+((n++*9)%360)+" 90% 50%)";requestAnimationFrame(tick)};tick()</script>');
    }
    if (p === "/__test/ticker") {
      res.writeHead(200, { "Content-Type": "text/html" });
      // A page that repaints every frame, like a video: the colour is the clock.
      return res.end('<!doctype html><title>Ticker page</title><body style="margin:0;height:100vh"><script>let n=0;const tick=()=>{document.body.style.background="hsl("+((n++*7)%360)+" 90% 50%)";requestAnimationFrame(tick)};tick()</script>');
    }
    if (p === "/__test/find") {
      res.writeHead(200, { "Content-Type": "text/html" });
      return res.end("<!doctype html><title>Find page</title><body style='font:16px sans-serif'><p>needle one needle two</p><p>needle three</p><p>no match here, needle four and needle five</p>");
    }
    if (p === "/__test/size") {
      res.writeHead(200, { "Content-Type": "text/html" });
      return res.end('<!doctype html><title>Size page</title><meta name="viewport" content="width=device-width"><body><h1 id="w"></h1><p id="ua"></p><script>w.textContent=innerWidth;ua.textContent=navigator.userAgent</script>');
    }
    if (p === "/__test/second-same-host") {
      res.writeHead(200, { "Content-Type": "text/html" });
      return res.end("<!doctype html><title>Same host</title><body>second</body>");
    }
    if (p === "/__test/login" && req.method === "GET") {
      res.writeHead(200, { "Content-Type": "text/html" });
      return res.end('<!doctype html><title>Login page</title><body style="font:16px sans-serif"><form id="f" action="/__test/login-done" method="post"><input id="user" name="user" type="text" style="width:200px;height:30px"><input id="pass" name="pass" type="password" style="width:200px;height:30px"><button id="go" type="submit" style="width:100px;height:30px">Sign in</button></form><input id="hidden" type="password" style="display:none"></body>');
    }
    if (p === "/__test/login-done") {
      res.writeHead(200, { "Content-Type": "text/html" });
      return res.end("<!doctype html><title>Logged in</title><body>ok</body>");
    }
    if (p === "/__test/login-script") {
      res.writeHead(200, { "Content-Type": "text/html" });
      return res.end(`<!doctype html><title>Script login</title><body><form id="f" action="/__test/login-done" method="post"><input id="user" name="user" type="text" value="script-user"><input id="pass" name="pass" type="password" value="script-secret"></form><script>setTimeout(function () { document.getElementById("f").requestSubmit(); }, 1500);</script></body>`);
    }
    if (p === "/__test/address") {
      res.writeHead(200, { "Content-Type": "text/html" });
      return res.end('<!doctype html><title>Address page</title><body style="font:16px sans-serif"><form id="f"><input id="aname" name="fullname" autocomplete="name" style="width:200px;height:30px"><input id="astreet" name="street" autocomplete="address-line1" style="width:200px;height:30px"><input id="acity" name="city" autocomplete="address-level2" style="width:200px;height:30px"><input id="apostal" name="zip" autocomplete="postal-code" style="width:200px;height:30px"><select id="acountry" name="country" autocomplete="country"><option value="">-</option><option value="TR">Turkey</option><option value="DE">Germany</option></select><input id="aphone" name="phone" autocomplete="tel" style="width:200px;height:30px"><input id="ahidden" name="hidden-street" autocomplete="address-line2" style="display:none"></form></body>');
    }
    if (p === "/__test/file" || p === "/__test/slow") {
      const body = Buffer.alloc(p === "/__test/slow" ? 400_000 : 2000, 97);
      res.writeHead(200, { "Content-Type": "application/octet-stream", "Content-Disposition": 'attachment; filename="atlas-test.txt"', "Content-Length": body.length });
      if (p === "/__test/file") return res.end(body);
      let at = 0;
      const timer = setInterval(() => {
        if (at >= body.length || res.destroyed) {
          clearInterval(timer);
          return res.end();
        }
        res.write(body.subarray(at, at + 20_000));
        at += 20_000;
      }, 250);
      res.on("close", () => clearInterval(timer));
      return;
    }
    if (p === "/__test/spam") {
      res.writeHead(200, { "Content-Type": "text/html" });
      return res.end('<!doctype html><title>Spam page</title><script>setInterval(() => window.open("/__test/popup"), 150)</script>');
    }
    if (p === "/__test/popup") {
      res.writeHead(200, { "Content-Type": "text/html" });
      return res.end("<!doctype html><title>Popup page</title><body>Popup</body>");
    }
    if (p === "/__test/emit") {
      const { type, payload } = await body(req);
      emit(type, payload);
      return json(res, 200, {});
    }
    if (p === "/__test/drop") {
      for (const s of state.streams) s.destroy();
      state.streams.clear();
      return json(res, 200, {});
    }
    if (p === "/__test/refuse") {
      state.refuse = (await body(req)).refuse;
      return json(res, 200, {});
    }
    if (p === "/__test/login") {
      state.login = await body(req);
      return json(res, 200, {});
    }
    if (p === "/__test/modes") {
      state.modes = (await body(req)).modes;
      return json(res, 200, {});
    }
    // The agent writing a design file, as design_canvas does on disk.
    if (p === "/__test/design" && req.method === "POST") {
      const { name, doc } = await body(req);
      state.designs[name] = { doc, rev: `r${++state.designRev}` };
      return json(res, 200, designInfo(name));
    }
    if (p === "/__test/design") {
      const name = url.searchParams.get("name");
      return json(res, state.designs[name] ? 200 : 404, state.designs[name] ?? {});
    }
    if (p === "/__test/state") return json(res, 200, { logins: state.logins, forks: state.forks, reviews: state.reviews, sideChats: state.sideChats, iconHits: state.iconHits, prompts: state.prompts, permissionAnswers: state.permissionAnswers, questionAnswers: state.questionAnswers, speechRequests: state.speechRequests, mcp: state.mcp });
    if (p === "/__test/seed") {
      const seed = await body(req);
      state.sessions = seed.sessions ?? [];
      state.messages = seed.messages ?? {};
      state.mcp = seed.mcp ?? [];
      state.commands = seed.commands ?? [];
      state.speechRequests = [];
      state.rewindPreview = seed.rewindPreview ?? { files_to_write: 0, files_to_delete: 0 };
      state.designs = {};
      state.designSaves = 0;
      for (const [name, doc] of Object.entries(seed.designs ?? {})) state.designs[name] = { doc, rev: `r${++state.designRev}` };
      state.forks = [];
      state.reviews = [];
      state.reviewScript = seed.review ?? null;
      state.reviewPolls = 0;
      state.sideChats = [];
      state.noSideChat = !!seed.noSideChat;
      state.twoStepSideChat = !!seed.twoStepSideChat;
      return json(res, 200, {});
    }

    if (p.startsWith("/api/") && state.refuse) {
      res.destroy();
      return;
    }
    if (p === "/api/bootstrap") return json(res, 200, { csrf: "t", instance: "i1", version: "test", server: { address: "", reachable: true, version: "test", platform: "test" }, workspaces: [] });
    if (p === "/api/projects") return json(res, 200, [{ path: "C:/demo", name: "demo" }]);
    if (p === "/api/workspaces" && req.method === "POST") {
      return json(res, 200, { id: "ws", path: (await body(req)).path, name: "demo", connected: true, agent_ready: true, configured: true });
    }
    if (m) {
      const tail = m[1] ?? "";
      if (req.method === "DELETE" && tail === "") return json(res, 200, {});
      if (tail === "/events") {
        res.writeHead(200, { "Content-Type": "text/event-stream", "Cache-Control": "no-store" });
        res.write(`data: ${JSON.stringify({ seq: state.seq, type: "hello", payload: { instance: "i1", resync: false } })}\n\n`);
        state.streams.add(res);
        req.on("close", () => state.streams.delete(res));
        return;
      }
      if (tail === "/sessions" && req.method === "GET") {
        const q = url.searchParams.get("q");
        return json(res, 200, q ? state.sessions.filter((s) => s.title.includes(q)) : state.sessions);
      }
      if (tail === "/sessions" && req.method === "POST") {
        const s = session(`s${state.sessions.length + 1}`, "New Session", { prompt_tokens: 0, completion_tokens: 0 });
        state.sessions.unshift(s);
        state.messages[s.id] = [];
        return json(res, 200, s);
      }
      if (/\/sessions\/[^/]+\/messages$/.test(tail)) return json(res, 200, state.messages[sid] ?? []);
      if (/\/sessions\/[^/]+\/prompt$/.test(tail)) {
        state.prompts.push({ sid, ...(await body(req)) });
        return json(res, 202, { run_id: "", duplicate: false });
      }
      if (/\/sessions\/[^/]+\/rewind\/preview$/.test(tail)) return json(res, 200, state.rewindPreview);
      if (/\/sessions\/[^/]+\/rewind\/before$/.test(tail)) {
        const { up_to_message_id: target } = await body(req);
        const all = state.messages[sid] ?? [];
        const fork = session(`s${state.sessions.length + 1}`, state.sessions.find((x) => x.id === sid)?.title ?? "");
        state.sessions.unshift(fork);
        state.messages[fork.id] = all.slice(0, all.findIndex((x) => x.id === target));
        state.forks.push({ from: sid, target });
        return json(res, 200, { session: fork, files_written: 0, files_deleted: 0 });
      }
      if (/\/sessions\/[^/]+\/side-chat$/.test(tail)) {
        if (state.noSideChat) return json(res, 501, { error: "the running Atlas server is an older version" });
        const source = state.sessions.find((x) => x.id === sid);
        const side = session(`side${state.sessions.length + 1}`, `Branch: ${source?.title ?? ""}`, { tags: [`side-of:${sid}`] });
        state.sessions.unshift(side);
        state.messages[side.id] = [...(state.messages[sid] ?? [])];
        state.sideChats.push({ from: sid, id: side.id });
        // The server used to say so in two steps: created without its tag, then updated with it.
        if (state.twoStepSideChat) {
          emit("session", { type: "created", payload: { ...side, tags: [] } });
          emit("session", { type: "updated", payload: side });
        }
        return json(res, 200, side);
      }
      if (/\/sessions\/[^/]+\/reviews$/.test(tail) && req.method === "POST") {
        const request = await body(req);
        if (state.reviewScript?.refuse) return json(res, state.reviewScript.refuse, { error: state.reviewScript.message ?? "refused" });
        state.reviews.push({ sid, ...request });
        state.reviewPolls = 0;
        return json(res, 202, { id: "r1", session_id: sid, state: "running", findings: [], structured: false, started_at: Date.now() });
      }
      if (/\/reviews\/[^/]+$/.test(tail) && req.method === "GET") {
        const script = state.reviewScript ?? {};
        state.reviewPolls += 1;
        if (state.reviewPolls <= (script.polls ?? 2)) return json(res, 200, { id: "r1", session_id: "s1", state: "running", findings: [], structured: false, started_at: Date.now() - 3000 });
        if (script.fail) return json(res, 200, { id: "r1", session_id: "s1", state: "failed", error: script.fail, findings: [], structured: false, started_at: 0 });
        return json(res, 200, { id: "r1", session_id: "s1", state: "done", structured: script.structured ?? true, summary: script.summary ?? "One problem found.", model: "fake-model", findings: script.findings ?? [], started_at: 0, finished_at: 1 });
      }
      if (/\/reviews\/[^/]+$/.test(tail) && req.method === "DELETE") return json(res, 200, { id: "r1", session_id: "s1", state: "canceled", findings: [], structured: false, started_at: 0 });
      if (/\/sessions\/[^/]+\/cancel$/.test(tail)) return json(res, 200, {});
      if (/\/sessions\/[^/]+$/.test(tail) && req.method === "PATCH") {
        const { title } = await body(req);
        const s = state.sessions.find((x) => x.id === sid);
        if (s) s.title = title;
        return json(res, 200, s ?? {});
      }
      if (/\/sessions\/[^/]+$/.test(tail) && req.method === "DELETE") {
        state.sessions = state.sessions.filter((x) => x.id !== sid);
        return json(res, 200, {});
      }
      if (tail === "/pending") return json(res, 200, { permissions: [], questions: [] });
      if (tail.startsWith("/permissions/")) {
        state.permissionAnswers.push((await body(req)).action);
        return json(res, 200, { resolved: true });
      }
      if (/^\/questions\/[^/]+\/answer$/.test(tail)) {
        state.questionAnswers.push((await body(req)).responses);
        return json(res, 200, { resolved: true });
      }
      if (tail === "/models") {
        if (req.method === "POST") {
          const pick = await body(req);
          catalog.active = catalog.selected.large = { provider: pick.provider, model: pick.model, reasoning_effort: pick.reasoning_effort };
        }
        return json(res, 200, catalog);
      }
      if (tail === "/settings") return json(res, 200, { session_mode: "", modes: state.modes, permission_mode: "manual", auto_compact: true, auto_compact_at: 0, mcp: state.mcp, skills: [], lsp: [] });
      if (tail.startsWith("/mcp/")) {
        const name = decodeURIComponent(tail.slice(5));
        const server = state.mcp.find((item) => item.name === name);
        if (!server) return json(res, 404, { error: "MCP not configured" });
        server.enabled = (await body(req)).enabled;
        server.state = server.enabled ? "connected" : "disabled";
        return json(res, 200, { enabled: server.enabled });
      }
      if (tail === "/speech") {
        if (req.method === "GET") return json(res, 200, { installed: ["en-US", "tr-TR"], language: "en-US", backend: "vosk" });
        const chunks = [];
        for await (const chunk of req) chunks.push(chunk);
        const wav = Buffer.concat(chunks);
        state.speechRequests.push({ header: wav.subarray(0, 4).toString(), size: wav.length, rate: wav.readUInt32LE(24) });
        return json(res, 200, { text: "Dictated words." });
      }
      if (tail === "/permission-mode") return json(res, 200, await body(req));
      if (tail === "/providers") {
        return json(res, 200, [
          { id: "test", name: "Test Provider", models: 2, configured: true },
          { id: "kimi-code", name: "Kimi Code", models: 5, configured: false, account: true },
          { id: "zai", name: "Z.AI Coding Plan", models: 3, configured: false },
        ]);
      }
      if (/^\/providers\/[^/]+\/login$/.test(tail)) {
        if (req.method === "POST") {
          state.logins.push(tail.split("/")[2]);
          state.login = { state: "waiting", url: "https://example.test/device", user_code: "ABCD-1234" };
          return json(res, 200, state.login);
        }
        if (req.method === "DELETE") {
          state.login = null;
          res.writeHead(204).end();
          return;
        }
        return json(res, state.login ? 200 : 404, state.login ?? {});
      }
      if (tail === "/commands") return json(res, 200, state.commands);
      if (tail === "/commands/expand") { const command = await body(req); return json(res, 200, { prompt: `Expanded ${command.id}` }); }
      if (tail === "/files") return json(res, 200, { path: "", entries: [{ name: "main.go", path: "main.go", is_dir: false }] });
      if (tail === "/files/search") return json(res, 200, { files: ["main.go"] });
      if (tail === "/files/content") return json(res, 200, { path: "main.go", content: "package main\n", size: 13, modified_at: 0, sha256: "" });
      if (tail === "/jobs") return json(res, 200, []);
      if (tail === "/design/files") return json(res, 200, { files: Object.keys(state.designs).map(designInfo) });
      if (tail === "/design/file" && req.method === "GET") {
        const name = url.searchParams.get("name");
        const entry = state.designs[name];
        if (!entry) return json(res, 404, { message: `no design named ${name}` });
        if (url.searchParams.get("rev") === entry.rev) return json(res, 200, { name, rev: entry.rev, unchanged: true });
        return json(res, 200, { name, rev: entry.rev, doc: entry.doc });
      }
      if (tail === "/design/file" && req.method === "POST") {
        const { name, doc, base_rev: base, create } = await body(req);
        const entry = state.designs[name];
        if (create && entry) return json(res, 400, { message: `a design named "${name}" already exists` });
        if (entry && base && base !== entry.rev) return json(res, 409, { error: "the design file changed since it was read", rev: entry.rev });
        state.designs[name] = { doc, rev: `r${++state.designRev}` };
        state.designSaves++;
        return json(res, 200, { name, rev: state.designs[name].rev });
      }
      if (tail === "/design/asset") return json(res, 404, { message: "no such image" });
      return json(res, 404, { message: `no route ${tail}` });
    }
    serveFile(res, p);
  });

  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address();
  const origin = `http://127.0.0.1:${port}`;

  const call = async (route, value) => {
    const res = await fetch(`${origin}${route}`, { method: "POST", body: JSON.stringify(value ?? {}) });
    return res.json();
  };

  return {
    origin,
    emit: (type, payload) => call("/__test/emit", { type, payload }),
    drop: () => call("/__test/drop"),
    refuse: (refuse) => call("/__test/refuse", { refuse }),
    seed: (value) => call("/__test/seed", value),
    login: (value) => call("/__test/login", value),
    modes: (modes) => call("/__test/modes", { modes }),
    state: () => fetch(`${origin}/__test/state`).then((r) => r.json()),
    /** Writes a design file the way the agent does; the app should pick it up. */
    writeDesign: (name, doc) => call("/__test/design", { name, doc }),
    design: (name) => fetch(`${origin}/__test/design?name=${encodeURIComponent(name)}`).then((r) => r.json()),
    close: () =>
      new Promise((resolve) => {
        for (const s of state.streams) s.destroy();
        server.close(resolve);
        server.closeAllConnections();
      }),
  };
}
