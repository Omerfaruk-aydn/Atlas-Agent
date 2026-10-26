import { Router } from "./router";
import { canonical } from "./path";
import { MAX_ROUTES } from "./config";
function eq(a: unknown, b: unknown): void { if (a !== b) throw new Error(`expected ${String(b)}, got ${String(a)}`); }
function rejects(fn: () => unknown): void { let rejected = false; try { fn(); } catch { rejected = true; } if (!rejected) throw new Error("expected rejection"); }
const router = new Router();
router.add("//Users/%41/?ignored=yes#x", "value");
eq(router.get("/Users/A"), "value");
eq(router.get("/users/A"), undefined);
rejects(() => router.add("/Users/A", "duplicate"));
eq(canonical("/a/../b"), "/a/../b");
eq(canonical("/"), "/");
eq(canonical("/%252F"), "/%2F");
rejects(() => canonical("/%2F"));
rejects(() => canonical("/%ZZ"));
rejects(() => canonical("relative"));
eq(MAX_ROUTES, 64);
const full = new Router();
for (let i = 0; i < 64; i++) full.add(`/route-${i}`, "v");
rejects(() => full.add("/overflow", "v"));
