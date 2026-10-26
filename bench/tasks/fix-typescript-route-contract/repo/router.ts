export class Router {
  private routes = new Map<string, string>();
  add(path: string, value: string): void { this.routes.set(path, value); }
  get(path: string): string | undefined { return this.routes.get(path); }
}
