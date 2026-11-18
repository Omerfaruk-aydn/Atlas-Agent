// A side chat: a copy of the open chat's history that goes its own way. The
// main thread never sees it; it lives here, in the panel.

import { useEffect, useRef, useState } from "react";
import { t } from "../../lib/i18n";
import type { PanelTab } from "../../lib/panel";
import { SideScope } from "../../lib/scope";
import { ensureMessages, useApp } from "../../stores/app";
import { focusSideChat } from "../../stores/panel";
import { Composer } from "../composer/Composer";
import { PermissionDock, QuestionDock } from "../composer/Docks";
import { Thread } from "../thread/Thread";

export function SideChatTab({ tab }: { tab: PanelTab }) {
  const sessionId = tab.sessionId ?? "";
  const known = useApp((s) => s.sideChats.some((x) => x.id === sessionId));
  const permission = useApp((s) => s.permissions.find((p) => p.session_id === sessionId));
  const question = useApp((s) => s.questions.find((q) => q.session_id === sessionId));
  // Taken once, when the tab first shows a chat the person just started.
  const [fresh] = useState(() => focusSideChat.has(sessionId));
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    focusSideChat.delete(sessionId);
    if (!fresh) return;
    // The panel opening with it stays inert until it is drawn, and an inert
    // field cannot take the focus: try each frame until it can, for a moment.
    let frame = 0;
    let tries = 0;
    const take = () => {
      const field = box.current?.querySelector<HTMLTextAreaElement>(".composer textarea");
      if (field && !field.closest("[inert]")) {
        field.focus();
        if (document.activeElement === field) return;
      }
      if (++tries < 60) frame = requestAnimationFrame(take);
    };
    take();
    return () => cancelAnimationFrame(frame);
  }, [sessionId, fresh]);

  useEffect(() => {
    if (sessionId) void ensureMessages(sessionId).catch(() => {});
  }, [sessionId]);

  if (!known) return <div className="rail-empty">{t("panel.sideGone")}</div>;
  return (
    <SideScope.Provider value>
      <div className="side-chat conversation" ref={box}>
        <Thread sessionId={sessionId} />
        <div className="composer-dock">
          <div className="composer-wrap">
            {permission ? (
              <PermissionDock key={permission.id} request={permission} />
            ) : question ? (
              <QuestionDock key={question.id} request={question} />
            ) : (
              <Composer sessionId={sessionId} />
            )}
          </div>
        </div>
      </div>
    </SideScope.Provider>
  );
}
