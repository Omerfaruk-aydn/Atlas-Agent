// Connecting a model provider, the way the terminal does: subscriptions sign
// in through the browser, everything else takes a key. Used by Settings and by
// the first-run flow. Keys go to the gateway once and are never read back.

import { useCallback, useEffect, useMemo, useState } from "react";
import { http } from "../../api/http";
import type { Catalog, ProviderChoice } from "../../api/types";
import { t, type Key } from "../../lib/i18n";
import { IconCheck, IconChevron, IconKey, IconSearch } from "../../primitives/icons";
import { Collapse } from "../../primitives/motion";
import { useApp } from "../../stores/app";
import { AccountLogin } from "./AccountLogin";
import { ProviderLogo } from "./ProviderLogo";

type Group = "account" | "plan" | "key";

const GROUPS: { id: Group; title: Key; hint: Key }[] = [
  { id: "account", title: "providers.account", hint: "providers.accountHint" },
  { id: "plan", title: "providers.plan", hint: "providers.planHint" },
  { id: "key", title: "providers.key", hint: "providers.keyHint" },
];

export function providerGroup(p: Pick<ProviderChoice, "name" | "account">): Group {
  if (p.account) return "account";
  return /coding|token plan|m plan/i.test(p.name) ? "plan" : "key";
}

function useProviders() {
  const workspaceId = useApp((s) => s.workspace?.id ?? "");
  const [providers, setProviders] = useState<ProviderChoice[] | null>(null);
  const load = useCallback(() => {
    if (!workspaceId) return;
    http
      .get<ProviderChoice[]>(`/api/workspaces/${encodeURIComponent(workspaceId)}/providers`)
      .then(setProviders)
      .catch(() => setProviders([]));
  }, [workspaceId]);
  useEffect(load, [load]);
  return { providers, load };
}

function KeyForm({ provider, onSaved }: { provider: ProviderChoice; onSaved(): void }) {
  const workspaceId = useApp((s) => s.workspace?.id ?? "");
  const toast = useApp((s) => s.toast);
  const [key, setKey] = useState("");
  const [saving, setSaving] = useState(false);

  const save = async () => {
    setSaving(true);
    try {
      const catalog = await http.post<Catalog>(`/api/workspaces/${encodeURIComponent(workspaceId)}/providers/${encodeURIComponent(provider.id)}/key`, { api_key: key });
      useApp.setState({ catalog });
      setKey("");
      toast(`${provider.name}: ${t("providers.connected")}`);
      onSaved();
    } catch (err) {
      toast(err instanceof Error ? err.message : String(err), "danger");
    } finally {
      setSaving(false);
    }
  };

  return (
    <form
      className="provider-key"
      onSubmit={(e) => {
        e.preventDefault();
        if (key.trim()) void save();
      }}
    >
      <IconKey />
      <input type="password" autoFocus autoComplete="off" spellCheck={false} value={key} placeholder={t("providers.keyPlaceholder")} aria-label={t("providers.keyPlaceholder")} onChange={(e) => setKey(e.target.value)} />
      <button type="submit" className="button" data-variant="primary" disabled={saving || !key.trim()}>
        {t("common.save")}
      </button>
    </form>
  );
}

