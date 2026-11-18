// The design editor on its own page, for a browser the agent drives by
// mouse and keyboard. It is the desktop app's Design tab, served by the
// agent's studio server (internal/designstudio) for one project, so every
// edit lands in the same file the person has open in the app.

import "@fontsource-variable/inter";
import "@fontsource-variable/jetbrains-mono";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { setCSRF } from "../api/http";
import type { WorkspaceView } from "../api/types";
import { DesignTab } from "../features/design/DesignTab";
import { TipProvider, Toasts } from "../primitives/ui";
import { useApp } from "../stores/app";
import { useDesign } from "../stores/design";
import "../design/tokens.css";
import "../design/motion.css";
import "../design/app.css";
import "../design/surfaces.css";
import "../design/panel.css";
import "./studio.css";

interface StudioInfo {
  id: string;
  path: string;
  name: string;
  csrf: string;
}

async function start() {
  const info = (await (await fetch("/studio.json")).json()) as StudioInfo;
  setCSRF(info.csrf);
  const workspace: WorkspaceView = { id: info.id, path: info.path, name: info.name, connected: true, agent_ready: true, configured: true };
  useApp.setState({ workspace });
  const file = new URLSearchParams(location.search).get("file") ?? "";
  if (file) useDesign.setState({ target: file });
  document.title = `${info.name} · Atlas Design`;
  createRoot(document.getElementById("root")!).render(
    <StrictMode>
      <TipProvider>
        <main className="studio">
          <DesignTab />
        </main>
        <Toasts />
      </TipProvider>
    </StrictMode>,
  );
}

// Shortcuts belong to the editor even before anything was clicked: a key
// pressed with focus nowhere goes to the canvas.
document.addEventListener("keydown", (e) => {
  if (e.target !== document.body) return;
  const canvas = document.querySelector<HTMLElement>(".design-canvas");
  if (!canvas) return;
  canvas.focus({ preventScroll: true });
  const copy = new KeyboardEvent("keydown", e);
  if (!canvas.dispatchEvent(copy)) e.preventDefault();
});

void start();
