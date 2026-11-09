// Unit tests for the logic behind the thread and composer.

import { describe, expect, test, vi } from "vitest";
import fs from "node:fs";
import vm from "node:vm";
import { createRequire } from "node:module";
import { EventEmitter } from "node:events";

vi.stubGlobal("window", { __atlasBoot: { ui: { lang: "en" } } });
vi.stubGlobal("document", { documentElement: { dataset: {}, lang: "" } });
vi.stubGlobal("navigator", { language: "en" });
vi.stubGlobal("location", { search: "" });

const { buildTurns, finalTextStart, toolCalls, turnPreview, userFiles } = await import("../renderer/src/lib/turns");
const { category, groupLabel, isDenied, toolArg, toolVerb } = await import("../renderer/src/lib/tools");
const { addFiles, attachmentMime, MAX_ATTACHMENTS, MAX_FILE_BYTES } = await import("../renderer/src/lib/attachments");
const { languageOf, languageOfFile } = await import("../renderer/src/lib/highlight");
const { clock, compact, duration, money, toMs } = await import("../renderer/src/lib/format");
const { contextShares } = await import("../renderer/src/lib/context");
const { logoOf } = await import("../renderer/src/lib/logos");
const { nextCut, stepSize, wordEnd } = await import("../renderer/src/lib/stream-pace");
const { dropIndex, fileEdits, moveItem, permissionSentence, plainText } = await import("../renderer/src/lib/chat");
const { countBySeverity, emptyMemory, findingKey, fixAllPrompt, fixPrompt, loadMemory, locationOf, saveMemory, severityRank } = await import("../renderer/src/lib/review");
const { agentSessionId, feedOf, latestActivity, runDuration, runReport, stepCount } = await import("../renderer/src/lib/subagent");
const shortcutsModule = await import("../renderer/src/lib/shortcuts");
const { allChords, canBind, holderOf, keysOf, rebind, SHORTCUTS } = shortcutsModule;
const { providerGroup } = await import("../renderer/src/features/models/Providers");
const { pcmWav } = await import("../renderer/src/features/composer/VoiceInput");
const { LaunchQueue } = createRequire(import.meta.url)("../main/launch-queue.cjs");
const { watchLoading } = createRequire(import.meta.url)("../main/window-loading.cjs");

describe("desktop startup", () => {
  test("a timed-out initial core does not recursively trigger the crash restart", async () => {
    vi.useFakeTimers();
    try {
      const child = Object.assign(new EventEmitter(), { stdin: Object.assign(new EventEmitter(), { end: vi.fn() }), stdout: new EventEmitter(), stderr: new EventEmitter(), kill: vi.fn() });
      child.kill.mockImplementation(() => child.emit("exit", 1));
      const lines = new EventEmitter();
      const module = { exports: {} as { Gateway: new (options: unknown) => { start(): Promise<unknown> } } };
      vm.runInNewContext(fs.readFileSync(new URL("../main/gateway.cjs", import.meta.url), "utf8"), {
        module, setTimeout, clearTimeout,
        require: (name: string) => name === "node:child_process" ? { spawn: () => child } : name === "node:os" ? { homedir: () => "C:\\Atlas-test" } : { createInterface: () => lines },
      });
      const crash = vi.fn();
      const gateway = new module.exports.Gateway({ bin: "test.exe", renderer: "renderer", onCrash: crash });
      const failure = expect(gateway.start()).rejects.toThrow("did not start in time");
      await vi.advanceTimersByTimeAsync(60_000);
      await failure;
      expect(child.kill).toHaveBeenCalledOnce();
      expect(crash).not.toHaveBeenCalled();
    } finally { vi.useRealTimers(); }
  });
  test("repeated launches wait for readiness and preserve each requested project", () => {
    const open = vi.fn();
    const focus = vi.fn();
    const queue = new LaunchQueue("first", open, focus);
    queue.request("first"); queue.request("second"); queue.request("");
    expect(open).not.toHaveBeenCalled();
    queue.ready();
    expect(open.mock.calls).toEqual([["first"], ["second"]]);
    expect(focus).toHaveBeenCalledOnce();
    queue.pause(); queue.request("third");
    expect(open).toHaveBeenCalledTimes(2);
    queue.ready();
    expect(open).toHaveBeenLastCalledWith("third");
  });

  function windowStub() {
    const win = Object.assign(new EventEmitter(), { webContents: new EventEmitter(), loadURL: vi.fn().mockResolvedValue(undefined), isDestroyed: () => false, isVisible: () => false, show: vi.fn(), close: vi.fn() });
    return win;
  }

  test("a failed main navigation retries once and surfaces a persistent failure", async () => {
    vi.useFakeTimers();
    const win = windowStub();
    try {
      const failure = vi.fn().mockResolvedValue(false);
      const loading = watchLoading(win, { url: () => "http://127.0.0.1/", record: vi.fn(), onFailure: failure });
      loading.load("project");
      win.webContents.emit("did-fail-load", {}, -105, "failed", "", true);
      await vi.advanceTimersByTimeAsync(200);
      expect(win.loadURL).toHaveBeenCalledTimes(2);
      win.webContents.emit("did-fail-load", {}, -105, "failed", "", true);
      await vi.advanceTimersByTimeAsync(0);
      expect(failure).toHaveBeenCalledOnce();
      expect(win.close).toHaveBeenCalledOnce();
    } finally { win.emit("closed"); vi.useRealTimers(); }
  });

  test("HTML without a committed renderer times out, but readiness cancels recovery", async () => {
    vi.useFakeTimers();
    const win = windowStub();
    try {
      const failure = vi.fn();
      const loading = watchLoading(win, { url: () => "http://127.0.0.1/", record: vi.fn(), onFailure: failure });
      loading.load("project");
      await vi.advanceTimersByTimeAsync(15_200);
      expect(win.loadURL).toHaveBeenCalledTimes(2);
      loading.ready();
      await vi.advanceTimersByTimeAsync(60_000);
      expect(win.loadURL).toHaveBeenCalledTimes(2);
      expect(failure).not.toHaveBeenCalled();
    } finally { win.emit("closed"); vi.useRealTimers(); }
  });

  test("closing a window cancels a pending renderer-crash recovery", async () => {
    vi.useFakeTimers();
    const win = windowStub();
    try {
      const loading = watchLoading(win, { url: () => "http://127.0.0.1/", record: vi.fn(), onFailure: vi.fn() });
      loading.load("project"); loading.ready();
      win.webContents.emit("render-process-gone", {}, { reason: "crashed", exitCode: 1 });
      win.emit("close");
      await vi.advanceTimersByTimeAsync(20_000);
      expect(win.loadURL).toHaveBeenCalledOnce();
    } finally { win.emit("closed"); vi.useRealTimers(); }
  });
});

describe("offline voice", () => {
  test("WAV output is mono PCM with bounded 16-bit samples", () => {
    const wav = pcmWav([new Float32Array([-2, -1, 0]), new Float32Array([1, 2])], 16000);
    const view = new DataView(wav);
    expect(new TextDecoder().decode(wav.slice(0, 4))).toBe("RIFF");
    expect(new TextDecoder().decode(wav.slice(8, 12))).toBe("WAVE");
    expect(view.getUint32(4, true)).toBe(wav.byteLength - 8);
    expect(view.getUint16(22, true)).toBe(1);
    expect(view.getUint32(24, true)).toBe(16000);
    expect(view.getUint32(40, true)).toBe(10);
    expect([0, 1, 2, 3, 4].map((i) => view.getInt16(44 + i * 2, true))).toEqual([-32768, -32768, 0, 32767, 32767]);
  });
  test("only the local app may request microphone access; camera remains denied", () => {
    let request: (...args: any[]) => void;
    let check: (...args: any[]) => boolean;
    const session = { webRequest: { onBeforeRequest() {} }, setPermissionRequestHandler: (fn: typeof request) => { request = fn; }, setPermissionCheckHandler: (fn: typeof check) => { check = fn; } };
    const module = { exports: {} as { hardenSession(origin: () => string): void } };
    vm.runInNewContext(fs.readFileSync(new URL("../main/security.cjs", import.meta.url), "utf8"), { module, URL, require: () => ({ session: { fromPartition: () => session }, shell: {} }) });
    module.exports.hardenSession(() => "http://127.0.0.1:7777");
    const local = { getURL: () => "http://127.0.0.1:7777/" };
    const remote = { getURL: () => "https://other.example/" };
    const answer = vi.fn();
    request!(local, "media", answer, { mediaTypes: ["audio"] });
    expect(answer).toHaveBeenLastCalledWith(true);
    request!(local, "media", answer, { mediaTypes: ["video"] });
    expect(answer).toHaveBeenLastCalledWith(false);
    request!(remote, "media", answer, { mediaTypes: ["audio"] });
    expect(answer).toHaveBeenLastCalledWith(false);
    expect(check!(local, "media", "", { mediaType: "audio" })).toBe(true);
    expect(check!(local, "media", "", { mediaType: "video" })).toBe(false);
  });
});

type Message = import("../renderer/src/api/types").Message;
type Part = import("../renderer/src/api/types").Part;

const base = { session_id: "s", model: "m", provider: "p", created_at: 100, updated_at: 100 };
const user = (id: string, text: string, parts: Part[] = []): Message => ({ ...base, id, role: "user", parts: [{ type: "text", data: { text } }, ...parts] });
const assistant = (id: string, parts: Part[], extra: Partial<Message> = {}): Message => ({ ...base, id, role: "assistant", parts, ...extra });
const text = (t: string): Part => ({ type: "text", data: { text: t } });
const call = (id: string, name = "view", input: unknown = { file_path: `${id}.go` }): Part => ({ type: "tool_call", data: { id, name, input: JSON.stringify(input) } });
const result = (id: string, content = "ok", is_error = false, metadata = ""): Part => ({ type: "tool_result", data: { tool_call_id: id, name: "view", content, metadata, is_error } });

describe("buildTurns", () => {
  test("starts a turn at each user message", () => {
    const turns = buildTurns([user("u1", "a"), assistant("a1", [text("b")]), user("u2", "c"), assistant("a2", [text("d")])]);
    expect(turns.map((t) => t.id)).toEqual(["u1", "u2"]);
    expect(turns[1].steps).toHaveLength(1);
  });

  test("a summary is its own turn and leaves the turn before it alone", () => {
    const turns = buildTurns([user("u1", "a"), assistant("a1", [text("b")]), assistant("sum", [text("summary")], { is_summary_message: true })]);
    expect(turns).toHaveLength(2);
    expect(turns[0].summary).toBe(false);
    expect(turns[1].summary).toBe(true);
  });

  test("a command the person ran is a step of its own turn", () => {
    const shell: Message = { ...base, id: "u", role: "user", parts: [{ type: "shell_command", data: { command: "ls", output: "a.txt", exit_code: 0 } }] };
    const [turn] = buildTurns([shell]);
    expect(turn.steps).toEqual([{ kind: "shell", id: "u:0", command: "ls", output: "a.txt", exitCode: 0 }]);
    expect(turn.model).toBe("");
  });

  test("groups parallel calls of a kind and keeps others apart", () => {
    const [turn] = buildTurns([user("u", "a"), assistant("a", [call("1"), call("2"), call("3", "bash", { command: "ls" }), result("1"), result("2")])]);
    expect(turn.steps.map((s) => s.kind)).toEqual(["group", "tool"]);
    expect(turn.tools).toBe(3);
    expect(toolCalls(turn.steps)).toHaveLength(3);
  });

  test("calls from different messages are not grouped", () => {
    const [turn] = buildTurns([user("u", "a"), assistant("a", [call("1")]), assistant("b", [call("2")])]);
    expect(turn.steps.map((s) => s.kind)).toEqual(["tool", "tool"]);
  });

  test("never groups agents", () => {
    const [turn] = buildTurns([user("u", "a"), assistant("a", [call("1", "agent", {}), call("2", "agent", {})])]);
    expect(turn.steps.map((s) => s.kind)).toEqual(["tool", "tool"]);
  });

  test("adds up changed files, skipping failed edits", () => {
    const meta = JSON.stringify({ additions: 3, removals: 1 });
    const edit = (id: string) => call(id, "edit", { file_path: "a.go" });
    const [turn] = buildTurns([user("u", "a"), assistant("a", [edit("1"), result("1", "ok", false, meta)]), assistant("b", [edit("2"), result("2", "ok", false, meta)]), assistant("c", [edit("3"), result("3", "no", true, meta)])]);
    expect(turn.changes).toEqual([{ path: "a.go", additions: 6, removals: 2 }]);
  });

  test("collects sources from searches and fetches", () => {
    const search = "Found 2 search results:\n\n1. Go release\n   URL: https://go.dev/doc\n   Summary: x\n\n2. Blog\n   URL: https://www.example.com/a\n   Summary: y\n";
    const [turn] = buildTurns([user("u", "a"), assistant("a", [call("1", "web_search", { query: "go" }), result("1", search), call("2", "fetch", { url: "https://go.dev/doc" }), result("2")])]);
    expect(turn.sources).toEqual([
      { url: "https://go.dev/doc", host: "go.dev", title: "Go release" },
      { url: "https://www.example.com/a", host: "example.com", title: "Blog" },
    ]);
  });

  test("a fetch still streaming its input does not throw", () => {
    const part: Part = { type: "tool_call", data: { id: "1", name: "fetch", input: '{"url": "htt' } };
    expect(buildTurns([user("u", "a"), assistant("a", [part])])[0].sources).toEqual([]);
  });

  test("finalTextStart finds the closing answer", () => {
    const [turn] = buildTurns([user("u", "a"), assistant("a", [text("first"), call("1"), result("1"), text("last")])]);
    expect(finalTextStart(turn.steps)).toBe(2);
  });
});

describe("userFiles", () => {
  test("turns images into data URLs and names other files", () => {
    const m = user("u", "a", [
      { type: "binary", data: { Path: "C:\\tmp\\a.png", MIMEType: "image/png", Data: "AAAA" } },
      { type: "binary", data: { Path: "notes.md", MIMEType: "text/plain" } },
    ]);
    expect(userFiles(m)).toEqual([{ name: "a.png", image: "data:image/png;base64,AAAA" }, { name: "notes.md", image: undefined }]);
  });
});