function ProviderRow({ provider, onChanged, onSignIn }: { provider: ProviderChoice; onChanged(): void; onSignIn(p: ProviderChoice): void }) {
  const workspaceId = useApp((s) => s.workspace?.id ?? "");
  const toast = useApp((s) => s.toast);
  const [editing, setEditing] = useState(false);
  const [testing, setTesting] = useState(false);

  const test = async () => {
    setTesting(true);
    try {
      const result = await http.post<{ ok: boolean; error?: string }>(`/api/workspaces/${encodeURIComponent(workspaceId)}/providers/${encodeURIComponent(provider.id)}/test`);
      if (result.ok) toast(`${provider.name}: ${t("providers.testOk")}`);
      else toast(`${provider.name}: ${result.error ?? t("providers.testFailed")}`, "danger");
    } catch (err) {
      toast(err instanceof Error ? err.message : String(err), "danger");
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className="provider-row" data-provider={provider.id}>
      <div className="provider-main">
        <ProviderLogo id={provider.id} size={22} reserve />
        <span className="provider-name">
          <span>{provider.name}</span>
          <span className="provider-models">
            {provider.models} {t("providers.models")}
          </span>
        </span>
        {provider.configured && (
          <span className="chip" data-tone="success">
            <IconCheck />
            {t("providers.connected")}
          </span>
        )}
        {provider.configured && !provider.account && (
          <button type="button" className="button" disabled={testing} onClick={() => void test()}>
            {t("providers.test")}
          </button>
        )}
        {provider.account ? (
          <button type="button" className="button" data-variant={provider.configured ? undefined : "primary"} onClick={() => onSignIn(provider)}>
            {t(provider.configured ? "providers.signInAgain" : "providers.signIn")}
          </button>
        ) : (
          <button type="button" className="button" aria-expanded={editing} onClick={() => setEditing(!editing)}>
            {t(provider.configured ? "providers.change" : "providers.connect")}
          </button>
        )}
      </div>
      {!provider.account && (
        <Collapse open={editing}>
          {editing && (
            <KeyForm
              provider={provider}
              onSaved={() => {
                setEditing(false);
                onChanged();
              }}
            />
          )}
        </Collapse>
      )}
    </div>
  );
}

function SignIn({ provider, onClose, onDone }: { provider: ProviderChoice | null; onClose(): void; onDone(): void }) {
  return provider ? <AccountLogin key={provider.id} provider={provider} onClose={onClose} onDone={onDone} /> : null;
}

/** Every provider, grouped by how it connects. */
export function Providers() {
  const { providers, load } = useProviders();
  const [query, setQuery] = useState("");
  const [signingIn, setSigningIn] = useState<ProviderChoice | null>(null);

  const groups = useMemo(() => {
    const q = query.trim().toLocaleLowerCase();
    const list = (providers ?? []).filter((p) => !q || p.name.toLocaleLowerCase().includes(q));
    list.sort((a, b) => Number(b.configured) - Number(a.configured));
    return GROUPS.map((g) => ({ ...g, items: list.filter((p) => providerGroup(p) === g.id) })).filter((g) => g.items.length > 0);
  }, [providers, query]);

  return (
    <div className="providers">
      <label className="search search-boxed">
        <IconSearch />
        <input value={query} placeholder={t("providers.search")} aria-label={t("providers.search")} onChange={(e) => setQuery(e.target.value)} />
      </label>
      {groups.map((group) => (
        <section className="provider-group" key={group.id}>
          <div className="provider-group-head">
            <span>{t(group.title)}</span>
            <span className="setting-hint">{t(group.hint)}</span>
          </div>
          {group.items.map((p) => (
            <ProviderRow key={p.id} provider={p} onChanged={load} onSignIn={setSigningIn} />
          ))}
        </section>
      ))}
      {providers !== null && groups.length === 0 && <div className="sidebar-empty">{t("sidebar.noResults")}</div>}
      <SignIn provider={signingIn} onClose={() => setSigningIn(null)} onDone={load} />
    </div>
  );
}

/** The first run: subscriptions as tiles, with keys a step further. */
export function ProviderTiles() {
  const { providers, load } = useProviders();
  const [signingIn, setSigningIn] = useState<ProviderChoice | null>(null);
  const [keys, setKeys] = useState(false);
  const accounts = (providers ?? []).filter((p) => p.account);

  return (
    <div className="provider-tiles-wrap">
      <div className="provider-tiles">
        {accounts.map((p) => (
          <button key={p.id} type="button" className="provider-tile" data-on={p.configured || undefined} onClick={() => setSigningIn(p)}>
            <ProviderLogo id={p.id} size={24} />
            <span className="provider-tile-name">{p.name}</span>
            {p.configured && <IconCheck />}
          </button>
        ))}
      </div>
      <button type="button" className="group-toggle" aria-expanded={keys} onClick={() => setKeys(!keys)}>
        <IconChevron className="chevron" />
        {t("onboarding.useKey")}
      </button>
      <Collapse open={keys}>
        {keys && (
          <div className="provider-tiles-keys">
            <Providers />
          </div>
        )}
      </Collapse>
      <SignIn provider={signingIn} onClose={() => setSigningIn(null)} onDone={load} />
    </div>
  );
}
