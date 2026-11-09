// The page of a new tab: the panel's tools, and sites to go to. A tool that is
// already open leaves the list and the rest move up; Terminal stays, because
// several can be open.

import { useMemo } from "react";
import { t, type Key } from "../../lib/i18n";
import { suggestedSites, type PanelTool } from "../../lib/panel";
import { useKeys } from "../../lib/shortcuts";
import { IconChat, IconDiff, IconFolder, IconJobs, IconMore, IconPen, IconTerminal } from "../../primitives/icons";
import { Kbd, Menu } from "../../primitives/ui";
import { usePanel, usePanelState } from "../../stores/panel";
import { SiteFavicon } from "./SiteIcon";

const TOOLS: { tool: PanelTool; label: Key; shortcut: string; icon: typeof IconDiff }[] = [
  { tool: "changes", label: "rail.changes", shortcut: "rail1", icon: IconDiff },
  { tool: "terminal", label: "panel.terminal", shortcut: "railTerminal", icon: IconTerminal },
  { tool: "side", label: "panel.side", shortcut: "railSide", icon: IconChat },
  { tool: "files", label: "rail.files", shortcut: "rail2", icon: IconFolder },
  { tool: "design", label: "design.title", shortcut: "railDesign", icon: IconPen },
];

function ToolRow({ tool, label, shortcut, icon: Icon }: (typeof TOOLS)[number]) {
  const keys = useKeys(shortcut);
  const openTool = usePanel((s) => s.openTool);
  const addTab = usePanel((s) => s.addTab);
  return (
    <div className="tool-cell" style={{ ["--vt-name" as string]: `tool-${tool}` }}>
      <button type="button" className="launch-row" data-tool={tool} onClick={() => openTool(tool, { fresh: true })}>
        <Icon />
        <span className="launch-row-name">{t(label)}</span>
        {keys && <Kbd keys={keys} />}
      </button>
      {tool === "terminal" && (
        <Menu
          side="bottom"
          align="end"
          trigger={
            <button type="button" className="icon-button tool-more" aria-label={t("panel.more")}>
              <IconMore />
            </button>
          }
          items={[
            { label: t("panel.newTerminal"), icon: <IconTerminal />, onSelect: () => addTab({ kind: "terminal" }) },
            { label: t("rail.jobs"), icon: <IconJobs />, onSelect: () => openTool("jobs") },
          ]}
        />
      )}
    </div>
  );
}

export function NewTab({ onOpen }: { onOpen(url: string): void }) {
  const recent = usePanel((s) => s.recent);
  const open = usePanelState().tabs;
  const sites = useMemo(() => suggestedSites(recent, 4), [recent]);
  const tools = TOOLS.filter((x) => x.tool === "terminal" || !open.some((tab) => tab.kind === x.tool));
  return (
    <div className="newtab">
      <div className="newtab-body">
        <h2 className="newtab-heading">{t("panel.tools")}</h2>
        <div className="tool-grid">
          {tools.map((tool) => (
            <ToolRow key={tool.tool} {...tool} />
          ))}
        </div>
        <h2 className="newtab-heading">{t("panel.suggested")}</h2>
        <div className="site-grid">
          {sites.map((site) => (
            <button key={site.host} type="button" className="site-tile" title={site.url} onClick={() => onOpen(site.url)}>
              <span className="site-tile-icon">
                <SiteFavicon host={site.host} />
              </span>
              <span className="site-tile-name">{site.title}</span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