describe("tools", () => {
  test("verbs turn past when the call ends", () => {
    expect(toolVerb("view", false)).toBe("Reading");
    expect(toolVerb("view", true)).toBe("Read");
    expect(toolVerb("mcp_some_tool", true)).toBe("Some tool");
  });

  test("categories follow the tool names", () => {
    expect(category("grep")).toBe("search");
    expect(category("lsp_hover")).toBe("lsp");
    expect(category("unknown")).toBe("other");
  });

  test("group labels count", () => {
    expect(groupLabel("read", "view", 3, true)).toBe("Read 3 files");
    expect(groupLabel("read", "view", 1, false)).toBe("Reading 1 file");
    expect(groupLabel("other", "my_tool", 2, true)).toBe("My tool · 2");
  });

  test("arguments pick the useful part", () => {
    const tool = (name: string, input: unknown) => ({ id: "1", name, input: JSON.stringify(input) });
    expect(toolArg(tool("view", { file_path: "a/b/c.go", offset: 9, limit: 10 }))).toBe("c.go L10-19");
    expect(toolArg(tool("fetch", { url: "https://go.dev/doc" }))).toBe("go.dev");
    expect(toolArg({ id: "1", name: "view", input: "{bad" })).toBe("");
    expect(toolArg(tool("image_compare", { reference_path: "design/mock.png", candidate_path: ".atlas/shot.png" }))).toBe("mock.png ↔ shot.png");
    expect(toolVerb("image_compare", true)).toBe("Compared images");
    expect(category("enter_plan_mode")).toBe("plan");
  });

  test("denied results are told from errors", () => {
    const r = (content: string) => ({ tool_call_id: "1", name: "x", content, metadata: "", is_error: true });
    expect(isDenied(r("User denied permission"))).toBe(true);
    expect(isDenied(r("file not found"))).toBe(false);
  });
});

describe("attachments", () => {
  vi.stubGlobal("URL", Object.assign(URL, { createObjectURL: () => "blob:x" }));
  const file = (name: string, type: string, size = 10) => ({ name, type, size }) as File;

  test("types follow the gateway", () => {
    expect(attachmentMime(file("a.png", "image/png"))).toBe("image/png");
    expect(attachmentMime(file("a.svg", "image/svg+xml"))).toBe("");
    expect(attachmentMime(file("a.go", ""))).toBe("text/plain");
    expect(attachmentMime(file("a.json", "application/json"))).toBe("application/json");
    expect(attachmentMime(file("a.exe", "application/x-msdownload"))).toBe("");
  });

  test("limits stop what does not fit and keep the rest", () => {
    const big = addFiles([], [file("ok.txt", "text/plain"), file("big.txt", "text/plain", MAX_FILE_BYTES + 1)]);
    expect(big.next).toHaveLength(1);
    expect(big.error).toContain("big.txt");

    const many = addFiles([], Array.from({ length: MAX_ATTACHMENTS + 2 }, (_, i) => file(`${i}.txt`, "text/plain")));
    expect(many.next).toHaveLength(MAX_ATTACHMENTS);
    expect(many.error).not.toBe("");
  });

  test("the total is capped", () => {
    const eight = 8 << 20;
    const { next, error } = addFiles([], [1, 2, 3, 4].map((i) => file(`${i}.txt`, "text/plain", eight)));
    expect(next).toHaveLength(3);
    expect(error).not.toBe("");
  });
});

describe("highlighting languages", () => {
  test("aliases resolve and unknowns stay plain", () => {
    expect(languageOf("ts")).toBe("typescript");
    expect(languageOf("GO")).toBe("go");
    expect(languageOf("brainfuck")).toBe("");
    expect(languageOfFile("src/app.tsx")).toBe("tsx");
    expect(languageOfFile("Dockerfile")).toBe("docker");
    expect(languageOfFile("LICENSE")).toBe("");
  });
});

describe("turnPreview", () => {
  test("pairs the question with the closing answer, without markdown", () => {
    const [turn] = buildTurns([user("u", "Fix the **bug**\nplease"), assistant("a", [text("first"), call("1"), result("1"), text("Done: `main.go` works.")])]);
    expect(turnPreview(turn)).toEqual({ title: "Fix the bug please", body: "Done: main.go works." });
  });
});

describe("contextShares", () => {
  const tool = assistant("t", [call("1", "view", { file_path: "a" }), result("1", "x".repeat(4000))]);

  test("what the messages do not explain is the system prompt", () => {
    const shares = contextShares([user("u", "x".repeat(400)), tool], 10_000);
    expect(shares.map((s) => s.part)).toEqual(["system", "user", "tools"]);
    expect(shares.find((s) => s.part === "user")?.tokens).toBe(100);
    expect(shares.reduce((sum, s) => sum + s.tokens, 0)).toBe(10_000);
  });

  test("messages before the last summary no longer count", () => {
    const old = user("old", "x".repeat(40_000));
    const summary = assistant("s", [text("x".repeat(400))], { is_summary_message: true });
    const shares = contextShares([old, summary, user("u", "x".repeat(40))], 1000);
    expect(shares.find((s) => s.part === "summary")?.tokens).toBe(100);
    expect(shares.find((s) => s.part === "user")?.tokens).toBe(10);
  });

  test("estimates over the real total shrink to fit it", () => {
    const shares = contextShares([user("u", "x".repeat(40_000))], 500);
    expect(shares.reduce((sum, s) => sum + s.tokens, 0)).toBe(500);
  });
});

describe("shortcuts", () => {
  const shortcut = (id: string) => SHORTCUTS.find((s) => s.id === id)!;
  const key = (key: string, mods: Partial<KeyboardEvent> = {}) => ({ key, ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, ...mods });

  test("an override replaces the defaults", () => {
    expect(keysOf(shortcut("new"), {})).toEqual(["Ctrl+N"]);
    expect(keysOf(shortcut("new"), { new: ["Ctrl+J"] })).toEqual(["Ctrl+J"]);
  });

  test("a chord moves to its new shortcut", () => {
    const next = rebind("new", "Ctrl+B", {});
    expect(keysOf(shortcut("new"), next)).toEqual(["Ctrl+B"]);
    expect(keysOf(shortcut("sidebar"), next)).toEqual([]);
    expect(holderOf("Ctrl+B", next, "new")).toBeUndefined();
  });

  test("the holder of a chord is found, leaving out the one being changed", () => {
    expect(holderOf("Ctrl+B", {})?.id).toBe("sidebar");
    expect(holderOf("Ctrl+B", {}, "sidebar")).toBeUndefined();
  });

  test("only chords that cannot be typed can be bound", () => {
    expect(canBind(key("k", { ctrlKey: true }))).toBe(true);
    expect(canBind(key("F5"))).toBe(true);
    expect(canBind(key("Tab", { shiftKey: true }))).toBe(true);
    expect(canBind(key("k"))).toBe(false);
    expect(canBind(key("K", { shiftKey: true }))).toBe(false);
    expect(canBind(key("Control", { ctrlKey: true }))).toBe(false);
    expect(canBind(key("Escape", { ctrlKey: true }))).toBe(false);
  });
});

describe("providers", () => {
  test("every account provider has its logo, and an unknown one has none", () => {
    for (const id of ["chatgpt", "claude", "copilot", "antigravity", "muse", "kimi-code", "minimax-plan", "minimax-plan-cn"]) expect(logoOf(id)).toContain("<svg");
    expect(logoOf("neuralwatt")).toBe("");
    expect(logoOf("openai")).not.toContain("<title>");
  });

  test("group by how they connect", () => {
    expect(providerGroup({ name: "Kimi Code", account: true })).toBe("account");
    expect(providerGroup({ name: "Z.AI Coding Plan" })).toBe("plan");
    expect(providerGroup({ name: "Qiniu Token Plan" })).toBe("plan");
    expect(providerGroup({ name: "Groq" })).toBe("key");
  });
});

describe("format", () => {
  test("timestamps in seconds and milliseconds agree", () => {
    expect(toMs(1_700_000_000)).toBe(1_700_000_000_000);
    expect(toMs(1_700_000_000_000)).toBe(1_700_000_000_000);
  });

  test("durations and clocks", () => {
    expect(duration(154_000)).toBe("2m 34s");
    expect(clock(62_000)).toBe("1:02");
  });

  test("token counts and small amounts", () => {
    expect(compact(116_300)).toBe("116.3K");
    expect(compact(1_100_000)).toBe("1.1M");
    expect(money(0.0081)).toBe("$0.008");
    expect(money(1.5)).toBe("$1.50");
  });
});

const panel = await import("../renderer/src/lib/panel");
const { webUrl } = createRequire(import.meta.url)("../main/security.cjs");

describe("side panel", () => {
  test("the address bar turns entries into pages or searches", () => {
    const { normalizeAddress, SEARCH_URL } = panel;
    expect(normalizeAddress("")).toBe("");
    expect(normalizeAddress("  ")).toBe("");
    expect(normalizeAddress("https://example.com/a?b=1")).toBe("https://example.com/a?b=1");
    expect(normalizeAddress("example.com")).toBe("https://example.com/");
    expect(normalizeAddress("docs.example.co.uk/path")).toBe("https://docs.example.co.uk/path");
    expect(normalizeAddress("localhost:3000")).toBe("http://localhost:3000/");
    expect(normalizeAddress("127.0.0.1:8080/x")).toBe("http://127.0.0.1:8080/x");
    expect(normalizeAddress("how to reverse a list")).toBe(SEARCH_URL + encodeURIComponent("how to reverse a list"));
    expect(normalizeAddress("hello")).toBe(SEARCH_URL + "hello");
    // Other schemes never load as pages.
    for (const bad of ["javascript:alert(1)", "file:///C:/Windows/win.ini", "data:text/html,<b>x</b>", "chrome://settings"]) {
      expect(normalizeAddress(bad).startsWith(SEARCH_URL)).toBe(true);
    }
  });

  test("the main process loads only http and https", () => {
    expect(webUrl("https://example.com/")).toBe("https://example.com/");
    expect(webUrl("http://127.0.0.1:1/x")).toBe("http://127.0.0.1:1/x");
    for (const bad of ["javascript:alert(1)", "file:///C:/x", "data:text/html,x", "chrome://gpu", "devtools://x", "about:blank", "", "not a url"]) {
      expect(webUrl(bad)).toBe("");
    }
  });

  test("a saved panel keeps what is valid and drops the rest", () => {
    const saved = panel.sanitizePanel({
      open: true,
      width: 99999,
      expanded: true,
      active: "gone",
      tabs: [
        { id: "a", kind: "web", url: "https://example.com/", title: "Example" },
        { id: "a", kind: "web" },
        { id: "b", kind: "web", url: "javascript:alert(1)" },
        { id: "c", kind: "nonsense" },
        { id: "d", kind: "side" },
        { id: "e", kind: "side", sessionId: "s9" },
        { id: "f", kind: "terminal" },
        "junk",
      ],
    });
    expect(saved.tabs.map((x: { id: string }) => x.id)).toEqual(["a", "b", "e", "f"]);
    expect(saved.tabs[1].url).toBeUndefined();
    expect(saved.active).toBe("a");
    expect(saved.width).toBe(2000);
    expect(saved.open).toBe(true);
    const empty = panel.sanitizePanel({ open: true, expanded: true, tabs: [] });
    expect(empty.open).toBe(false);
    expect(empty.expanded).toBe(false);
    expect(empty.width).toBe(panel.PANEL_DEFAULT);
    expect(panel.sanitizePanel(null).tabs).toEqual([]);
  });

  test("width keeps both the panel and the thread at their minimum", () => {
    expect(panel.clampWidth(100, 1200)).toBe(panel.PANEL_MIN);
    expect(panel.clampWidth(900, 1200)).toBe(1200 - panel.THREAD_MIN);
    expect(panel.clampWidth(500, 1200)).toBe(500);
    expect(panel.clampWidth(500, 600)).toBe(panel.PANEL_MIN);
  });

  test("suggested sites are recent ones first, then defaults, one per host", () => {
    let recent = panel.rememberSite([], "https://github.com/Omerfaruk-aydn/Atlas-Agent/pull/1", "Pull request");
    recent = panel.rememberSite(recent, "https://docs.example.com/guide", "Guide");
    expect(recent.map((x: { host: string }) => x.host)).toEqual(["docs.example.com", "github.com"]);
    const tiles = panel.suggestedSites(recent, 8);
    expect(tiles.map((x: { host: string }) => x.host)).toEqual(["docs.example.com", "github.com", "developer.mozilla.org", "en.wikipedia.org", "news.ycombinator.com"]);
    expect(tiles[1].title).toBe("Pull request");
    expect(panel.suggestedSites([], 2)).toHaveLength(2);
    expect(panel.rememberSite(recent, "javascript:alert(1)", "x")).toBe(recent);
  });

  test("the panel shortcuts are registered and do not collide", () => {
    const ids = ["rail", "rail1", "railTerminal", "railSide", "rail2", "summary"];
    const keys = ids.map((id) => SHORTCUTS.find((s) => s.id === id)?.keys[0]);
    expect(keys).toEqual(["Ctrl+Alt+B", "Ctrl+Shift+G", "Ctrl+`", "Ctrl+Shift+Y", "Ctrl+P", "Ctrl+Shift+U"]);
    const seen = new Map<string, string>();
    for (const s of SHORTCUTS.filter((x) => !x.info)) {
      for (const k of s.keys) {
        expect(seen.get(k), `${k} is bound twice`).toBeUndefined();
        seen.set(k, s.id);
      }
    }
  });
});

const requireCjs = createRequire(import.meta.url);
const { Vault, VaultError } = requireCjs("../main/vault.cjs");
const { originOf, sameOrigin } = requireCjs("../main/origin.cjs");
const { parseCsv, parsePasswordCsv } = requireCjs("../main/csv.cjs");
const { chordOf } = requireCjs("../main/chords.cjs");
const { pickCore } = requireCjs("../main/core-pick.cjs");
const { isPublicAddress } = requireCjs("../main/security.cjs");
const { HistoryStore, NotesStore, ZoomStore, pageKey } = requireCjs("../main/browser-data.cjs");
const cookieImport = requireCjs("../main/cookie-import.cjs");
const { summarize, signature } = requireCjs("../main/extensions.cjs");
const { Gateway } = requireCjs("../main/gateway.cjs");
const nodeCrypto = await import("node:crypto");
const nodeFs = await import("node:fs");
const nodeOs = await import("node:os");
const nodePath = await import("node:path");

const scratch = () => nodeFs.mkdtempSync(nodePath.join(nodeOs.tmpdir(), "atlas-unit-"));

// A stand-in for safeStorage: reversible, and visibly not plain text.
const fakeCrypt = (available = true) => ({
  isEncryptionAvailable: () => available,
  encryptString: (s: string) => Buffer.from(`enc:${Buffer.from(s).reverse().toString("base64")}`),
  decryptString: (b: Buffer) => Buffer.from(b.toString().slice(4), "base64").reverse().toString(),
});

