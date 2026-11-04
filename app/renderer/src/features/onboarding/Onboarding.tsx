// The first launch: the brand moment, then the few choices worth making up
// front. Esc skips it, and every step can be changed later in Settings.

import { useEffect, useState, type ReactNode } from "react";
import type { PermissionMode } from "../../api/types";
import { t, type Key } from "../../lib/i18n";
import { AtlasMark, IconCheck, IconChevron, IconMoon, IconShield, IconSun } from "../../primitives/icons";
import { useApp } from "../../stores/app";
import { Wordmark } from "../home/Wordmark";
import { ProviderTiles } from "../models/Providers";

type Step = "welcome" | "provider" | "prefs";

const STEPS: Step[] = ["welcome", "provider", "prefs"];

function Welcome({ onNext }: { onNext(): void }) {
  return (
    <div className="onboarding-step onboarding-hero">
      <AtlasMark draw width={64} height={64} />
      <div className="onboarding-word">
        <Wordmark />
      </div>
      <p>{t("onboarding.welcome")}</p>
      <button type="button" className="button" data-variant="brand" autoFocus onClick={onNext}>
        {t("onboarding.start")}
      </button>
    </div>
  );
}

function Provider() {
  return (
    <div className="onboarding-step">
      <h1>{t("onboarding.providerTitle")}</h1>
      <p>{t("onboarding.providerBody")}</p>
      <ProviderTiles />
    </div>
  );
}

function Choice({ selected, title, hint, icon, onPick }: { selected: boolean; title: string; hint?: string; icon: ReactNode; onPick(): void }) {
  return (
    <button type="button" role="radio" aria-checked={selected} className="choice" onClick={onPick}>
      <span className="choice-icon">{icon}</span>
      <span className="choice-text">
        <span className="choice-title">{title}</span>
        {hint && <span className="setting-hint">{hint}</span>}
      </span>
      {selected && <IconCheck />}
    </button>
  );
}

const THEMES: { id: "system" | "light" | "dark"; label: Key; icon: ReactNode }[] = [
  { id: "system", label: "common.system", icon: <span className="choice-split" /> },
  { id: "light", label: "settings.light", icon: <IconSun /> },
  { id: "dark", label: "settings.dark", icon: <IconMoon /> },
];

const MODES: { id: PermissionMode; label: Key; hint: Key }[] = [
  { id: "manual", label: "mode.manual", hint: "mode.manual.hint" },
  { id: "auto_accept_edits", label: "mode.autoEdits", hint: "mode.autoEdits.hint" },
];

function Prefs() {
  const theme = useApp((s) => s.theme);
  const setTheme = useApp((s) => s.setTheme);
  const mode = useApp((s) => s.settings?.permission_mode ?? "manual");
  const setMode = useApp((s) => s.setPermissionMode);
  return (
    <div className="onboarding-step">
      <h1>{t("onboarding.prefsTitle")}</h1>
      <div className="onboarding-label">{t("settings.theme")}</div>
      <div className="choices choices-row" role="radiogroup" aria-label={t("settings.theme")}>
        {THEMES.map((item) => (
          <Choice key={item.id} selected={theme === item.id} title={t(item.label)} icon={item.icon} onPick={() => setTheme(item.id)} />
        ))}
      </div>
      <div className="onboarding-label">{t("mode.title")}</div>
      <div className="choices" role="radiogroup" aria-label={t("mode.title")}>
        {MODES.map((item) => (
          <Choice key={item.id} selected={mode === item.id} title={t(item.label)} hint={t(item.hint)} icon={<IconShield />} onPick={() => void setMode(item.id)} />
        ))}
      </div>
    </div>
  );
}

export function Onboarding() {
  const onboarded = useApp((s) => s.ui.onboarded);
  const ready = useApp((s) => s.phase === "ready");
  const hasModels = useApp((s) => (s.catalog?.providers?.length ?? 0) > 0);
  const setUI = useApp((s) => s.setUI);
  const [step, setStep] = useState<Step>("welcome");
  const finish = () => setUI("onboarded", true);

  useEffect(() => {
    if (onboarded || !ready) return;
    const onKey = (e: KeyboardEvent) => {
      // A dialog over this screen takes its own Esc.
      if (e.key === "Escape" && !document.querySelector(".modal")) setUI("onboarded", true);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onboarded, ready, setUI]);

  if (onboarded || !ready) return null;

  const at = STEPS.indexOf(step);
  return (
    <div className="onboarding" role="dialog" aria-modal="true" aria-label={t("onboarding.title")} data-step={step}>
      <div key={step} className="onboarding-card">
        {step === "welcome" && <Welcome onNext={() => setStep("provider")} />}
        {step === "provider" && <Provider />}
        {step === "prefs" && <Prefs />}
        {step !== "welcome" && (
          <div className="onboarding-foot">
            <button type="button" className="button" data-variant="ghost" onClick={() => setStep(STEPS[at - 1])}>
              <IconChevron className="onboarding-back" />
              {t("onboarding.back")}
            </button>
            <div className="onboarding-dots" aria-hidden="true">
              {STEPS.map((s) => (
                <span key={s} data-on={s === step || undefined} />
              ))}
            </div>
            {step === "provider" ? (
              <button type="button" className="button" data-variant={hasModels ? "primary" : undefined} onClick={() => setStep("prefs")}>
                {hasModels ? t("onboarding.next") : t("onboarding.later")}
              </button>
            ) : (
              <button type="button" className="button" data-variant="primary" autoFocus onClick={finish}>
                {t("onboarding.finish")}
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
