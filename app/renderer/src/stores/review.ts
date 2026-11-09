// The reviews of turns: starting one, following it while it runs, and what
// the person did with its findings. A finished review and the person's
// choices are remembered on this computer, per turn.

import { create } from "zustand";
import { ApiError, http } from "../api/http";
import type { Review, ReviewFinding } from "../api/types";
import { t } from "../lib/i18n";
import { emptyMemory, findingKey, fixAllPrompt, fixPrompt, loadMemory, saveMemory, type Remembered } from "../lib/review";
import { useApp } from "./app";

export type Phase = "idle" | "starting" | "running" | "done" | "failed" | "canceled";

export interface Entry {
  phase: Phase;
  review?: Review;
  error?: string;
  /** The chat the reviewed turn belongs to; fixes go there. */
  sessionId: string;
  dismissed: string[];
  sent: string[];
}

interface ReviewState {
  byTurn: Record<string, Entry>;
  /** Loads what is remembered for a turn, and picks up a review that was still running. */
  hydrate(turnId: string, sessionId: string): void;
  start(turnId: string, sessionId: string, paths: string[], task: string): Promise<void>;
  cancel(turnId: string): Promise<void>;
  dismiss(turnId: string, finding: ReviewFinding): void;
  restore(turnId: string): void;
  /** Hands findings to the agent that made the changes, as one request. */
  fix(turnId: string, findings: ReviewFinding[]): Promise<void>;
}

const POLL_MS = 1500;
const GIVE_UP_MS = 20 * 60_000;
const timers = new Map<string, number>();

const base = (): string => `/api/workspaces/${encodeURIComponent(useApp.getState().workspace?.id ?? "")}`;

const blank = (sessionId: string): Entry => ({ phase: "idle", sessionId, dismissed: [], sent: [] });

function remember(turnId: string, entry: Entry) {
  const memory: Remembered = {
    ...emptyMemory(),
    reviewId: entry.phase === "running" || entry.phase === "starting" ? entry.review?.id : undefined,
    review: entry.phase === "done" ? entry.review : undefined,
    dismissed: entry.dismissed,
    sent: entry.sent,
  };
  saveMemory(turnId, memory);
}

function failure(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 501) return t("review.old");
    if (err.status === 429) return t("review.busy");
  }
  return err instanceof Error ? err.message : String(err);
}

export const useReview = create<ReviewState>((set, get) => {
  const put = (turnId: string, patch: Partial<Entry>) => {
    const entry = { ...(get().byTurn[turnId] ?? blank("")), ...patch };
    set((s) => ({ byTurn: { ...s.byTurn, [turnId]: entry } }));
    remember(turnId, entry);
  };

  const stopPolling = (turnId: string) => {
    const timer = timers.get(turnId);
    if (timer !== undefined) clearTimeout(timer);
    timers.delete(turnId);
  };

  const follow = (turnId: string, reviewId: string, startedAt: number) => {
    stopPolling(turnId);
    const tick = async () => {
      timers.delete(turnId);
      const current = get().byTurn[turnId];
      // Cancelled, replaced by a newer review, or the window moved on.
      if (!current || current.phase !== "running" || current.review?.id !== reviewId) return;
      try {
        const review = await http.get<Review>(`${base()}/reviews/${encodeURIComponent(reviewId)}`);
        const now = get().byTurn[turnId];
        if (!now || now.review?.id !== reviewId || now.phase !== "running") return;
        if (review.state === "running") {
          if (Date.now() - startedAt > GIVE_UP_MS) {
            put(turnId, { phase: "failed", review: undefined, error: t("review.timeout") });
            return;
          }
          put(turnId, { review });
          timers.set(turnId, window.setTimeout(() => void tick(), POLL_MS));
          return;
        }
        put(turnId, { phase: review.state, review, error: review.error });
      } catch (err) {
        const gone = err instanceof ApiError && err.status === 404;
        put(turnId, { phase: "failed", review: undefined, error: gone ? t("review.gone") : failure(err) });
      }
    };
    timers.set(turnId, window.setTimeout(() => void tick(), POLL_MS / 3));
  };

  return {
    byTurn: {},

    hydrate(turnId, sessionId) {
      if (get().byTurn[turnId]) return;
      const memory = loadMemory(turnId);
      const entry: Entry = { ...blank(sessionId), dismissed: memory.dismissed, sent: memory.sent };
      if (memory.review) {
        entry.phase = "done";
        entry.review = memory.review;
      } else if (memory.reviewId) {
        entry.phase = "running";
        entry.review = { id: memory.reviewId, session_id: sessionId, state: "running", findings: [], structured: false, started_at: Date.now() };
      }
      set((s) => ({ byTurn: { ...s.byTurn, [turnId]: entry } }));
      if (entry.phase === "running" && entry.review) follow(turnId, entry.review.id, entry.review.started_at);
    },

    async start(turnId, sessionId, paths, task) {
      const previous = get().byTurn[turnId];
      if (previous?.phase === "starting" || previous?.phase === "running") return;
      put(turnId, { phase: "starting", sessionId, review: undefined, error: undefined });
      try {
        const review = await http.post<Review>(`${base()}/sessions/${encodeURIComponent(sessionId)}/reviews`, { paths, task });
        put(turnId, { phase: "running", review, error: undefined });
        follow(turnId, review.id, review.started_at || Date.now());
      } catch (err) {
        put(turnId, { phase: "failed", error: failure(err) });
      }
    },

    async cancel(turnId) {
      const entry = get().byTurn[turnId];
      const id = entry?.review?.id;
      stopPolling(turnId);
      put(turnId, { phase: "canceled", error: undefined });
      if (!id) return;
      try {
        await http.del<Review>(`${base()}/reviews/${encodeURIComponent(id)}`);
      } catch {
        // The review is already gone or finished; the page has stopped following it either way.
      }
    },

    dismiss(turnId, finding) {
      const entry = get().byTurn[turnId];
      if (!entry) return;
      const key = findingKey(finding);
      if (!entry.dismissed.includes(key)) put(turnId, { dismissed: [...entry.dismissed, key] });
    },

    restore(turnId) {
      put(turnId, { dismissed: [] });
    },

    async fix(turnId, findings) {
      const entry = get().byTurn[turnId];
      if (!entry || findings.length === 0 || !entry.sessionId) return;
      const prompt = findings.length === 1 ? fixPrompt(findings[0]!) : fixAllPrompt(findings);
      const keys = findings.map(findingKey);
      // Marked at once, so a double click cannot send the same findings twice.
      put(turnId, { sent: [...new Set([...entry.sent, ...keys])] });
      try {
        await useApp.getState().send(prompt, [], entry.sessionId);
      } catch {
        put(turnId, { sent: entry.sent });
      }
    },
  };
});