describe("the password vault", () => {
  test("passwords reach the disk encrypted, and lists never carry them", () => {
    const file = nodePath.join(scratch(), "vault.json");
    const vault = new Vault(file, fakeCrypt());
    vault.add("https://example.test/login?x=1", "ada", "hunter2-fake");
    const raw = nodeFs.readFileSync(file, "utf8");
    expect(raw).not.toContain("hunter2-fake");
    expect(raw).toContain("https://example.test");
    const [entry] = vault.list();
    expect(entry).toMatchObject({ origin: "https://example.test", username: "ada" });
    expect(JSON.stringify(vault.list())).not.toContain("hunter2");
    expect(Object.keys(entry)).not.toContain("secret");
    expect(vault.reveal(entry.id)).toBe("hunter2-fake");
    // A second vault over the same file reads it back.
    expect(new Vault(file, fakeCrypt()).reveal(entry.id)).toBe("hunter2-fake");
  });

  test("without safe storage nothing is saved, and the reason is an error, not plain text", () => {
    const file = nodePath.join(scratch(), "vault.json");
    const vault = new Vault(file, fakeCrypt(false));
    expect(() => vault.add("https://example.test", "ada", "pw-fake")).toThrow(VaultError);
    try {
      vault.add("https://example.test", "ada", "pw-fake");
    } catch (err) {
      expect((err as { code: string }).code).toBe("unavailable");
      expect(String((err as Error).message)).not.toContain("pw-fake");
    }
    expect(nodeFs.existsSync(file)).toBe(false);
    expect(() => vault.addMany([{ origin: "https://example.test", username: "a", password: "b" }])).toThrow(VaultError);
  });

  test("a login is filled only on its exact origin", () => {
    const vault = new Vault(nodePath.join(scratch(), "vault.json"), fakeCrypt());
    const { id } = vault.add("https://example.test", "ada", "pw-fake");
    expect(vault.fill(id, "https://example.test/account")).toEqual({ username: "ada", password: "pw-fake" });
    for (const other of ["http://example.test/", "https://example.test.evil.test/", "https://evil.test/", "https://example.test:8443/", "https://sub.example.test/", "javascript:alert(1)", "https://user:pw@example.test/"]) {
      expect(vault.fill(id, other), other).toBeNull();
    }
    expect(vault.forOrigin("https://example.test/x")).toHaveLength(1);
    expect(vault.forOrigin("https://example.test:81/")).toHaveLength(0);
  });

  test("saving again updates, never duplicates; edit, delete, never-save and bulk add work", () => {
    const vault = new Vault(nodePath.join(scratch(), "vault.json"), fakeCrypt());
    const first = vault.add("https://a.test", "ada", "one-fake");
    const again = vault.add("https://a.test", "ada", "two-fake");
    expect(again).toEqual({ id: first.id, updated: true });
    expect(vault.list()).toHaveLength(1);
    expect(vault.reveal(first.id)).toBe("two-fake");
    expect(vault.isSaved("https://a.test", "ada", "two-fake")).toBe(true);
    expect(vault.isSaved("https://a.test", "ada", "other")).toBe(false);
    vault.update(first.id, { username: "grace", password: "three-fake" });
    expect(vault.list()[0].username).toBe("grace");
    expect(vault.reveal(first.id)).toBe("three-fake");
    vault.update(first.id, { username: "grace2" });
    expect(vault.reveal(first.id)).toBe("three-fake");
    expect(vault.list("grace2")).toHaveLength(1);
    expect(vault.list("nothing-like-this")).toHaveLength(0);
    expect(vault.neverSave("https://never.test")).toBe(false);
    vault.setNeverSave("https://never.test/page", true);
    expect(vault.neverSave("https://never.test")).toBe(true);
    const bulk = vault.addMany([
      { origin: "https://b.test", username: "x", password: "p1" },
      { origin: "https://a.test", username: "grace2", password: "p2" },
      { origin: "ftp://c.test", username: "y", password: "p3" },
      { origin: "https://d.test", username: "z", password: "" },
    ]);
    expect(bulk).toEqual({ saved: 1, replaced: 1, refused: 2 });
    // An import is one write, however many rows it has.
    const writes: string[] = [];
    const counted = new Vault(nodePath.join(scratch(), "counted.json"), fakeCrypt());
    const original = counted.save.bind(counted);
    counted.save = () => {
      writes.push("w");
      original();
    };
    counted.addMany(Array.from({ length: 40 }, (_, i) => ({ origin: `https://site${i}.test`, username: "u", password: `p${i}` })));
    expect(writes).toHaveLength(1);
    expect(counted.list()).toHaveLength(40);
    expect(vault.remove(first.id)).toBe(true);
    expect(vault.remove(first.id)).toBe(false);
    expect(() => vault.reveal(first.id)).toThrow(VaultError);
  });
});

describe("origins", () => {
  test("only exact http(s) origins match", () => {
    expect(originOf("https://Example.test/a?b#c")).toBe("https://example.test");
    expect(originOf("http://127.0.0.1:8080/x")).toBe("http://127.0.0.1:8080");
    for (const bad of ["", "not a url", "file:///C:/x", "javascript:1", "data:text/html,x", "https://u:p@example.test/"]) expect(originOf(bad), bad).toBe("");
    expect(sameOrigin("https://example.test/a", "https://example.test/b")).toBe(true);
    expect(sameOrigin("https://example.test", "http://example.test")).toBe(false);
    expect(sameOrigin("", "")).toBe(false);
  });
});

describe("password CSV", () => {
  test("RFC 4180 quoting, a BOM and CRLF", () => {
    const rows = parseCsv('\uFEFFa,b,c\r\n1,"two, with comma","line\nbreak ""quoted"""\r\n\r\n4,5,6');
    expect(rows).toEqual([["a", "b", "c"], ["1", "two, with comma", 'line\nbreak "quoted"'], ["4", "5", "6"]]);
  });

  test("Chrome and Edge exports", () => {
    const csv = ["name,url,username,password,note", 'Example,https://example.test/login,ada,"p,w-fake",', 'Dup,https://example.test/other,ada,"x",', "Bad,chrome://settings,ada,pw,", "Empty,https://empty.test,ada,,", '"Quoted",https://q.test,"u ""x""",pw2,"n"'].join("\r\n");
    const r = parsePasswordCsv(csv);
    expect(r.format).toBe("chromium");
    expect(r.entries).toEqual([
      { origin: "https://example.test", username: "ada", password: "p,w-fake" },
      { origin: "https://q.test", username: 'u "x"', password: "pw2" },
    ]);
    expect(r.skipped).toBe(3);
  });

  test("Firefox export and files that are not exports", () => {
    const csv = "url,username,password,httpRealm,formActionOrigin,guid,timeCreated,timeLastUsed,timePasswordChanged\nhttps://f.test,ff,secret-fake,,https://f.test,{g},1,2,3";
    const r = parsePasswordCsv(csv);
    expect(r.format).toBe("firefox");
    expect(r.entries).toEqual([{ origin: "https://f.test", username: "ff", password: "secret-fake" }]);
    expect(parsePasswordCsv("a,b\n1,2").format).toBe("");
    expect(parsePasswordCsv("").entries).toEqual([]);
  });
});

describe("cookies from other browsers", () => {
  const { DatabaseSync } = requireCjs("node:sqlite");

  test("Firefox cookies are read in plain text, containers left out", () => {
    const file = nodePath.join(scratch(), "cookies.sqlite");
    const db = new DatabaseSync(file);
    db.exec("CREATE TABLE moz_cookies (name TEXT, value TEXT, host TEXT, path TEXT, expiry INTEGER, isSecure INTEGER, isHttpOnly INTEGER, sameSite INTEGER, originAttributes TEXT)");
    const put = db.prepare("INSERT INTO moz_cookies VALUES (?,?,?,?,?,?,?,?,?)");
    put.run("sid", "fake-value", ".example.test", "/", 1893456000, 1, 1, 1, "");
    put.run("pref", "x", "example.test", "/a", 1893456000000, 0, 0, 0, "");
    put.run("ctx", "y", "example.test", "/", 0, 0, 0, 0, "^userContextId=2");
    db.close();
    const r = cookieImport.readFirefoxCookies(file);
    expect(r.appBound).toBe(0);
    expect(r.cookies.map((c: { name: string }) => c.name)).toEqual(["sid", "pref"]);
    expect(r.cookies[0]).toMatchObject({ host: ".example.test", value: "fake-value", secure: true, httpOnly: true, sameSite: "lax", expires: 1893456000 });
    expect(r.cookies[1].expires).toBe(1893456000);
  });

  test("Chromium: v10 values are decrypted with the user's key, v20 are counted and left alone", async () => {
    const dir = scratch();
    const key = nodeCrypto.randomBytes(32);
    const seal = (text: string) => {
      const nonce = nodeCrypto.randomBytes(12);
      const cipher = nodeCrypto.createCipheriv("aes-256-gcm", key, nonce);
      const body = Buffer.concat([cipher.update(text, "utf8"), cipher.final()]);
      return Buffer.concat([Buffer.from("v10"), nonce, body, cipher.getAuthTag()]);
    };
    const localState = nodePath.join(dir, "Local State");
    nodeFs.writeFileSync(localState, JSON.stringify({ os_crypt: { encrypted_key: Buffer.concat([Buffer.from("DPAPI"), Buffer.from("wrapped-fake")]).toString("base64") } }));
    const file = nodePath.join(dir, "Cookies");
    const db = new DatabaseSync(file);
    db.exec("CREATE TABLE cookies (host_key TEXT, name TEXT, value TEXT, encrypted_value BLOB, path TEXT, expires_utc INTEGER, is_secure INTEGER, is_httponly INTEGER, samesite INTEGER)");
    const put = db.prepare("INSERT INTO cookies VALUES (?,?,?,?,?,?,?,?,?)");
    const expires = (1893456000 + 11_644_473_600) * 1_000_000;
    put.run(".a.test", "enc", "", seal("secret-fake"), "/", expires, 1, 1, 2);
    put.run("b.test", "plain", "visible", Buffer.alloc(0), "/", 0, 0, 0, -1);
    put.run("c.test", "bound", "", Buffer.concat([Buffer.from("v20"), nodeCrypto.randomBytes(40)]), "/", 0, 1, 0, 0);
    const broken = seal("x");
    broken[20] ^= 1;
    put.run("d.test", "broken", "", broken, "/", 0, 0, 0, 0);
    db.close();
    let asked = 0;
    const r = await cookieImport.readChromiumCookies(
      { cookies: file, localState },
      {
        unprotect: async (blob: Buffer) => {
          asked++;
          expect(blob.toString()).toBe("wrapped-fake");
          return key;
        },
      },
    );
    expect(asked).toBe(1);
    expect(r.appBound).toBe(1);
    expect(r.failed).toBe(1);
    expect(r.cookies.map((c: { name: string; value: string }) => [c.name, c.value])).toEqual([["enc", "secret-fake"], ["plain", "visible"]]);
    expect(r.cookies[0]).toMatchObject({ expires: 1893456000, sameSite: "strict", secure: true });
    // When the key cannot be had, encrypted values are failures, not guesses.
    const none = await cookieImport.readChromiumCookies(
      { cookies: file, localState },
      {
        unprotect: async () => {
          throw new Error("no");
        },
      },
    );
    expect(none.cookies.map((c: { name: string }) => c.name)).toEqual(["plain"]);
    expect(none.failed).toBe(2);
    expect(none.appBound).toBe(1);
  });

  test("profiles are found from the folders the browsers use", () => {
    const env = scratch();
    const appdata = nodePath.join(env, "Roaming");
    const local = nodePath.join(env, "Local");
    const ff = nodePath.join(appdata, "Mozilla", "Firefox", "Profiles", "abc.default");
    nodeFs.mkdirSync(ff, { recursive: true });
    nodeFs.writeFileSync(nodePath.join(ff, "cookies.sqlite"), "");
    nodeFs.writeFileSync(nodePath.join(appdata, "Mozilla", "Firefox", "profiles.ini"), "[Profile0]\nName=default\nIsRelative=1\nPath=Profiles/abc.default\n");
    const edge = nodePath.join(local, "Microsoft", "Edge", "User Data");
    nodeFs.mkdirSync(nodePath.join(edge, "Default", "Network"), { recursive: true });
    nodeFs.writeFileSync(nodePath.join(edge, "Default", "Network", "Cookies"), "");
    nodeFs.writeFileSync(nodePath.join(edge, "Local State"), JSON.stringify({ profile: { info_cache: { Default: { name: "Person 1" } } } }));
    const found = cookieImport.listProfiles({ APPDATA: appdata, LOCALAPPDATA: local });
    expect(found.map((p: { browser: string; name: string }) => `${p.browser}:${p.name}`)).toEqual(["firefox:default", "edge:Person 1"]);
    expect(cookieImport.listProfiles({})).toEqual([]);
  });
});

describe("browser keys", () => {
  const press = (key: string, code: string, mods: Record<string, boolean> = {}) => chordOf({ key, code, ...mods });

  test("chords are named as the renderer names them", () => {
    expect(press("b", "KeyB", { control: true, alt: true })).toBe("Ctrl+Alt+B");
    expect(press("G", "KeyG", { control: true, shift: true })).toBe("Ctrl+Shift+G");
    expect(press("`", "Backquote", { control: true })).toBe("Ctrl+`");
    expect(press("1", "Digit1", { control: true, alt: true })).toBe("Ctrl+Alt+1");
    expect(press("a", "KeyA")).toBe("");
  });

  test("AltGr characters are text, never chords", () => {
    // Turkish Q layout: AltGr+Q is @, AltGr+E is a euro sign, AltGr+7 is {, AltGr+8 is [.
    expect(press("@", "KeyQ", { control: true, alt: true })).toBe("");
    expect(press("\u20AC", "KeyE", { control: true, alt: true })).toBe("");
    expect(press("{", "Digit7", { control: true, alt: true })).toBe("");
    expect(press("[", "Digit8", { control: true, alt: true })).toBe("");
    expect(press("\\", "KeyQ", { control: true, alt: true })).toBe("");
    expect(press("~", "Backslash", { control: true, alt: true })).toBe("");
  });

  test("the browser view passes on only registered shortcuts: editing keys stay with the page", () => {
    const registered = new Set(allChords({}));
    expect(registered.has("Ctrl+Alt+B")).toBe(true);
    const cases: [string, string, Record<string, boolean>][] = [
      ["ArrowLeft", "ArrowLeft", { control: true }],
      ["Backspace", "Backspace", { control: true }],
      ["Z", "KeyZ", { control: true, shift: true }],
      ["V", "KeyV", { control: true, shift: true }],
      ["c", "KeyC", { control: true }],
      ["@", "KeyQ", { control: true, alt: true }],
    ];
    for (const [key, code, mods] of cases) expect(registered.has(press(key, code, mods)), `${key} ${JSON.stringify(mods)}`).toBe(false);
  });
});

