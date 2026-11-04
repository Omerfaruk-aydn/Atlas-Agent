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
    permissionAnswers: [],
    questionAnswers: [],
    streams: new Set(),
    seq: 0,
    login: null,
    logins: [],
    modes: [],
    forks: [],
    rewindPreview: { files_to_write: 0, files_to_delete: 0 },
  };

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
    const sid = /\/sessions\/([^/]+)/.exec(p)?.[1];

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
    if (p === "/__test/state") return json(res, 200, { logins: state.logins, forks: state.forks, prompts: state.prompts, permissionAnswers: state.permissionAnswers, questionAnswers: state.questionAnswers });
    if (p === "/__test/seed") {
      const seed = await body(req);
      state.sessions = seed.sessions ?? [];
      state.messages = seed.messages ?? {};
      state.rewindPreview = seed.rewindPreview ?? { files_to_write: 0, files_to_delete: 0 };
      state.forks = [];
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
      if (tail === "/settings") return json(res, 200, { session_mode: "", modes: state.modes, permission_mode: "manual", auto_compact: true, auto_compact_at: 0, mcp: [], skills: [], lsp: [] });
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
      if (tail === "/commands") return json(res, 200, []);
      if (tail === "/files") return json(res, 200, { path: "", entries: [{ name: "main.go", path: "main.go", is_dir: false }] });
      if (tail === "/files/search") return json(res, 200, { files: ["main.go"] });
      if (tail === "/files/content") return json(res, 200, { path: "main.go", content: "package main\n", size: 13, modified_at: 0, sha256: "" });
      if (tail === "/jobs") return json(res, 200, []);
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
    close: () =>
      new Promise((resolve) => {
        for (const s of state.streams) s.destroy();
        server.close(resolve);
        server.closeAllConnections();
      }),
  };
}
