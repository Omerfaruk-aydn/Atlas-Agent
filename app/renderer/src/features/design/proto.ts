// Prototype logic: which frame a presentation starts on, which interaction a
// pointer or key event triggers, and how the stack of frames and overlays
// moves. Pure, so the viewer and the tests share it.

import type { DesignNode, Interaction, Page, Trigger } from "./model";

export interface Overlay {
  id: string;
  position: NonNullable<Interaction["position"]>;
}

export interface ProtoState {
  /** Frames visited, the current one last. */
  stack: string[];
  overlays: Overlay[];
}

export interface Outcome {
  state: ProtoState;
  /** The interaction that moved the presentation, for its transition. */
  interaction: Interaction;
  /** A link to open. */
  url?: string;
  /** A layer to scroll into view. */
  scroll?: string;
}

/** The top-level frames of a page, in order. */
export const framesOf = (page: Page): DesignNode[] => page.children.filter((n) => n.type === "frame" && n.visible !== false);

/** The frame a presentation starts on: the selection's top-level frame, else the flow start, else the first frame. */
export function startFrame(page: Page, selection: string[] = []): string | null {
  const frames = framesOf(page);
  for (const id of selection) {
    const top = page.children.find((c) => c.id === id || containsId(c, id));
    if (top && frames.includes(top)) return top.id;
  }
  return (frames.find((f) => f.flow_start) ?? frames[0])?.id ?? null;
}

function containsId(n: DesignNode, id: string): boolean {
  return !!n.children?.some((c) => c.id === id || containsId(c, id));
}

export function initial(page: Page, selection: string[] = []): ProtoState | null {
  const id = startFrame(page, selection);
  return id ? { stack: [id], overlays: [] } : null;
}

/** The nodes from a top-level node down to the one with this id. */
export function chainTo(page: Page, id: string): DesignNode[] {
  const walk = (nodes: DesignNode[], path: DesignNode[]): DesignNode[] | null => {
    for (const n of nodes) {
      const here = [...path, n];
      if (n.id === id) return here;
      const inner = n.children ? walk(n.children, here) : null;
      if (inner) return inner;
    }
    return null;
  };
  return walk(page.children, []) ?? [];
}

/** The interaction a layer or the closest layer around it has for a trigger; `key` narrows key triggers. */
export function interactionFor(chain: DesignNode[], trigger: Trigger, key?: string): { node: DesignNode; interaction: Interaction } | null {
  for (let i = chain.length - 1; i >= 0; i--) {
    const node = chain[i]!;
    const interaction = node.interactions?.find((x) => x.trigger === trigger && (trigger !== "key" || x.key === key));
    if (interaction) return { node, interaction };
  }
  return null;
}

/** Every interaction of a trigger on the current frame and its overlays, for triggers with no pointer target (after, key). */
export function ambient(page: Page, state: ProtoState, trigger: Trigger, key?: string): { node: DesignNode; interaction: Interaction }[] {
  const out: { node: DesignNode; interaction: Interaction }[] = [];
  const visit = (n: DesignNode) => {
    for (const interaction of n.interactions ?? []) if (interaction.trigger === trigger && (trigger !== "key" || interaction.key === key)) out.push({ node: n, interaction });
    n.children?.forEach(visit);
  };
  const shown = [state.stack[state.stack.length - 1], ...state.overlays.map((o) => o.id)];
  for (const id of shown) {
    const f = page.children.find((c) => c.id === id);
    if (f) visit(f);
  }
  return out;
}

/** Applies an interaction to the state; null when it changes nothing. */
export function step(page: Page, state: ProtoState, interaction: Interaction): Outcome | null {
  const frames = new Set(framesOf(page).map((f) => f.id));
  const top = state.stack[state.stack.length - 1]!;
  switch (interaction.action) {
    case "navigate": {
      if (!interaction.to || !frames.has(interaction.to) || interaction.to === top) return null;
      return { interaction, state: { stack: [...state.stack, interaction.to], overlays: [] } };
    }
    case "overlay": {
      if (!interaction.to || !frames.has(interaction.to)) return null;
      const rest = state.overlays.filter((o) => o.id !== interaction.to);
      return { interaction, state: { ...state, overlays: [...rest, { id: interaction.to, position: interaction.position ?? "center" }] } };
    }
    case "close": {
      if (state.overlays.length === 0) return null;
      return { interaction, state: { ...state, overlays: state.overlays.slice(0, -1) } };
    }
    case "back": {
      if (state.overlays.length) return { interaction, state: { ...state, overlays: state.overlays.slice(0, -1) } };
      if (state.stack.length < 2) return null;
      return { interaction, state: { stack: state.stack.slice(0, -1), overlays: [] } };
    }
    case "url":
      return interaction.url ? { interaction, state, url: interaction.url } : null;
    case "scroll_to":
      return interaction.to ? { interaction, state, scroll: interaction.to } : null;
  }
}

export const EASING: Record<NonNullable<Interaction["easing"]>, string> = {
  linear: "linear",
  ease: "ease",
  ease_in: "ease-in",
  ease_out: "ease-out",
  ease_in_out: "ease-in-out",
};

/** Layers that exist in both frames by name, for smart animate: id in `from` → id in `to`. */
export function matchByName(from: DesignNode, to: DesignNode): Map<string, DesignNode> {
  const names = new Map<string, DesignNode>();
  const index = (n: DesignNode) => {
    for (const c of n.children ?? []) {
      if (!names.has(c.name)) names.set(c.name, c);
      index(c);
    }
  };
  index(to);
  const out = new Map<string, DesignNode>();
  const pair = (n: DesignNode) => {
    for (const c of n.children ?? []) {
      const hit = names.get(c.name);
      if (hit && hit.type === c.type) out.set(c.id, hit);
      pair(c);
    }
  };
  pair(from);
  return out;
}