describe("the terminal and the app's chords", () => {
  test("plain Ctrl chords stay with the shell; Alt, Shift and Ctrl+` go to the app", () => {
    const { terminalLeavesToApp } = shortcutsModule;
    expect(terminalLeavesToApp({ key: "p", altKey: false, shiftKey: false })).toBe(false);
    expect(terminalLeavesToApp({ key: "l", altKey: false, shiftKey: false })).toBe(false);
    expect(terminalLeavesToApp({ key: "`", altKey: false, shiftKey: false })).toBe(true);
    expect(terminalLeavesToApp({ key: "s", altKey: true, shiftKey: false })).toBe(true);
    expect(terminalLeavesToApp({ key: "G", altKey: false, shiftKey: true })).toBe(true);
  });
});

describe("the core the app starts", () => {
  const files = (list: Record<string, number>) => ({
    readdirSync: () => Object.keys(list),
    statSync: (p: string) => ({ isFile: () => true, mtimeMs: list[nodePath.basename(p)] }),
  });
  test("the exact name wins; otherwise the newest build, never one moved aside", () => {
    expect(nodePath.basename(pickCore("C:/bin", files({ "atlas-agent-old-dev.exe": 1, "atlas-agent.exe": 0, "atlas-agent-test.exe": 9 })))).toBe("atlas-agent.exe");
    expect(nodePath.basename(pickCore("C:/bin", files({ "atlas-agent-capability-complete-dev.exe": 1, "atlas-agent-test.exe": 9, "atlas-agent-test.old123.exe": 99, "atlas-agent-web-dev.exe": 5 })))).toBe("atlas-agent-test.exe");
    expect(pickCore("C:/bin", files({ "other.exe": 1 }))).toBe("");
    expect(
      pickCore("C:/missing", {
        readdirSync: () => {
          throw new Error("no");
        },
        statSync: () => ({}),
      }),
    ).toBe("");
    // The same names in another order give the same answer.
    const a = pickCore("C:/bin", files({ "atlas-agent-b.exe": 5, "atlas-agent-a.exe": 5 }));
    const b = pickCore("C:/bin", files({ "atlas-agent-a.exe": 5, "atlas-agent-b.exe": 5 }));
    expect(a).toBe(b);
  });

  test("a core that does not answer ends in an error, and is stopped", async () => {
    const gateway = new Gateway({ bin: process.execPath, args: ["-e", "setTimeout(() => {}, 30000)"], renderer: "", timeout: 400, onCrash() {} });
    await expect(gateway.start()).rejects.toThrow(/did not start in time/);
    await new Promise((r) => setTimeout(r, 300));
    expect(gateway.child === null || gateway.child.killed).toBe(true);
  });
});

describe("what a page may make the app fetch", () => {
  test("only public addresses", () => {
    for (const ok of ["8.8.8.8", "93.184.216.34", "2606:4700:4700::1111"]) expect(isPublicAddress(ok), ok).toBe(true);
    for (const bad of ["127.0.0.1", "10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.1.1", "169.254.169.254", "0.0.0.0", "100.64.0.1", "::1", "::", "fe80::1", "fd00::1", "::ffff:127.0.0.1", "::ffff:10.0.0.1", "224.0.0.1", "not an ip"]) expect(isPublicAddress(bad), bad).toBe(false);
  });
});

describe("what the browser remembers", () => {
  test("history: recorded, searched, removed, cleared by time", () => {
    const file = nodePath.join(scratch(), "history.json");
    const h = new HistoryStore(file);
    h.record("https://a.test/page#frag", "A page", 1000);
    h.record("https://a.test/page#other", "A page", 1500);
    h.record("https://b.test/", "B", 10_000);
    h.record("file:///C:/secret", "No", 11_000);
    h.record("javascript:1", "No", 11_000);
    expect(h.search("").map((e: { url: string }) => e.url)).toEqual(["https://b.test/", "https://a.test/page"]);
    h.retitle("https://b.test/", "B retitled");
    expect(h.search("retitled")).toHaveLength(1);
    expect(h.search("a.test")).toHaveLength(1);
    expect(h.clear(5000)).toBe(1);
    expect(h.search("")).toHaveLength(1);
    const id = h.search("")[0].id;
    expect(h.remove(id)).toBe(true);
    expect(h.remove(id)).toBe(false);
    h.record("https://c.test/", "C", 20_000);
    h.flush();
    expect(new HistoryStore(file).search("")).toHaveLength(1);
    expect(h.clear(0)).toBe(1);
  });

  test("notes belong to a page, ignoring its anchor, and can be edited and deleted", () => {
    const n = new NotesStore(nodePath.join(scratch(), "notes.json"));
    const note = n.add("https://a.test/doc#intro", "  remember this  ", "quoted text");
    expect(note).toMatchObject({ text: "remember this", quote: "quoted text" });
    expect(n.list("https://a.test/doc#other")).toHaveLength(1);
    expect(n.list("https://a.test/doc2")).toHaveLength(0);
    expect(n.add("https://a.test/doc", "   ")).toBeNull();
    expect(n.add("file:///x", "no")).toBeNull();
    expect(n.update("https://a.test/doc", note.id, "changed")?.text).toBe("changed");
    expect(n.all()).toHaveLength(1);
    expect(n.remove("https://a.test/doc", note.id)).toBe(true);
    expect(n.list("https://a.test/doc")).toHaveLength(0);
    expect(pageKey("https://a.test/x#y")).toBe("https://a.test/x");
  });

  test("zoom is kept per site and 100% forgets the site", () => {
    const z = new ZoomStore(nodePath.join(scratch(), "zoom.json"));
    z.set("https://a.test:8443/x", 1.5);
    expect(z.get("https://a.test:8443/other")).toBe(1.5);
    expect(z.get("https://a.test/")).toBe(1);
    z.set("https://a.test:8443/x", 1);
    expect(z.get("https://a.test:8443/")).toBe(1);
  });
});

describe("extensions", () => {
  test("what an extension asks for is summarized, and a changed manifest no longer matches what was confirmed", () => {
    const manifest = { manifest_version: 3, name: "T", version: "1.0", permissions: ["storage", "tabs", "https://x.test/*"], host_permissions: ["https://y.test/*"], content_scripts: [{ matches: ["https://z.test/*"] }] };
    const s = summarize(manifest);
    expect(s.permissions).toEqual(["storage", "tabs"]);
    expect([...s.hostPermissions].sort()).toEqual(["https://x.test/*", "https://y.test/*", "https://z.test/*"]);
    const sig = signature(manifest);
    expect(signature({ ...manifest })).toBe(sig);
    expect(signature({ ...manifest, permissions: [...manifest.permissions, "cookies"] })).not.toBe(sig);
    expect(signature({ ...manifest, version: "1.1" })).not.toBe(sig);
  });
});

describe("AltGr in the renderer", () => {
  const ev = (key: string, code: string, mods: Partial<KeyboardEvent> & { altGraph?: boolean } = {}) =>
    ({ key, code, ctrlKey: false, altKey: false, shiftKey: false, metaKey: false, getModifierState: (k: string) => k === "AltGraph" && !!mods.altGraph, ...mods }) as unknown as KeyboardEvent;

  test("AltGr characters on a Turkish layout are text, never chords", async () => {
    const { chord, canBind, isAltGrText } = await import("../renderer/src/lib/shortcuts");
    // On Windows AltGr is Ctrl+Alt, with the typed character in `key`.
    const presses: [string, string][] = [["ß", "KeyS"], ["ı", "KeyI"], ["#", "Digit3"], ["@", "KeyQ"], ["€", "KeyE"], ["{", "Digit7"], ["[", "Digit8"], ["\\", "KeyQ"], ["~", "Digit1"]];
    for (const [key, code] of presses) {
      const e = ev(key, code, { ctrlKey: true, altKey: true });
      expect(isAltGrText(e), `${key} ${code}`).toBe(true);
      expect(chord(e), `${key} ${code}`).toBe("");
      expect(canBind(e), `${key} ${code}`).toBe(false);
    }
    // A layout that reports the modifier also counts, whatever the key says.
    expect(chord(ev("s", "KeyS", { ctrlKey: true, altKey: true, altGraph: true }))).toBe("");
  });

  test("real chords are still chords, and no default clashes with AltGr", async () => {
    const { chord, SHORTCUTS } = await import("../renderer/src/lib/shortcuts");
    expect(chord(ev("b", "KeyB", { ctrlKey: true, altKey: true }))).toBe("Ctrl+Alt+B");
    expect(chord(ev("Y", "KeyY", { ctrlKey: true, shiftKey: true }))).toBe("Ctrl+Shift+Y");
    expect(chord(ev("?", "Slash", { ctrlKey: true, shiftKey: true }))).toBe("Ctrl+Shift+/");
    // The only Ctrl+Alt default left types nothing under AltGr on the Turkish layout.
    const withAlt = SHORTCUTS.filter((s) => !s.info && s.keys.some((k) => k.includes("Alt+") && !k.startsWith("Alt+")));
    expect(withAlt.map((s) => s.keys[0])).toEqual(["Ctrl+Alt+B"]);
  });
});

describe("what the browser keeps survives a kill", () => {
  test("a zoom, a note and a deleted history entry are on disk the moment they change", () => {
    const dir = scratch();
    const zoomFile = nodePath.join(dir, "zoom.json");
    new ZoomStore(zoomFile).set("https://example.com/a", 1.25);
    expect(JSON.parse(nodeFs.readFileSync(zoomFile, "utf8")).zoom["example.com"]).toBe(1.25);

    const notesFile = nodePath.join(dir, "notes.json");
    const notes = new NotesStore(notesFile);
    const note = notes.add("https://example.com/a", "remember", "");
    expect(nodeFs.readFileSync(notesFile, "utf8")).toContain("remember");
    notes.remove("https://example.com/a", note.id);
    expect(nodeFs.readFileSync(notesFile, "utf8")).not.toContain("remember");

    const historyFile = nodePath.join(dir, "history.json");
    const history = new HistoryStore(historyFile);
    history.record("https://example.com/p", "P");
    // A visit is the one thing that waits a moment; flush (quit) writes it.
    expect(nodeFs.existsSync(historyFile)).toBe(false);
    history.flush();
    expect(nodeFs.readFileSync(historyFile, "utf8")).toContain("example.com/p");
    history.clear?.();
    expect(JSON.parse(nodeFs.readFileSync(historyFile, "utf8")).entries).toHaveLength(0);
  });
});

describe("login offers", () => {
  test("a password is forgotten when answered, when old, and when its tab closes", async () => {
    const { OfferBook } = requireCjs("../main/browser-ipc.cjs");
    const book = new OfferBook(40);
    book.add("a", { origin: "https://x.test", username: "u", password: "fixture-pw", tab: "t1" });
    book.add("b", { origin: "https://y.test", username: "u", password: "fixture-pw", tab: "t2" });
    book.add("c", { origin: "https://z.test", username: "u", password: "fixture-pw", tab: "t2" });
    book.dropTab("t2");
    expect(book.size).toBe(1);
    const taken = book.take("a");
    expect(taken.password).toBe("fixture-pw");
    expect(book.take("a")).toBeNull();
    book.add("d", { origin: "https://x.test", username: "u", password: "fixture-pw", tab: "t3" });
    await new Promise((r) => setTimeout(r, 120));
    expect(book.size).toBe(0);
    expect(book.take("d")).toBeNull();
  });
});

describe("risky downloads", () => {
  test("everything that runs code or opens a program is shown in its folder, never opened", () => {
    const { isRiskyFile } = requireCjs("../main/browser.cjs");
    for (const name of ["a.exe", "a.URL", "help.chm", "m.docm", "m.xlsm", "disk.iso", "x.application", "invoice.pdf.exe", "run.ps1", "a.lnk", "s.scr", "t.xlam", "p.msix", "sneaky.exe.", "sneaky.bat  ", "x.hta", "x.jnlp", "x.settingcontent-ms"]) {
      expect(isRiskyFile(name), name).toBe(true);
    }
    for (const name of ["notes.txt", "photo.png", "report.pdf", "data.csv", "paper.docx", "sheet.xlsx", "archive.zip", "exe", "my.exe.txt"]) {
      expect(isRiskyFile(name), name).toBe(false);
    }
  });
});

describe("the search engine of the address bar", () => {
  test("an address that is not a page goes to the engine chosen in the settings", () => {
    panel.setSearchEngine("google");
    expect(panel.normalizeAddress("atlas coding agent")).toBe("https://www.google.com/search?q=atlas%20coding%20agent");
    panel.setSearchEngine("bing");
    expect(panel.normalizeAddress("atlas")).toBe("https://www.bing.com/search?q=atlas");
    panel.setSearchEngine("nonsense");
    expect(panel.normalizeAddress("atlas")).toBe("https://duckduckgo.com/?q=atlas");
    // Pages stay pages, whatever the engine.
    expect(panel.normalizeAddress("example.com")).toBe("https://example.com/");
  });

  test("the settings store checks every value", () => {
    const { SettingsStore } = requireCjs("../main/browser-data.cjs");
    const store = new SettingsStore(nodePath.join(scratch(), "settings.json"));
    expect(store.get()).toEqual({ searchEngine: "duckduckgo", offerSave: true, addressAutofill: true });
    expect(store.set("searchEngine", "google")).toBe(true);
    expect(store.set("searchEngine", "javascript:1")).toBe(false);
    expect(store.set("offerSave", "yes")).toBe(false);
    expect(store.set("offerSave", false)).toBe(true);
    expect(store.set("other", true)).toBe(false);
    expect(new SettingsStore(nodePath.join(nodePath.dirname(store.store.file), "settings.json")).get()).toEqual({ searchEngine: "google", offerSave: false, addressAutofill: true });
  });

  test("addresses are stored encrypted and only known fields are kept", () => {
    const { AddressBook } = requireCjs("../main/addresses.cjs");
    const file = nodePath.join(scratch(), "addresses.json");
    const book = new AddressBook(file, fakeCrypt());
    const saved = book.save_("", { name: "Ada Fixture", city: "Testville", evil: "x".repeat(10) });
    expect(nodeFs.readFileSync(file, "utf8")).not.toContain("Ada");
    expect(book.get(saved.id).name).toBe("Ada Fixture");
    expect(Object.keys(book.get(saved.id)).sort()).toEqual(["city", "country", "email", "id", "name", "organization", "phone", "postal", "region", "street", "street2", "updated"]);
    expect(() => book.save_("", {})).toThrow();
    expect(() => new AddressBook(file, fakeCrypt(false)).save_("", { name: "x" })).toThrow();
    expect(book.remove(saved.id)).toBe(true);
    expect(book.list()).toHaveLength(0);
  });
});

