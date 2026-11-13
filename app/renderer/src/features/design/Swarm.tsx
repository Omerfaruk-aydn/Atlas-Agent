// The swarm dialog: a brief, how many variants and which subagents (models) to run them on.

import { useEffect, useRef, useState } from "react";
import { t } from "../../lib/i18n";
import { useApp } from "../../stores/app";
import { useDesign } from "../../stores/design";
import { SWARM_MAX, SWARM_MIN, clampCount, parseAgents, swarmPrompt } from "./swarm-prompt";

export function Swarm({ onClose }: { onClose: () => void }) {
  const file = useDesign((s) => s.file);
  const [brief, setBrief] = useState("");
  const [count, setCount] = useState(3);
  const [agents, setAgents] = useState("");
  const area = useRef<HTMLTextAreaElement>(null);
  useEffect(() => area.current?.focus(), []);
  const ready = !!file && brief.trim().length > 0;

  const start = () => {
    if (!ready || !file) return;
    const text = swarmPrompt({ file, brief, count, agents: parseAgents(agents) });
    onClose();
    void useApp.getState().send(text);
  };

  return (
    <div className="design-swarm" role="dialog" aria-label={t("design.swarm")} onKeyDown={(e) => e.key === "Escape" && (e.stopPropagation(), onClose())}>
      <h3>{t("design.swarm")}</h3>
      <p className="design-hint">{t("design.swarmHint")}</p>
      <textarea ref={area} value={brief} rows={4} placeholder={t("design.swarmBrief")} aria-label={t("design.swarmBrief")} onChange={(e) => setBrief(e.target.value)} />
      <label className="design-field">
        <span>{t("design.swarmCount")}</span>
        <input type="number" min={SWARM_MIN} max={SWARM_MAX} value={count} onChange={(e) => setCount(clampCount(Number(e.target.value)))} />
      </label>
      <label className="design-field">
        <span>{t("design.swarmAgents")}</span>
        <input type="text" value={agents} placeholder={t("design.swarmAgentsHint")} onChange={(e) => setAgents(e.target.value)} />
      </label>
      <div className="design-row">
        <button type="button" className="design-chip" onClick={onClose}>
          {t("common.cancel")}
        </button>
        <button type="button" className="design-chip" data-on disabled={!ready} onClick={start}>
          {t("design.swarmStart")}
        </button>
      </div>
    </div>
  );
}
