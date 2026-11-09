import type { TabKind } from "../../lib/panel";
import { AddressesView, BrowserSettingsView } from "./AddressViews";
import { ExtensionsView, HistoryView, PasswordsView } from "./BrowserViews";

/** The views of the browser's own data. */
export function BrowserViews({ kind }: { kind: TabKind }) {
  if (kind === "history") return <HistoryView />;
  if (kind === "passwords") return <PasswordsView />;
  if (kind === "extensions") return <ExtensionsView />;
  if (kind === "addresses") return <AddressesView />;
  if (kind === "settings") return <BrowserSettingsView />;
  return null;
}