describe("streaming pace", () => {
  test("a short backlog moves a few characters, a long one catches up quickly", () => {
    expect(stepSize(0)).toBe(0);
    expect(stepSize(5)).toBe(2);
    expect(stepSize(2800)).toBeGreaterThanOrEqual(100);
  });

  test("a cut stops at the end of a word", () => {
    const text = "The quick brown fox jumps";
    expect(wordEnd(text, 6, 6)).toBe(9);
    expect(nextCut(text, 0)).toBeLessThanOrEqual(text.length);
    expect(nextCut(text, text.length)).toBe(text.length);
  });

  test("a cut never lands inside a link target or an open code span", () => {
    const link = "See [the docs](https://example.com/a/very/long/path) now";
    const inside = link.indexOf("https") + 4;
    const cut = wordEnd(link, inside, inside);
    expect(cut === link.length || link.slice(0, cut).split("](").length === 1 || link.slice(0, cut).includes(")")).toBe(true);
    const code = "Run `npm run build --watch` today";
    const open = code.indexOf("npm") + 1;
    expect(wordEnd(code, open, open)).toBeGreaterThanOrEqual(code.indexOf("watch` ") + 6);
  });

  test("the cursor always reaches the end", () => {
    const text = "word ".repeat(200);
    let at = 0;
    for (let i = 0; i < 2000 && at < text.length; i++) at = nextCut(text, at);
    expect(at).toBe(text.length);
  });
});

describe("chat extras", () => {
  test("plain text drops markdown syntax but keeps the words", () => {
    expect(plainText("## Title\n\nSome **bold** and `code` with [a link](https://x.y).")).toBe("Title\n\nSome bold and code with a link.");
    expect(plainText("```js\nconst a = 1;\n```")).toBe("const a = 1;");
  });

  test("queue items move to the place they are dropped", () => {
    expect(moveItem(["a", "b", "c", "d"], 0, 2)).toEqual(["b", "c", "a", "d"]);
    expect(moveItem(["a", "b", "c"], 2, 0)).toEqual(["c", "a", "b"]);
    expect(moveItem(["a", "b"], 1, 1)).toEqual(["a", "b"]);
    expect(dropIndex(0, 3)).toBe(2);
    expect(dropIndex(3, 1)).toBe(1);
  });

  test("a permission request reads as one question", () => {
    const run = permissionSentence({ id: "p1", session_id: "s", tool_call_id: "c", tool_name: "bash", description: "", action: "execute", path: "", params: { command: "npm test" } } as never);
    expect(run.detail).toBe("npm test");
    const edit = permissionSentence({ id: "p2", session_id: "s", tool_call_id: "c", tool_name: "edit", description: "", action: "write", path: "/work/src/app.ts", params: { file_path: "/work/src/app.ts" } } as never);
    expect(edit.title).toContain("app.ts");
  });

  test("file edits keep the first old text and the last new text per file", () => {
    const call = (id: string, path: string) => ({ id, name: "edit", input: JSON.stringify({ file_path: path }), finished: true });
    const result = (id: string, before: string, after: string) => ({ tool_call_id: id, name: "edit", content: "ok", is_error: false, metadata: JSON.stringify({ old_content: before, new_content: after, additions: 1, removals: 1 }) });
    const steps = [
      { id: "1", kind: "tool", call: call("1", "/w/a.ts"), result: result("1", "one", "two") },
      { id: "2", kind: "tool", call: call("2", "/w/a.ts"), result: result("2", "two", "three") },
    ] as never;
    const [file] = fileEdits(steps);
    expect(file?.before).toBe("one");
    expect(file?.after).toBe("three");
    expect(file?.additions).toBe(2);
    expect(file?.undoable).toBe(true);
  });
});

describe("motion stylesheets", () => {
  const css = fs.readFileSync(new URL("../renderer/src/design/motion.css", import.meta.url), "utf8");
  test("a hidden window pauses every animation", () => {
    expect(css).toMatch(/:root\[data-hidden\][\s\S]*animation-play-state: paused/);
  });
  test("reduced motion keeps a calm opacity pulse for things that are working", () => {
    expect(css).toMatch(/data-motion="reduce"\] \.shimmer[\s\S]*calm-pulse/);
  });
  test("the shimmer is one element with one gradient", () => {
    expect(css).toMatch(/\.shimmer \{[^}]*background-clip: text/);
    expect(css).not.toMatch(/shimmer-char/);
  });
});

describe("review findings", () => {
  const high = { severity: "high" as const, title: "Empty input panics", file: "src/app.ts", line: 7, explanation: "An empty list indexes element 0.", suggestion: "Check the length." };
  const low = { severity: "low" as const, title: "Unclear name", file: "src/app.ts" };
  const info = { severity: "info" as const, title: "Note" };

  test("severities rank worst first and unknown ones last", () => {
    expect(severityRank("critical")).toBeLessThan(severityRank("high"));
    expect(severityRank("low")).toBeLessThan(severityRank("info"));
    expect(severityRank("odd" as never)).toBeGreaterThan(severityRank("info"));
  });

  test("a location reads as file and line, the file alone, or nothing", () => {
    expect(locationOf(high)).toBe("src/app.ts:7");
    expect(locationOf(low)).toBe("src/app.ts");
    expect(locationOf(info)).toBe("");
  });

  test("a finding keeps its identity across reloads, and differs from others", () => {
    expect(findingKey(high)).toBe(findingKey({ ...high }));
    expect(findingKey(high)).not.toBe(findingKey({ ...high, line: 8 }));
    expect(findingKey(high)).not.toBe(findingKey(low));
  });

  test("counts are per severity, worst first, and skip empty ones", () => {
    expect(countBySeverity([low, high, low, info])).toEqual([
      { severity: "high", count: 1 },
      { severity: "low", count: 2 },
      { severity: "info", count: 1 },
    ]);
    expect(countBySeverity([])).toEqual([]);
  });

  test("a fix request carries the problem, the place and the suggestion, and invites disagreement", () => {
    const one = fixPrompt(high);
    expect(one).toContain("[high] Empty input panics");
    expect(one).toContain("src/app.ts:7");
    expect(one).toContain("An empty list indexes element 0.");
    expect(one).toContain("Check the length.");
    expect(one).toMatch(/If you disagree/);
    const many = fixAllPrompt([low, high]);
    expect(many.indexOf("Empty input panics")).toBeLessThan(many.indexOf("Unclear name"));
    expect(many).toContain("1. [high]");
    expect(many).toContain("2. [low]");
  });

  test("what the person decided is remembered per turn and the oldest turns are forgotten", () => {
    const store = new Map<string, string>();
    vi.stubGlobal("localStorage", { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => void store.set(k, v) });
    expect(loadMemory("t1")).toEqual(emptyMemory());
    saveMemory("t1", { ...emptyMemory(), dismissed: ["a"], sent: ["b"] });
    expect(loadMemory("t1").dismissed).toEqual(["a"]);
    for (let i = 0; i < 60; i++) saveMemory(`turn${i}`, emptyMemory());
    expect(loadMemory("t1")).toEqual(emptyMemory());
    expect(Object.keys(JSON.parse(store.get("atlas:reviews") ?? "{}"))).toHaveLength(40);
    vi.stubGlobal("localStorage", { getItem: () => { throw new Error("blocked"); }, setItem: () => { throw new Error("blocked"); } });
    expect(loadMemory("x")).toEqual(emptyMemory());
    expect(() => saveMemory("x", emptyMemory())).not.toThrow();
  });
});

describe("a sub-agent's work", () => {
  const message = (id: string, role: string, parts: unknown[]) => ({ id, session_id: "child", role, parts, model: "", provider: "", created_at: 1, updated_at: 1 }) as never;
  const call = (id: string, name: string, input: object) => ({ type: "tool_call", data: { id, name, input: JSON.stringify(input), finished: true } });
  const result = (id: string, name: string) => ({ type: "tool_result", data: { tool_call_id: id, name, content: "ok", metadata: "", is_error: false } });

  test("its session is named after the message and the call that started it", () => {
    expect(agentSessionId("a1:3", "call_9")).toBe("a1$$call_9");
    expect(agentSessionId("a1", "c")).toBe("a1$$c");
  });

  test("its steps are the main chat's steps without the task it was given", () => {
    expect(feedOf(undefined)).toEqual([]);
    expect(feedOf([])).toEqual([]);
    const steps = feedOf([
      message("u", "user", [{ type: "text", data: { text: "Look in the docs" } }]),
      message("a", "assistant", [call("v1", "view", { file_path: "docs/intro.md" }), result("v1", "view"), { type: "text", data: { text: "Found it." } }]),
    ]);
    expect(steps.map((s) => s.kind)).toEqual(["tool", "text"]);
    expect(stepCount(steps)).toBe(1);
  });

  test("the newest call reads as what it is doing now, then as what it did", () => {
    const running = feedOf([message("a", "assistant", [call("v1", "view", { file_path: "docs/intro.md" }), result("v1", "view"), call("g1", "grep", { pattern: "needle" })])]);
    expect(stepCount(running)).toBe(2);
    expect(latestActivity(running)).toMatch(/needle/);
    const done = feedOf([message("a", "assistant", [call("g1", "grep", { pattern: "needle" }), result("g1", "grep")])]);
    expect(latestActivity(done)).not.toBe(latestActivity(running));
    expect(latestActivity([])).toBe("");
  });

  test("parallel calls count one by one", () => {
    const steps = feedOf([message("a", "assistant", [call("a", "view", { file_path: "a.go" }), call("b", "view", { file_path: "b.go" }), call("c", "view", { file_path: "c.go" })])]);
    expect(steps).toHaveLength(1);
    expect(stepCount(steps)).toBe(3);
  });
});

describe("a sub-agent started in the background", () => {
  test("is told apart from one that finished", async () => {
    const { startedInBackground } = await import("../renderer/src/lib/subagent");
    expect(startedInBackground("Started in the background as agent-1a2b3c4d. Carry on")).toBe(true);
    expect(startedInBackground("The review found two problems.")).toBe(false);
    expect(startedInBackground(undefined)).toBe(false);
  });
});

describe("what a finished sub-agent run reports", () => {
  test("reads the run out of a result's metadata", () => {
    const metadata = JSON.stringify({ run: { duration_ms: 65_400, cost_usd: 0.02, files_changed: ["a.go", "b.go"] } });
    expect(runReport(metadata)).toEqual({ durationMs: 65_400, costUsd: 0.02, filesChanged: 2 });
  });

  test("is null when there is nothing readable", () => {
    expect(runReport(undefined)).toBeNull();
    expect(runReport("")).toBeNull();
    expect(runReport("not json")).toBeNull();
    expect(runReport(JSON.stringify({ routed_to: "research" }))).toBeNull();
  });

  test("writes a duration the way a person reads it", () => {
    expect(runDuration(400)).toBe("");
    expect(runDuration(12_000)).toBe("12s");
    expect(runDuration(65_400)).toBe("1m 05s");
  });
});

// ---------------------------------------------------------------------------
// The design tool.

const designModel = await import("../renderer/src/features/design/model");
const designGeo = await import("../renderer/src/features/design/geometry");
const designOps = await import("../renderer/src/features/design/ops");
const designText = await import("../renderer/src/features/design/text");
const designExport = await import("../renderer/src/features/design/export");
const designRender = await import("../renderer/src/features/design/render");
const { arithmetic } = await import("../renderer/src/features/design/arith");
const { designTarget, newDesignTargets } = await import("../renderer/src/lib/design-follow");

designText.setMeasure(designText.estimate);

type DNode = import("../renderer/src/features/design/model").DesignNode;
type DDoc = import("../renderer/src/features/design/model").DesignDoc;

const designSample = (): DDoc => designModel.sanitizeDoc(JSON.parse(fs.readFileSync(new URL("./fixtures/design-sample.json", import.meta.url), "utf8")));

function docOf(children: Partial<DNode>[]): DDoc {
  return designModel.sanitizeDoc({ name: "T", pages: [{ id: "p", name: "Page 1", children }] });
}

const nodeIn = (doc: DDoc, id: string) => designModel.findNode(doc, id)!.at.node;
const near = (a: number, b: number) => expect(Math.abs(a - b)).toBeLessThan(1e-6);

describe("design documents", () => {
  test("read the shared format the agent writes, and drop what they do not know", () => {
    const doc = designSample();
    expect(designModel.countNodes(doc)).toBe(7);
    const title = nodeIn(doc, "title");
    expect(title.line_height).toBe(76.8);
    expect(title.letter_spacing).toBe(-1);
    expect(title.auto_resize).toBe("width");
    expect(nodeIn(doc, "logo").view_box).toEqual([24, 24]);
    expect(nodeIn(doc, "card-1").component_id).toBe("card");
    expect(nodeIn(doc, "cta").radius).toEqual([12, 12, 12, 12]);
    // Sanitizing again changes nothing.
    expect(designModel.sanitizeDoc(JSON.parse(JSON.stringify(doc)))).toEqual(doc);
  });

  test("repair bad input instead of failing", () => {
    const doc = designModel.sanitizeDoc({ pages: [{ children: [{ id: "a", type: "rect", width: -4, fills: [{ color: "red" }, { color: "#abc" }] }, { id: "a", type: "text" }, { type: "nope" }] }] });
    const [rect, text] = doc.pages[0]!.children;
    expect(doc.pages[0]!.children).toHaveLength(2);
    expect(rect!.width).toBe(0);
    expect(rect!.fills).toEqual([{ type: "solid", color: "#ABC" }]);
    expect(text!.id).not.toBe("a");
    expect(text!.font).toEqual({ family: "Inter", size: 16, weight: 400 });
    expect(designModel.sanitizeDoc(null).pages).toHaveLength(1);
  });

  test("edits return new documents and keep untouched nodes as they were", () => {
    const doc = docOf([{ id: "a", type: "rect" }, { id: "f", type: "frame", children: [{ id: "b", type: "rect" } as DNode] }]);
    const next = designModel.patchNodes(doc, ["b"], { x: 5 });
    expect(nodeIn(next, "b").x).toBe(5);
    expect(nodeIn(doc, "b").x).toBe(0);
    expect(next.pages[0]!.children[0]).toBe(doc.pages[0]!.children[0]);
    const removed = designModel.removeNodes(next, ["a"]);
    expect(designModel.findNode(removed, "a")).toBeNull();
    const inserted = designModel.insertNodes(removed, "p", "f", [designModel.createNode("ellipse", { id: "e" })], 0);
    expect(nodeIn(inserted, "f").children!.map((c) => c.id)).toEqual(["e", "b"]);
  });

  test("name files the way the store accepts", () => {
    expect(designModel.fileSlug("Açılış Sayfası!")).toBe("acilis-sayfasi");
    expect(designModel.fileSlug("   ")).toBe("untitled");
    expect(designModel.nextName(docOf([{ type: "frame", name: "Frame 2" }]).pages[0]!, "Frame")).toBe("Frame 3");
  });
});

