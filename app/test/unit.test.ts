// Unit tests for the logic behind the thread and composer.

import { describe, expect, test, vi } from "vitest";

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
const { canBind, holderOf, keysOf, rebind, SHORTCUTS } = await import("../renderer/src/lib/shortcuts");
const { providerGroup } = await import("../renderer/src/features/models/Providers");

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
