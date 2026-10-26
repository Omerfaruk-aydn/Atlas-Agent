import { canonical } from "./path";
import { MAX_ROUTES } from "./config";
export class Router {
  private routes = new Map<string, string>();
  add(path: string, value: string): void {
    const key = canonical(path);
    if (this.routes.has(key) || this.routes.size >= MAX_ROUTES) throw new Error("duplicate or full");
    this.routes.set(key, value);
  }
  get(path: string): string | undefined { return this.routes.get(canonical(path)); }
}