describe("design geometry", () => {
  test("places rotated and nested nodes on the page", () => {
    const doc = docOf([{ id: "f", type: "frame", x: 100, y: 100, width: 200, height: 200, children: [{ id: "r", type: "rect", x: 10, y: 20, width: 40, height: 20, rotation: 90 } as DNode] }]);
    const box = designGeo.nodeBounds(doc.pages[0]!, "r")!;
    // Turned a quarter about its center (130, 130): 20 wide, 40 tall.
    near(box.x, 120);
    near(box.y, 110);
    near(box.width, 20);
    near(box.height, 40);
  });

  test("hits the topmost node, respects clipping, and selects through artboards", () => {
    const doc = docOf([
      { id: "f", type: "frame", width: 100, height: 100, children: [{ id: "inner", type: "rect", x: 10, y: 10, width: 20, height: 20 } as DNode, { id: "out", type: "rect", x: 150, y: 10, width: 20, height: 20 } as DNode] },
      { id: "top", type: "rect", x: 50, y: 50, width: 100, height: 100 },
    ]);
    const page = doc.pages[0]!;
    expect(designGeo.hitPath(page, { x: 60, y: 60 })!.at(-1)!.id).toBe("top");
    const inner = designGeo.hitPath(page, { x: 15, y: 15 })!;
    expect(inner.map((n) => n.id)).toEqual(["f", "inner"]);
    expect(designGeo.pickTarget(inner, [], false).id).toBe("inner");
    expect(designGeo.hitPath(page, { x: 160, y: 15 })).toBeNull();
    expect(designGeo.marquee(page, { x: 0, y: 0, width: 40, height: 40 })).toEqual(["inner"]);
  });

  test("a group is picked whole until it is entered", () => {
    const doc = docOf([{ id: "g", type: "group", children: [{ id: "a", type: "rect", width: 10, height: 10 } as DNode, { id: "b", type: "rect", x: 20, width: 10, height: 10 } as DNode] }]);
    const path = designGeo.hitPath(doc.pages[0]!, { x: 25, y: 5 })!;
    expect(designGeo.pickTarget(path, [], false).id).toBe("g");
    expect(designGeo.pickTarget(path, ["a"], false).id).toBe("b");
    expect(designGeo.pickTarget(path, [], true).id).toBe("b");
  });

  test("resizes from a handle, keeps the ratio with shift, and keeps a rotated box's far corner still", () => {
    const n = designModel.createNode("rect", { x: 0, y: 0, width: 100, height: 50 });
    expect(designGeo.resizeNode(n, designGeo.IDENTITY, "se", { x: 150, y: 80 })).toEqual({ x: 0, y: 0, width: 150, height: 80 });
    const ratio = designGeo.resizeNode(n, designGeo.IDENTITY, "se", { x: 200, y: 60 }, { keepRatio: true });
    expect(ratio.width / ratio.height).toBeCloseTo(2);
    const flipped = designGeo.resizeNode(n, designGeo.IDENTITY, "e", { x: -20, y: 25 });
    expect(flipped).toEqual({ x: -20, y: 0, width: 20, height: 50 });

    const turned = { ...n, rotation: 30 };
    const before = designGeo.apply(designGeo.localMatrix(turned), { x: 0, y: 0 });
    const box = designGeo.resizeNode(turned, designGeo.IDENTITY, "se", designGeo.apply(designGeo.localMatrix(turned), { x: 180, y: 90 }));
    near(box.width, 180);
    near(box.height, 90);
    const after = designGeo.apply(designGeo.localMatrix({ ...turned, ...box }), { x: 0, y: 0 });
    near(after.x, before.x);
    near(after.y, before.y);
  });

  test("snaps edges and centers within the threshold and draws the guide", () => {
    const snap = designGeo.snapRect({ x: 103, y: 0, width: 50, height: 50 }, [{ x: 0, y: 200, width: 100, height: 20 }], 5);
    expect(snap.dx).toBe(-3);
    expect(snap.dy).toBe(0);
    expect(snap.guides).toEqual([{ axis: "x", at: 100, from: 0, to: 220 }]);
    expect(designGeo.snapRect({ x: 120, y: 0, width: 50, height: 50 }, [{ x: 0, y: 200, width: 100, height: 20 }], 5).dx).toBe(0);
  });

  test("aligns and distributes", () => {
    const doc = docOf([{ id: "a", type: "rect", x: 0, width: 10 }, { id: "b", type: "rect", x: 50, width: 10 }, { id: "c", type: "rect", x: 200, width: 20, y: 30 }]);
    const page = doc.pages[0]!;
    expect(nodeIn(designGeo.align(doc, page, ["b", "c"], "left"), "c").x).toBe(50);
    expect(nodeIn(designGeo.align(doc, page, ["a", "c"], "top"), "c").y).toBe(0);
    // 220 wide in all, 40 of boxes: gaps of 90.
    expect(nodeIn(designGeo.distribute(doc, page, ["a", "b", "c"], "x"), "b").x).toBe(100);
  });

  test("groups fit what they hold", () => {
    const doc = designGeo.fitGroups(docOf([{ id: "g", type: "group", x: 10, y: 10, width: 1, height: 1, children: [{ type: "rect", x: 20, y: 30, width: 10, height: 10 } as DNode, { type: "rect", x: 50, y: 40, width: 10, height: 20 } as DNode] }]));
    const g = nodeIn(doc, "g");
    expect([g.x, g.y, g.width, g.height]).toEqual([30, 40, 40, 30]);
    expect(g.children![0]!.x).toBe(0);
  });
});

describe("design commands", () => {
  const world = (doc: DDoc, id: string) => designGeo.nodeBounds(doc.pages[0]!, id)!;

  test("group and ungroup keep every layer where it is", () => {
    const doc = docOf([{ id: "a", type: "rect", x: 10, y: 10, width: 10, height: 10 }, { id: "b", type: "ellipse", x: 40, y: 30, width: 10, height: 10 }]);
    const grouped = designOps.groupSelection(doc, "p", ["a", "b"])!;
    const gid = grouped.selection[0]!;
    expect(nodeIn(grouped.doc, gid).type).toBe("group");
    expect(world(grouped.doc, "a")).toEqual(world(doc, "a"));
    expect(world(grouped.doc, gid)).toEqual({ x: 10, y: 10, width: 40, height: 30 });
    const back = designOps.ungroup(grouped.doc, "p", [gid])!;
    expect(back.selection.slice().sort()).toEqual(["a", "b"]);
    expect(world(back.doc, "b")).toEqual(world(doc, "b"));
  });

  test("moving into a frame keeps the page position", () => {
    const doc = docOf([{ id: "f", type: "frame", x: 100, y: 100, width: 300, height: 300 }, { id: "r", type: "rect", x: 150, y: 150, width: 10, height: 10 }]);
    const moved = designOps.reparent(doc, "p", ["r"], "f");
    expect(designModel.findNode(moved, "r")!.at.parent!.id).toBe("f");
    expect(nodeIn(moved, "r").x).toBe(50);
    expect(world(moved, "r")).toEqual(world(doc, "r"));
    expect(designOps.reparent(doc, "p", ["f"], "f")).toBe(doc);
  });

  test("duplicate, paste and reorder", () => {
    const doc = docOf([{ id: "a", type: "rect" }, { id: "b", type: "rect" }, { id: "f", type: "frame", x: 500, width: 200, height: 200 }]);
    const dup = designOps.duplicate(doc, "p", ["a"], 10)!;
    const copy = nodeIn(dup.doc, dup.selection[0]!);
    expect([copy.x, copy.y]).toEqual([10, 10]);
    expect(dup.doc.pages[0]!.children.map((n) => n.id).slice(0, 2)).toEqual(["a", copy.id]);
    const clip = designOps.copyNodes(doc.pages[0]!, ["a"]);
    const pasted = designOps.paste(doc, "p", clip, ["f"])!;
    const into = designModel.findNode(pasted.doc, pasted.selection[0]!)!;
    expect(into.at.parent!.id).toBe("f");
    expect(into.at.node.x).toBe(-500);
    expect(designOps.reorder(doc, "p", ["a"], "front").pages[0]!.children.map((n) => n.id)).toEqual(["b", "f", "a"]);
    expect(designOps.reorder(doc, "p", ["f"], "backward").pages[0]!.children.map((n) => n.id)).toEqual(["a", "f", "b"]);
  });

  test("components, instances and detach", () => {
    const doc = docOf([{ id: "card", type: "frame", width: 200, height: 100, children: [{ id: "dot", type: "ellipse", width: 20, height: 20 } as DNode] }]);
    const comp = designOps.makeComponent(doc, "p", ["card"])!;
    expect(nodeIn(comp.doc, "card").type).toBe("component");
    const inst = designOps.instantiate(comp.doc, "p", "card")!;
    const node = nodeIn(inst.doc, inst.selection[0]!);
    expect(node.type).toBe("instance");
    expect([node.x, node.width]).toEqual([240, 200]);
    const frame = nodeIn(designOps.detachInstance(inst.doc, [node.id]), node.id);
    expect(frame.type).toBe("frame");
    expect(frame.children).toHaveLength(1);
    expect(frame.children![0]!.id).not.toBe("dot");
  });

  test("an agent's edit is found node by node", () => {
    const doc = docOf([{ id: "a", type: "rect" }, { id: "b", type: "rect" }]);
    const next = designModel.insertNodes(designModel.patchNodes(doc, ["b"], { x: 4 }), "p", null, [designModel.createNode("text", { id: "t", text: "Hi" })]);
    expect(designOps.changedIds(doc, next).sort()).toEqual(["b", "t"]);
  });
});

describe("design text and drawing", () => {
  test("auto-width text measures itself; fixed-width text wraps", () => {
    const auto = designModel.createNode("text", { text: "Hello\nWorld!!", font: { family: "Inter", size: 20, weight: 400 } });
    const size = designText.sizeOf(auto);
    expect(size.width).toBe(Math.ceil(7 * 20 * 0.55));
    expect(size.height).toBe(48);
    const wrapped = designText.layoutText({ ...auto, text: "one two three four", auto_resize: "height", width: 100 });
    expect(wrapped.lines.map((l) => l.text.trim())).toEqual(["one two", "three", "four"]);
    expect(designOps.bakeText(docOf([{ id: "t", type: "text", text: "abc" }])).pages[0]!.children[0]!.width).toBeGreaterThan(0);
  });

  test("exports SVG with gradients, effects, clips and text", () => {
    const doc = designSample();
    const page = doc.pages[0]!;
    const plan = designExport.exportPlan(page, [])!;
    expect(plan.bounds.width).toBe(1920);
    expect(plan.background).toBe("#F5F5F5");
    const svg = designExport.buildSvg(doc, page, plan, { style: "@font-face{}" });
    expect(svg.startsWith("<?xml")).toBe(true);
    for (const part of ["<linearGradient", "<filter", "<clipPath", ">Build faster</tspan>", "@font-face{}", 'fill="#F5F5F5"']) expect(svg).toContain(part);
    expect(designExport.buildSvg(doc, page, designExport.exportPlan(page, ["cta"])!)).toContain('viewBox="0 0 160 48"');
    expect(designExport.familiesOf(doc, page, ["desk", "card-1"])).toEqual(new Set(["Inter"]));
    expect(designExport.exportName(doc, page, ["cta"], "png", 2)).toBe("Button@2x.png");
  });

  test("scales path data so strokes keep their width", () => {
    expect(designRender.scalePath("M0 0h10v10A1 1 0 0 1 2 2z", 2, 3)).toBe("M 0 0 h 20 v 30 A 2 3 0 0 1 4 6 z");
    expect(designRender.polygonPoints(4, 10, 10)).toBe("5,0 10,5 5,10 0,5");
  });

  test("number fields take simple arithmetic and nothing else", () => {
    expect(arithmetic("100/2")).toBe(50);
    expect(arithmetic("24 + 8*2")).toBe(40);
    expect(arithmetic("-(3+1)")).toBe(-4);
    expect(arithmetic("2;alert(1)")).toBeNaN();
    expect(arithmetic("")).toBeNaN();
  });
});

describe("following the agent's design work", () => {
  test("names the file a design write targets", () => {
    expect(designTarget("design_canvas", JSON.stringify({ action: "apply", file: "landing" }))).toBe("landing");
    expect(designTarget("design_canvas", JSON.stringify({ action: "read", file: "landing" }))).toBe("");
    expect(designTarget("design_canvas", JSON.stringify({ action: "create", file: "../x" }))).toBe("");
    expect(designTarget("edit", JSON.stringify({ action: "apply", file: "landing" }))).toBe("");
    expect(designTarget("design_canvas", "{")).toBe("");
  });

  test("opens a file once per chat", () => {
    const msg = (sid: string) => ({ id: "m", role: "assistant", session_id: sid, parts: [{ type: "tool_call", data: { id: "c", name: "design_canvas", input: JSON.stringify({ action: "create", file: "logo" }), finished: false } }], model: "", provider: "", created_at: 0, updated_at: 0 }) as never;
    expect(newDesignTargets(msg("s-follow"))).toEqual(["logo"]);
    expect(newDesignTargets(msg("s-follow"))).toEqual([]);
    expect(newDesignTargets(msg("s-other"))).toEqual(["logo"]);
  });

  test("reads as designing in the thread", () => {
    expect(category("design_canvas")).toBe("design");
    expect(toolVerb("design_canvas", false)).toBe("Designing");
    expect(toolArg({ id: "1", name: "design_canvas", input: JSON.stringify({ action: "apply", file: "landing", ops: [{}, {}] }), finished: true } as never)).toBe("landing · 2");
  });
});

