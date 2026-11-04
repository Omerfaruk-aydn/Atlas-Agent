import "@fontsource-variable/inter";
import "@fontsource-variable/jetbrains-mono";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./app";
import "./design/tokens.css";
import "./design/motion.css";
import "./design/app.css";
import "./design/surfaces.css";
import { useApp } from "./stores/app";

window.atlas?.onTheme((scheme) => useApp.getState().setScheme(scheme));
window.atlas?.onOpenSession((session) => void useApp.getState().selectSession(session));
void useApp.getState().init();

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}
