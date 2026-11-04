// Signing in to a subscription: the same flows as `atlas-agent login`, with
// the browser step in front of the person instead of in a terminal. The
// gateway runs the sign-in; this dialog follows it.

import { useCallback, useEffect, useRef, useState } from "react";
import { http } from "../../api/http";
import type { LoginView, ProviderChoice } from "../../api/types";
import { t } from "../../lib/i18n";
import { IconAlert, IconCheck } from "../../primitives/icons";
import { Modal } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { ProviderLogo } from "./ProviderLogo";

const POLL_MS = 1000;
const CLOSE_AFTER_MS = 900;

export function AccountLogin({ provider, onClose, onDone }: { provider: ProviderChoice; onClose(): void; onDone(): void }) {
  const workspaceId = useApp((s) => s.workspace?.id ?? "");
  const reloadCatalog = useApp((s) => s.reloadCatalog);
  const toast = useApp((s) => s.toast);
  const [view, setView] = useState<LoginView | null>(null);
  const [opened, setOpened] = useState(false);
  const [attempt, setAttempt] = useState(0);
  // The parent's callbacks change on every render; the timer below must not.
  const finish = useRef({ onClose, onDone });
  finish.current = { onClose, onDone };
  const path = `/api/workspaces/${encodeURIComponent(workspaceId)}/providers/${encodeURIComponent(provider.id)}/login`;

  const openPage = useCallback((url: string | undefined) => {
    if (!url) return;
    void window.atlas?.openExternal(url);
    setOpened(true);
  }, []);

  useEffect(() => {
    let live = true;
    setView(null);
    setOpened(false);
    http
      .post<LoginView>(path)
      .then((started) => {
        if (!live) return;
        setView(started);
        // A device code has to be copied first, so that page waits for a click.
        if (!started.user_code) openPage(started.url);
      })
      .catch((err: unknown) => {
        if (live) setView({ state: "failed", error: err instanceof Error ? err.message : String(err) });
      });
    return () => {
      live = false;
    };
  }, [path, attempt, openPage]);

  const waiting = view?.state === "waiting";
  useEffect(() => {
    if (!waiting) return;
    const timer = setInterval(() => {
      http
        .get<LoginView>(path)
        .then((next) => setView((now) => (now?.state === "waiting" ? { ...now, ...next, url: now.url, user_code: now.user_code } : now)))
        .catch(() => {});
    }, POLL_MS);
    return () => clearInterval(timer);
  }, [waiting, path]);

  const done = view?.state === "done";
  useEffect(() => {
    if (!done) return;
    void reloadCatalog().catch(() => {});
    toast(`${provider.name}: ${t("providers.connected")}`);
    finish.current.onDone();
    const timer = setTimeout(() => finish.current.onClose(), CLOSE_AFTER_MS);
    return () => clearTimeout(timer);
  }, [done, provider.name, reloadCatalog, toast]);

  const cancel = () => {
    if (waiting) void http.del(path).catch(() => {});
    onClose();
  };

  const copyAndOpen = () => {
    if (view?.user_code) void navigator.clipboard.writeText(view.user_code).catch(() => {});
    openPage(view?.url);
  };

  return (
    <Modal open onClose={cancel} title={t("login.title").replace("{name}", provider.name)} hideTitle className="login">
      <div className="login-head">
        <ProviderLogo id={provider.id} size={36} />
        <h2>{t("login.title").replace("{name}", provider.name)}</h2>
      </div>

      {!view && <p className="login-status"><span className="spinner" />{t("login.starting")}</p>}

      {waiting && view.user_code && (
        <>
          <p className="modal-text">{t("login.codeBody")}</p>
          <button type="button" className="login-code" title={t("thread.copy")} onClick={() => void navigator.clipboard.writeText(view.user_code ?? "").catch(() => {})}>
            {view.user_code}
          </button>
          <button type="button" className="button login-primary" data-variant={opened ? undefined : "primary"} onClick={copyAndOpen}>
            {t(opened ? "login.reopen" : "login.copyOpen")}
          </button>
        </>
      )}

      {waiting && !view.user_code && view.url && (
        <>
          <p className="modal-text">{t("login.browserBody")}</p>
          <button type="button" className="button login-primary" onClick={() => openPage(view.url)}>
            {t("login.reopen")}
          </button>
        </>
      )}

      {waiting && (opened || !view.url) && (
        <p className="login-status">
          <span className="spinner" />
          {view.message || t(view.url ? "login.waiting" : "login.importing")}
        </p>
      )}

      {done && (
        <p className="login-status" data-tone="success">
          <IconCheck />
          {t("providers.connected")}
        </p>
      )}

      {view?.state === "failed" && (
        <>
          <p className="login-status" data-tone="danger">
            <IconAlert />
            {t("login.failed")}
          </p>
          {view.error && <p className="login-error">{view.error}</p>}
        </>
      )}

      <div className="modal-actions">
        {view?.state === "failed" && (
          <button type="button" className="button" data-variant="primary" onClick={() => setAttempt(attempt + 1)}>
            {t("app.retry")}
          </button>
        )}
        {!done && (
          <button type="button" className="button" onClick={cancel}>
            {t("common.cancel")}
          </button>
        )}
      </div>
    </Modal>
  );
}