// Phase 2: auto layout and constraints.

const { resolveLayout } = await import("../renderer/src/features/design/layout");

const box = (id: string, w: number, h: number, extra: Partial<DNode> = {}): Partial<DNode> => ({ id, type: "rect", x: 0, y: 0, width: w, height: h, ...extra });
const stack = (extra: Record<string, unknown>, children: Partial<DNode>[], frame: Partial<DNode> = {}): Partial<DNode> => ({ id: "f", type: "frame", x: 0, y: 0, width: 400, height: 300, layout: extra, children, ...frame }) as Partial<DNode>;

describe("auto layout", () => {
  test("a column stacks children with gap and padding", () => {
    const doc = resolveLayout(docOf([stack({ dir: "column", gap: 10, pad: 20 }, [box("a", 50, 30), box("b", 80, 40)])]), null, designText.sizeOf);
    expect([nodeIn(doc, "a").x, nodeIn(doc, "a").y]).toEqual([20, 20]);
    expect([nodeIn(doc, "b").x, nodeIn(doc, "b").y]).toEqual([20, 60]);
  });

  test("a row centers on both axes and spreads between", () => {
    const centered = resolveLayout(docOf([stack({ dir: "row", gap: 0, main: "center", cross: "center" }, [box("a", 100, 50), box("b", 100, 100)])]), null, designText.sizeOf);
    expect([nodeIn(centered, "a").x, nodeIn(centered, "a").y]).toEqual([100, 125]);
    expect([nodeIn(centered, "b").x, nodeIn(centered, "b").y]).toEqual([200, 100]);
    const between = resolveLayout(docOf([stack({ dir: "row", main: "between", pad: [0, 10, 0, 10] }, [box("a", 100, 50), box("b", 100, 50), box("c", 100, 50)])]), null, designText.sizeOf);
    expect([nodeIn(between, "a").x, nodeIn(between, "b").x, nodeIn(between, "c").x]).toEqual([10, 150, 290]);
  });

  test("hug frames fit their content and fill children share what is left", () => {
    const hug = resolveLayout(docOf([stack({ dir: "row", gap: 8, pad: [4, 6] }, [box("a", 50, 30), box("b", 70, 20)], { sizing_x: "hug", sizing_y: "hug" })]), null, designText.sizeOf);
    expect([nodeIn(hug, "f").width, nodeIn(hug, "f").height]).toEqual([50 + 70 + 8 + 12, 30 + 8]);
    const fill = resolveLayout(docOf([stack({ dir: "row", gap: 10, pad: 0 }, [box("a", 100, 30), box("b", 10, 30, { sizing_x: "fill" }), box("c", 10, 30, { sizing_x: "fill" })])]), null, designText.sizeOf);
    expect(nodeIn(fill, "b").width).toBe(140);
    expect(nodeIn(fill, "c").x).toBe(100 + 10 + 140 + 10);
  });

  test("cross stretch, absolute children and nested hug", () => {
    const doc = resolveLayout(
      docOf([
        stack({ dir: "column", cross: "stretch", pad: 10 }, [
          box("a", 10, 20),
          box("pin", 30, 30, { absolute: true, x: 5, y: 7 }),
          { id: "inner", type: "frame", width: 10, height: 10, sizing_y: "hug", layout: { dir: "column", pad: 3 }, children: [box("i", 20, 14)] } as Partial<DNode>,
        ]),
      ]),
      null,
      designText.sizeOf,
    );
    expect(nodeIn(doc, "a").width).toBe(380);
    expect([nodeIn(doc, "pin").x, nodeIn(doc, "pin").y]).toEqual([5, 7]);
    expect(nodeIn(doc, "inner").height).toBe(20);
    expect(nodeIn(doc, "inner").y).toBe(30);
  });

  test("text wraps to a filled width and stays one object when nothing moves", () => {
    const t: Partial<DNode> = { id: "t", type: "text", text: "hello world this wraps", font: { family: "Inter", size: 16, weight: 400 }, sizing_x: "fill", auto_resize: "width" };
    const doc = resolveLayout(docOf([stack({ dir: "column", pad: 10 }, [t])]), null, designText.sizeOf);
    expect(nodeIn(doc, "t").width).toBe(380);
    expect(nodeIn(doc, "t").auto_resize).toBe("height");
    expect(resolveLayout(doc, null, designText.sizeOf)).toBe(doc);
  });

  test("sanitizing repairs a layout and expands padding", () => {
    const doc = docOf([stack({ dir: "diagonal", gap: -4, pad: [1, 2] }, [])]);
    expect(nodeIn(doc, "f").layout).toEqual({ dir: "column", gap: 0, pad: [1, 2, 1, 2], main: "start", cross: "start" });
  });
});

describe("constraints", () => {
  const frame = (w: number, h: number, children: Partial<DNode>[]): Partial<DNode> => ({ id: "f", type: "frame", x: 0, y: 0, width: w, height: h, children });

  test("a resized frame moves and stretches children by their constraints", () => {
    const before = docOf([
      frame(200, 100, [
        box("r", 20, 20, { x: 170, y: 5, constraints: { x: "right", y: "top" } }),
        box("c", 20, 20, { x: 90, y: 40, constraints: { x: "center", y: "bottom" } }),
        box("s", 100, 20, { x: 10, y: 0, constraints: { x: "stretch", y: "top" } }),
        box("k", 50, 50, { x: 100, y: 10, constraints: { x: "scale", y: "scale" } }),
      ]),
    ]);
    const after = resolveLayout(designModel.patchNodes(before, ["f"], { width: 400, height: 200 }), before, designText.sizeOf);
    expect(nodeIn(after, "r").x).toBe(370);
    expect([nodeIn(after, "c").x, nodeIn(after, "c").y]).toEqual([190, 140]);
    expect(nodeIn(after, "s").width).toBe(300);
    expect(nodeIn(after, "k")).toMatchObject({ x: 200, y: 20, width: 100, height: 100 });
  });

  test("children without constraints and ones the edit also changed are left alone", () => {
    const before = docOf([frame(200, 100, [box("a", 20, 20, { x: 170 }), box("b", 20, 20, { x: 10, constraints: { x: "right", y: "top" } })])]);
    const edited = designModel.patchNodes(designModel.patchNodes(before, ["f"], { width: 300 }), ["b"], { x: 50 });
    const after = resolveLayout(edited, before, designText.sizeOf);
    expect(nodeIn(after, "a").x).toBe(170);
    expect(nodeIn(after, "b").x).toBe(50);
  });
});

// Phase 3: styles, variables, variants and overrides.

const { resolveTokens } = await import("../renderer/src/features/design/tokens");

describe("styles, variables and instances", () => {
  const master = (id: string, extra: Partial<DNode>, kids: Partial<DNode>[]): Partial<DNode> => ({ id, type: "component", x: 0, y: 0, width: 100, height: 40, children: kids as DNode[], ...extra });
  const solid = (color: string) => [{ type: "solid" as const, color }];

  test("variants pick the component of the set with the asked values", () => {
    const doc = docOf([
      master("def", { set: "Button", variant: { state: "default" } }, [box("bg", 100, 40, { fills: solid("#111111") })]),
      master("hov", { set: "Button", variant: { state: "hover" }, x: 200 }, [box("bg2", 100, 40, { fills: solid("#2222FF") })]),
      { id: "a", type: "instance", component_id: "def", x: 0, y: 100, width: 100, height: 40 },
      { id: "b", type: "instance", component_id: "def", variant: { state: "hover" }, x: 0, y: 200, width: 100, height: 40 },
    ]);
    const r = resolveTokens(doc);
    expect(nodeIn(r, "a").children![0]!.fills).toEqual(solid("#111111"));
    expect(nodeIn(r, "b").children![0]!.fills).toEqual(solid("#2222FF"));
    // The stored document is not touched.
    expect(nodeIn(doc, "a").children).toBeUndefined();
  });

  test("overrides change a layer of one instance only", () => {
    const doc = docOf([
      master("m", {}, [box("bg", 100, 40, { fills: solid("#111111") }), { id: "label", type: "text", text: "Go", font: { family: "Inter", size: 16, weight: 500 } }]),
      { id: "i1", type: "instance", component_id: "m", width: 100, height: 40, overrides: { label: { text: "Buy", font: { size: 20 } }, bg: { visible: false } } as never },
      { id: "i2", type: "instance", component_id: "m", y: 60, width: 100, height: 40 },
    ]);
    const r = resolveTokens(doc);
    const label = nodeIn(r, "i1").children![1]!;
    expect([label.text, label.font!.size, label.font!.family]).toEqual(["Buy", 20, "Inter"]);
    expect(nodeIn(r, "i1").children![0]!.visible).toBe(false);
    expect(nodeIn(r, "i2").children![1]!.text).toBe("Go");
  });

  test("styles and variables resolve for the active mode", () => {
    const raw = {
      pages: [{ id: "p", children: [box("b", 50, 50, { fill_style: "primary" }), { id: "t", type: "text", text: "Hi", text_style: "h1" }] }],
      styles: { fills: { primary: { name: "Primary", fills: [{ type: "solid", color: "#000000", variable: "brand" }] } }, text: { h1: { name: "H1", font: { family: "Inter", size: 32, weight: 700 }, line_height: 40 } } },
      variables: { modes: ["Light", "Dark"], mode: "Light", items: { brand: { name: "Brand", values: { Light: "#0055FF", Dark: "#88AAFF" } } } },
    };
    const doc = designModel.sanitizeDoc(raw);
    expect((nodeIn(resolveTokens(doc), "b").fills![0] as { color: string }).color).toBe("#0055FF");
    const t = nodeIn(resolveTokens(doc), "t");
    expect([t.font!.size, t.line_height]).toEqual([32, 40]);
    const dark = { ...doc, variables: { ...doc.variables!, mode: "Dark" } };
    expect((nodeIn(resolveTokens(dark), "b").fills![0] as { color: string }).color).toBe("#88AAFF");
    // Unchanged nodes keep their identity between resolves.
    expect(nodeIn(resolveTokens(dark), "b")).toBe(nodeIn(resolveTokens(dark), "b"));
  });

  test("sanitizing drops broken tokens and bad ids", () => {
    const doc = designModel.sanitizeDoc({
      pages: [{ id: "p", children: [{ id: "x", type: "rect", fill_style: "bad id", overrides: { y: {} } }] }],
      styles: { fills: { ok: { name: "Ok", fills: [{ type: "solid", color: "#123456" }] }, bad: { fills: [] } } },
      variables: { modes: ["A", "A", ""], mode: "Z", items: { v: { values: { A: "nope" } } } },
    });
    expect(nodeIn(doc, "x").fill_style).toBeUndefined();
    expect(nodeIn(doc, "x").overrides).toBeUndefined();
    expect(Object.keys(doc.styles!.fills!)).toEqual(["ok"]);
    expect(doc.variables).toEqual({ modes: ["A"], mode: "A", items: { v: { name: "v", values: { A: "#000000" } } } });
  });

  test("a document without tokens resolves to itself", () => {
    const doc = docOf([box("r", 10, 10)]);
    expect(resolveTokens(doc)).toBe(doc);
  });
});

// Phase 4: boolean groups, masks and image fills.

describe("vector features", () => {
  const svgOfIds = (doc: DDoc, ids: string[]) => {
    const page = doc.pages[0]!;
    return designExport.buildSvg(doc, page, designExport.exportPlan(page, ids)!);
  };
  const ellipse = (id: string, x: number): Partial<DNode> => ({ id, type: "ellipse", x, y: 0, width: 60, height: 60 });

  test("boolean groups draw as masks of their shapes", () => {
    for (const [op, marker] of [["union", "<mask"], ["subtract", 'fill="#000"'], ["intersect", 'mask="url(#'], ["exclude", "difference"]] as const) {
      const doc = docOf([{ id: "g", type: "group", bool_op: op, fills: [{ type: "solid", color: "#FF0000" }], children: [ellipse("a", 0), ellipse("b", 30)] }]);
      const svg = svgOfIds(doc, ["g"]);
      expect(svg, op).toContain(marker);
      expect(svg, op).toContain('fill="#FF0000"');
    }
    const plain = docOf([{ id: "g", type: "group", children: [ellipse("a", 0)] }]);
    expect(svgOfIds(plain, ["g"])).not.toContain("<mask");
    expect(nodeIn(docOf([{ id: "r", type: "rect", bool_op: "union" } as never]), "r").bool_op).toBeUndefined();
  });

  test("a mask clips the siblings above it and is not drawn", () => {
    const doc = docOf([{ id: "f", type: "frame", width: 200, height: 200, children: [box("low", 200, 200), { ...ellipse("m", 50), mask: true, width: 80, height: 80 }, box("top", 200, 200, { fills: [{ type: "solid", color: "#00FF00" }] })] as DNode[] }]);
    const svg = svgOfIds(doc, ["f"]);
    expect(svg).toContain("<clipPath");
    expect(svg.match(/<ellipse/g)?.length).toBe(1);
  });

  test("image paints become patterns and need a source", () => {
    const doc = docOf([box("r", 100, 50, { fills: [{ type: "image", src: "data:image/png;base64,AAAA", fit: "contain" }, { type: "image", src: "" } as never] })]);
    expect(nodeIn(doc, "r").fills).toHaveLength(1);
    const svg = svgOfIds(doc, ["r"]);
    expect(svg).toContain("<pattern");
    expect(svg).toContain('preserveAspectRatio="xMidYMid meet"');
  });
});

