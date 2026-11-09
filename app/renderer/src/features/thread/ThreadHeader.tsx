// The row above a thread: the chat's title, its actions, the summary toggle,
// and a `+` that opens the side panel when it is closed. The panel's tab strip
// shares this row when the panel is open.

import { useState } from "react";
import { t } from "../../lib/i18n";
import { IconArchive, IconFolder, IconMore, IconPin, IconPlus, IconTrash } from "../../primitives/icons";
import { Menu, Modal, Tip } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { usePanel, usePanelState } from "../../stores/panel";
import { SummaryButton } from "../panel/Summary";

export function ThreadHeader() {
  const session = useApp((s) => s.sessions.find((x) => x.id === s.currentId));
  const pinned = useApp((s) => !!s.currentId && s.ui.pinned.includes(s.currentId));
  const togglePin = useApp((s) => s.togglePin);
  const toggleArchive = useApp((s) => s.toggleArchive);
  const deleteSession = useApp((s) => s.deleteSession);
  const panelOpen = usePanelState().open;
  const setOpen = usePanel((s) => s.setOpen);
  const [confirm, setConfirm] = useState(false);
  if (!session) return null;
  return (
    <div className="thread-head">
      <IconFolder className="thread-head-icon" />
      <h1 className="thread-head-title" title={session.title}>
        {session.title}
      </h1>
      <Menu
        side="bottom"
        align="end"
        trigger={
          <button type="button" className="icon-button" aria-label={t("thread.actions")}>
            <IconMore />
          </button>
        }
        items={[
          { label: t(pinned ? "thread.unpin" : "thread.pin"), icon: <IconPin />, onSelect: () => togglePin(session.id) },
          { label: t("thread.archive"), icon: <IconArchive />, onSelect: () => toggleArchive(session.id) },
          { label: t("thread.delete"), icon: <IconTrash />, danger: true, onSelect: () => setConfirm(true) },
        ]}
      />
      <SummaryButton />
      {!panelOpen && (
        <Tip label={t("panel.open")}>
          <button type="button" className="icon-button" aria-label={t("panel.open")} onClick={() => setOpen(true)}>
            <IconPlus />
          </button>
        </Tip>
      )}
      <Modal open={confirm} onClose={() => setConfirm(false)} title={t("thread.deleteTitle")} description={t("thread.deleteBody")} className="modal-sm">
        <div className="modal-actions">
          <button type="button" className="button" onClick={() => setConfirm(false)}>
            {t("common.cancel")}
          </button>
          <button
            type="button"
            className="button"
            data-variant="primary"
            onClick={() => {
              setConfirm(false);
              void deleteSession(session.id);
            }}
          >
            {t("thread.delete")}
          </button>
        </div>
      </Modal>
    </div>
  );
}
