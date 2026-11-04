import { t, type Key } from "../../lib/i18n";
import { IconBranch, IconChevron, IconFolder } from "../../primitives/icons";
import { Menu } from "../../primitives/ui";
import { useApp } from "../../stores/app";
import { Composer } from "../composer/Composer";
import { Wordmark } from "./Wordmark";

const PROMPTS: { label: Key; prompt: Key }[] = [
  { label: "home.plan", prompt: "home.plan.prompt" },
  { label: "home.review", prompt: "home.review.prompt" },
  { label: "home.tests", prompt: "home.tests.prompt" },
];

function ContextLine() {
  const workspace = useApp((s) => s.workspace);
  const git = useApp((s) => s.git);
  const projects = useApp((s) => s.projects);
  const pick = async () => {
    const folder = await window.atlas?.pickFolder();
    if (folder) await window.atlas?.openProject(folder);
  };
  return (
    <div className="home-context">
      <Menu
        side="bottom"
        trigger={
          <button type="button" className="chip-button">
            <IconFolder />
            <span>{workspace?.name}</span>
          </button>
        }
        items={[
          { label: t("sidebar.openFolder"), onSelect: () => void pick() },
          ...projects.slice(0, 6).map((p) => ({ label: p.name, hint: p.path, onSelect: () => void window.atlas?.openProject(p.path) })),
        ]}
      />
      {git && (
        <span className="chip-button" data-static="">
          <IconBranch />
          <span>{git.branch}</span>
        </span>
      )}
    </div>
  );
}

/** The new-chat screen. */
export function Home() {
  const configured = useApp((s) => (s.catalog?.providers?.length ?? 0) > 0);
  const hasCatalog = useApp((s) => s.catalog !== null);
  const notice = useApp((s) => s.notice);
  const insert = useApp((s) => s.insertInComposer);
  const openSettings = useApp((s) => s.openSettings);
  const lastSession = useApp((s) => s.sessions.find((x) => !s.ui.archived.includes(x.id)));
  const select = useApp((s) => s.selectSession);
  return (
    <div className="home">
      <div className="home-center">
        <div className="home-mark">
          <Wordmark />
        </div>
        <div className="composer-wrap">
          <ContextLine />
          <Composer sessionId={null} autoFocus />
          <div className="home-pills">
            {PROMPTS.map((p) => (
              <button key={p.label} type="button" className="pill" onClick={() => insert(t(p.prompt))}>
                {t(p.label)}
              </button>
            ))}
          </div>
        </div>
        {notice && <div className="notice">{notice}</div>}
        {hasCatalog && !configured ? (
          <button type="button" className="link-button home-link" onClick={() => openSettings("models")}>
            {t("home.connect")}
            <IconChevron />
          </button>
        ) : (
          lastSession && (
            <button type="button" className="link-button home-link" onClick={() => void select(lastSession.id)}>
              {t("home.resume")}
              <IconChevron />
            </button>
          )
        )}
      </div>
    </div>
  );
}

/** Shown when the app was opened without a project. */
export function PickProject() {
  const projects = useApp((s) => s.projects);
  const open = async () => {
    const folder = await window.atlas?.pickFolder();
    if (folder) await window.atlas?.openProject(folder);
  };
  return (
    <div className="pick">
      <div className="pick-card">
        <h1>{t("home.pick.title")}</h1>
        <p>{t("home.pick.body")}</p>
        <button type="button" className="button" data-variant="primary" onClick={() => void open()}>
          <IconFolder />
          {t("home.pick.open")}
        </button>
        {projects.length > 0 && (
          <div className="recent">
            <div className="group-label">{t("home.recent")}</div>
            {projects.slice(0, 8).map((p) => (
              <button key={p.path} type="button" className="recent-row" title={p.path} onClick={() => void window.atlas?.openProject(p.path)}>
                <span>{p.name}</span>
                <span className="recent-path">{p.path}</span>
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

export function Failed() {
  const error = useApp((s) => s.error);
  const init = useApp((s) => s.init);
  return (
    <div className="pick">
      <div className="pick-card">
        <h1>{t("app.failed")}</h1>
        <p>{error}</p>
        <button type="button" className="button" onClick={() => void init()}>
          {t("app.retry")}
        </button>
      </div>
    </div>
  );
}