describe("vector paths", () => {
  const P = () => import("../renderer/src/features/design/path");

  test("path data round trips through anchors", async () => {
    const { parsePath, pathData, pathBounds } = await P();
    const subs = parsePath("M10 10 L50 10 C60 10 70 20 70 30 Q70 50 50 50 H10 Z");
    expect(subs).toHaveLength(1);
    expect(subs[0]!.closed).toBe(true);
    expect(subs[0]!.anchors).toHaveLength(5);
    expect(subs[0]!.anchors[1]!.out).toEqual({ x: 60, y: 10 });
    const again = parsePath(pathData(subs));
    expect(pathData(again)).toBe(pathData(subs));
    const b = pathBounds(subs);
    expect([b.x, b.y, b.width]).toEqual([10, 10, 60]);
  });

  test("relative commands, implicit lines, smooth curves and arcs", async () => {
    const { parsePath, pathBounds } = await P();
    const rel = parsePath("m10 10 20 0 0 20 z");
    expect(rel[0]!.anchors.map((a) => [a.x, a.y])).toEqual([[10, 10], [30, 10], [30, 30]]);
    const arc = parsePath("M0 0 A50 50 0 0 1 100 0");
    const b = pathBounds(arc);
    expect(b.width).toBeCloseTo(100, 3);
    expect(b.height).toBeGreaterThan(40);
    expect(arc[0]!.anchors.length).toBeGreaterThan(2);
    expect(parsePath("M0 0 C 10 0 10 10 20 10 S 30 20 40 20")[0]!.anchors).toHaveLength(3);
    expect(parsePath("garbage")).toEqual([]);
  });

  test("shapes flatten to paths in their own box and the box follows an edit", async () => {
    const { shapeToPath, pathData, pathBounds, pathFields, nodePath } = await P();
    const rect = designModel.createNode("rect", { width: 100, height: 40, radius: 10 });
    const subs = shapeToPath(rect)!;
    const b = pathBounds(subs);
    expect([b.x, b.y, b.width, b.height]).toEqual([0, 0, 100, 40]);
    expect(subs[0]!.anchors.length).toBe(8);
    expect(pathBounds(shapeToPath(designModel.createNode("ellipse", { width: 80, height: 40 }))!)).toEqual({ x: 0, y: 0, width: 80, height: 40 });
    expect(shapeToPath(designModel.createNode("text"))).toBeNull();
    const node = designModel.createNode("path", { x: 5, y: 5, ...pathFields({ x: 5, y: 5 }, [{ closed: false, anchors: [{ x: 0, y: 0 }, { x: 20, y: 10 }] }]) });
    expect(pathData(nodePath(node))).toBe("M0 0L20 10");
    const moved = pathFields(node, [{ closed: false, anchors: [{ x: -10, y: 0 }, { x: 20, y: 10 }] }]);
    expect([moved.x, moved.width]).toEqual([-5, 30]);
  });

  test("a stroke outlines into a filled shape of the stroke's width", async () => {
    const { outlineStroke, pathBounds } = await P();
    const out = outlineStroke([{ closed: false, anchors: [{ x: 0, y: 0 }, { x: 100, y: 0 }] }], 10);
    const b = pathBounds(out);
    expect([b.x, b.y, b.width, b.height]).toEqual([0, -5, 100, 10]);
    expect(outlineStroke([{ closed: false, anchors: [{ x: 0, y: 0 }, { x: 0, y: 0 }] }], 10)).toEqual([]);
    expect(outlineStroke([{ closed: true, anchors: [{ x: 0, y: 0 }, { x: 10, y: 0 }, { x: 10, y: 10 }] }], 0)).toEqual([]);
  });

  test("freehand strokes simplify and smooth", async () => {
    const { simplify, smooth } = await P();
    const line = Array.from({ length: 50 }, (_, i) => ({ x: i, y: i * 0.01 }));
    expect(simplify(line, 0.5)).toHaveLength(2);
    const bent = [...line, ...Array.from({ length: 50 }, (_, i) => ({ x: 49, y: 0.5 + i }))];
    expect(simplify(bent, 0.5).length).toBe(3);
    const s = smooth([{ x: 0, y: 0 }, { x: 10, y: 10 }, { x: 20, y: 0 }]);
    expect(s[1]!.in).toBeDefined();
    expect(s[0]!.in).toBeUndefined();
  });
});

// Phase 5: prototyping.

const proto = await import("../renderer/src/features/design/proto");

describe("prototype logic", () => {
  const screens = () =>
    designModel.sanitizeDoc({
      pages: [
        {
          id: "p",
          children: [
            { id: "home", type: "frame", width: 390, height: 800, flow_start: true, children: [{ id: "btn", type: "rect", width: 100, height: 40, interactions: [{ trigger: "click", action: "navigate", to: "detail", transition: "push", direction: "left" }, { trigger: "hover", action: "overlay", to: "menu", position: "top" }] }, { id: "next", type: "rect", interactions: [{ trigger: "key", key: "ArrowRight", action: "navigate", to: "detail" }, { trigger: "after", delay: 1500, action: "navigate", to: "detail" }] }] },
            { id: "detail", type: "frame", x: 500, width: 390, height: 800, children: [{ id: "back", type: "rect", interactions: [{ trigger: "click", action: "back" }, { trigger: "click", action: "url", url: "javascript:alert(1)" }] }] },
            { id: "menu", type: "frame", x: 1000, width: 200, height: 100, children: [] },
          ],
        },
      ],
    });

  test("sanitizing keeps valid interactions only", () => {
    const doc = designModel.sanitizeDoc({
      pages: [{ id: "p", children: [{ id: "a", type: "rect", interactions: [{ trigger: "click", action: "navigate" }, { trigger: "wave", action: "back" }, { trigger: "click", action: "url", url: "ftp://x" }, { trigger: "click", action: "url", url: "https://example.com", duration: 99999 }, { trigger: "key", key: "Enter", action: "back", easing: "bounce" }] }] }],
    });
    expect(nodeIn(doc, "a").interactions).toEqual([
      { trigger: "click", action: "url", url: "https://example.com", duration: 10000 },
      { trigger: "key", action: "back", key: "Enter" },
    ]);
    const s = screens();
    // Only a frame can start a flow or scroll.
    expect(designModel.sanitizeDoc({ pages: [{ id: "p", children: [{ id: "r", type: "rect", flow_start: true, scroll: "both" }] }] }).pages[0]!.children[0]).not.toHaveProperty("flow_start");
    expect(nodeIn(s, "home").flow_start).toBe(true);
  });

  test("a presentation starts on the selection's frame, then the flow start", () => {
    const page = screens().pages[0]!;
    expect(proto.startFrame(page, [])).toBe("home");
    expect(proto.startFrame(page, ["back"])).toBe("detail");
    expect(proto.startFrame(page, ["menu"])).toBe("menu");
    expect(proto.startFrame({ ...page, children: [] }, [])).toBeNull();
  });

  test("interactions come from the layer or the closest one around it", () => {
    const page = screens().pages[0]!;
    const hit = proto.interactionFor(proto.chainTo(page, "btn"), "click")!;
    expect(hit.interaction).toMatchObject({ action: "navigate", to: "detail", transition: "push" });
    expect(proto.interactionFor(proto.chainTo(page, "btn"), "press")).toBeNull();
    expect(proto.interactionFor(proto.chainTo(page, "next"), "key", "ArrowRight")?.interaction.to).toBe("detail");
    expect(proto.interactionFor(proto.chainTo(page, "next"), "key", "ArrowLeft")).toBeNull();
  });

  test("navigating, overlays and back move the stack", () => {
    const page = screens().pages[0]!;
    let state = proto.initial(page)!;
    const nav = proto.step(page, state, { trigger: "click", action: "navigate", to: "detail" })!;
    expect(nav.state.stack).toEqual(["home", "detail"]);
    expect(proto.step(page, nav.state, { trigger: "click", action: "navigate", to: "detail" })).toBeNull();
    expect(proto.step(page, state, { trigger: "click", action: "navigate", to: "ghost" })).toBeNull();
    state = proto.step(page, state, { trigger: "hover", action: "overlay", to: "menu", position: "top" })!.state;
    expect(state.overlays).toEqual([{ id: "menu", position: "top" }]);
    state = proto.step(page, state, { trigger: "click", action: "back" })!.state;
    expect(state.overlays).toEqual([]);
    expect(proto.step(page, state, { trigger: "click", action: "back" })).toBeNull();
    const url = proto.step(page, state, { trigger: "click", action: "url", url: "https://example.com" })!;
    expect(url.url).toBe("https://example.com");
    expect(proto.ambient(page, nav.state, "after")).toHaveLength(0);
    expect(proto.ambient(page, proto.initial(page)!, "after").map((x) => x.interaction.delay)).toEqual([1500]);
  });

  test("smart animate pairs layers of the same name and type", () => {
    const a = designModel.sanitizeDoc({ pages: [{ id: "p", children: [{ id: "f1", type: "frame", children: [{ id: "x1", name: "Card", type: "rect" }, { id: "y1", name: "Only here", type: "rect" }] }, { id: "f2", type: "frame", children: [{ id: "x2", name: "Card", type: "rect" }, { id: "y2", name: "Card2", type: "ellipse" }] }] }] });
    const [f1, f2] = a.pages[0]!.children;
    const pairs = proto.matchByName(f1!, f2!);
    expect([...pairs.keys()]).toEqual(["x1"]);
    expect(pairs.get("x1")!.id).toBe("x2");
  });
});

// Phase 6: merging concurrent edits, comments.

const { mergeDocs } = await import("../renderer/src/features/design/merge");

describe("collaboration", () => {
  const base = () => docOf([box("a", 50, 50, { x: 0, fills: [{ type: "solid", color: "#111111" }] }), box("b", 50, 50, { x: 100 }), { id: "f", type: "frame", width: 300, height: 300, children: [box("k", 10, 10)] as DNode[] }]);

  test("different fields of one layer and different layers both survive", () => {
    const b = base();
    const mine = designModel.patchNodes(designModel.patchNodes(b, ["a"], { x: 20 }), ["b"], { width: 80 });
    const theirs = designModel.patchNodes(designModel.patchNodes(b, ["a"], { fills: [{ type: "solid", color: "#FF0000" }] }), ["k"], { x: 7 });
    const out = mergeDocs(b, mine, theirs);
    expect(nodeIn(out, "a").x).toBe(20);
    expect((nodeIn(out, "a").fills![0] as { color: string }).color).toBe("#FF0000");
    expect(nodeIn(out, "b").width).toBe(80);
    expect(nodeIn(out, "k").x).toBe(7);
  });

  test("the same field changed on both sides takes theirs", () => {
    const b = base();
    const out = mergeDocs(b, designModel.patchNodes(b, ["a"], { x: 1 }), designModel.patchNodes(b, ["a"], { x: 2 }));
    expect(nodeIn(out, "a").x).toBe(2);
  });

  test("added and deleted layers", () => {
    const b = base();
    const mine = designModel.insertNodes(designModel.removeNodes(b, ["b"]), "p", "f", [designModel.createNode("rect", { id: "mine-new" })]);
    const theirs = designModel.insertNodes(b, "p", null, [designModel.createNode("rect", { id: "their-new" })]);
    const out = mergeDocs(b, mine, theirs);
    const ids = out.pages[0]!.children.map((n) => n.id);
    expect(ids).toContain("their-new");
    expect(ids).not.toContain("b");
    expect(nodeIn(out, "mine-new")).toBeDefined();
    // Deleted by theirs but changed by mine: theirs wins; deleted by mine but changed by theirs: kept.
    const gone = mergeDocs(b, designModel.patchNodes(b, ["a"], { x: 9 }), designModel.removeNodes(b, ["a"]));
    expect(gone.pages[0]!.children.map((n) => n.id)).not.toContain("a");
    const kept = mergeDocs(b, designModel.removeNodes(b, ["a"]), designModel.patchNodes(b, ["a"], { x: 9 }));
    expect(nodeIn(kept, "a").x).toBe(9);
  });

  test("comments from both sides are kept, with replies", () => {
    const c = (id: string, extra = {}) => ({ id, page: "p", x: 1, y: 2, author: "person", text: id, created: "", ...extra });
    const b = { ...base(), comments: [c("one")] };
    const mine = { ...b, comments: [c("one", { replies: [{ author: "person", text: "me", created: "" }] }), c("mine")] };
    const theirs = { ...b, comments: [c("one", { resolved: true }), c("theirs")] };
    const out = mergeDocs(b, mine, theirs);
    expect(out.comments!.map((x) => x.id).sort()).toEqual(["mine", "one", "theirs"]);
    const one = out.comments!.find((x) => x.id === "one")!;
    expect(one.resolved).toBe(true);
    expect(one.replies).toHaveLength(1);
  });

  test("comments are sanitized against the pages", () => {
    const doc = designModel.sanitizeDoc({ pages: [{ id: "p", children: [] }], comments: [{ id: "a", page: "p", text: "hi" }, { id: "b", page: "ghost", text: "x" }, { id: "a", page: "p", text: "dup" }] });
    expect(doc.comments).toHaveLength(1);
    expect(doc.comments![0]!.author).toBe("person");
  });
});

// Phase 7: handoff.

const { codeFor } = await import("../renderer/src/features/design/inspect");

describe("developer handoff", () => {
  const card = () =>
    nodeIn(
      docOf([
        {
          id: "card", type: "frame", x: 0, y: 0, width: 300, height: 200, radius: 12, fills: [{ type: "solid", color: "#FFFFFF" }], effects: [{ type: "drop-shadow", x: 0, y: 4, blur: 12, spread: 0, color: "#00000040" }],
          layout: { dir: "column", gap: 8, pad: [16, 16, 16, 16], main: "start", cross: "stretch" },
          children: [{ id: "t", type: "text", text: "Pro", font: { family: "Inter", size: 20, weight: 700 }, fills: [{ type: "solid", color: "#111111" }] }, box("b", 100, 40, { sizing_x: "fill", fills: [{ type: "solid", color: "#0055FF" }] })] as DNode[],
        },
      ]),
      "card",
    );

  test("CSS follows auto layout and styles", () => {
    const css = codeFor(card(), "css");
    expect(css).toContain("display: flex;");
    expect(css).toContain("gap: 8px;");
    expect(css).toContain("padding: 16px 16px 16px 16px;");
    expect(css).toContain("border-radius: 12px;");
    expect(css).toContain("box-shadow: 0px 4px 12px 0px rgba(0, 0, 0, 0.25);");
    const child = codeFor(card().children![1]!, "css", card());
    expect(child).toContain("width: 100%;");
    expect(child).not.toContain("position: absolute");
    expect(codeFor(card().children![0]!, "css", card())).toContain("font-weight: 700;");
  });

  test("Tailwind, SwiftUI and Compose describe the same layer", () => {
    expect(codeFor(card(), "tailwind")).toContain("flex-col");
    expect(codeFor(card(), "tailwind")).toContain("gap-[8px]");
    const swift = codeFor(card(), "swiftui");
    expect(swift).toContain("VStack(alignment: .leading, spacing: 8)");
    expect(swift).toContain('Text("Pro")');
    expect(swift).toContain(".cornerRadius(12)");
    const kt = codeFor(card(), "compose");
    expect(kt).toContain("Column(");
    expect(kt).toContain("Arrangement.spacedBy(8.dp)");
    expect(kt).toContain("RoundedCornerShape(12.dp)");
  });
});
