// 16px grid, 1.5px stroke, drawn for Atlas.

import type { ReactNode, SVGProps } from "react";

type Props = SVGProps<SVGSVGElement>;

function Icon({ children, ...props }: Props & { children: ReactNode }) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" {...props}>
      {children}
    </svg>
  );
}

export const IconSidebar = (p: Props) => (
  <Icon {...p}>
    <rect x="2" y="3" width="12" height="10" rx="2" />
    <path d="M6.5 3v10" />
  </Icon>
);
export const IconPlus = (p: Props) => (
  <Icon {...p}>
    <path d="M8 3.5v9M3.5 8h9" />
  </Icon>
);
export const IconMic = (p: Props) => (
  <Icon {...p}><rect x="5.75" y="1.75" width="4.5" height="8" rx="2.25" fill="currentColor" stroke="none" /><path d="M3.75 7.5v.75a4.25 4.25 0 0 0 8.5 0V7.5M8 12.5v1.75M5.75 14.25h4.5" strokeWidth="1.35" /></Icon>
);
export const IconWave = (p: Props) => (
  <Icon {...p}><path d="M2 6.5v3M4.4 4.25v7.5M6.8 2.5v11M9.2 5v6M11.6 3.5v9M14 6v4" strokeWidth="1.4" /></Icon>
);
export const IconPlug = (p: Props) => (
  <Icon {...p}><path d="m3 10 5.5-5.5a2.1 2.1 0 0 1 3 3L7 12a2.1 2.1 0 0 1-3-3l5.5-5.5a2.1 2.1 0 0 1 3 3L9 10M9 10l2 2-1.5 1.5" strokeWidth="1.35" /></Icon>
);
export const IconPlan = (p: Props) => (
  <Icon {...p}><circle cx="3.25" cy="3.25" r="1.25" /><rect x="2" y="7" width="2.5" height="2.5" rx=".55" /><rect x="2" y="12" width="2.5" height="2.5" rx=".55" /><path d="M7.25 3.25H14M7.25 8.25h4.5M7.25 13.25H14" strokeWidth="1.35" /></Icon>
);
export const IconDebug = (p: Props) => (
  <Icon {...p}><path d="M5.25 5a2.75 2.75 0 0 1 5.5 0v5a2.75 2.75 0 0 1-5.5 0V5Z" /><path d="m5.5 1.75 1.25 1.5m3.75-1.5-1.25 1.5M2 5l3.25 1M14 5l-3.25 1M1.75 9h3.5m5.5 0h3.5M3 13l2.75-1.75M13 13l-2.75-1.75M5.5 6.5h5M8 7v5" strokeWidth="1.25" /></Icon>
);
export const IconMultitask = (p: Props) => (
  <Icon {...p}><path d="M10.5 9.5A4 4 0 1 0 4 5.5" /><path d="M5.5 6.5A4 4 0 1 0 12 10.5" /><path d="m6 6 4 4" strokeWidth="1.35" /></Icon>
);
export const IconAsk = (p: Props) => (
  <Icon {...p}><path d="M8 2.25a5.25 5.25 0 0 0-4.5 8l-.75 3.25L6 12.75A5.25 5.25 0 1 0 8 2.25Z" strokeWidth="1.35" /><path d="M6.6 6.1a1.4 1.4 0 1 1 2.8 0c0 1-1.4 1.15-1.4 2.2" strokeWidth="1.2" /><circle cx="8" cy="10.2" r=".65" fill="currentColor" stroke="none" /></Icon>
);
export const IconLaptop = (p: Props) => (
  <Icon {...p}><rect x="3" y="2.75" width="10" height="7.5" rx="1" /><path d="M3 10.25 1.5 13v.25h13V13L13 10.25M6.25 11.75h3.5" strokeWidth="1.25" /></Icon>
);
export const IconSkill = (p: Props) => (
  <Icon {...p}><path d="m8 1.75 5.5 3.1v6.3L8 14.25l-5.5-3.1v-6.3L8 1.75Z" strokeWidth="1.3" /><path d="M2.75 5 8 8l5.25-3M8 8v6M5.25 3.4l5.5 3.15" strokeWidth="1.2" /></Icon>
);
export const IconCommand = (p: Props) => (
  <Icon {...p}><rect x="1.75" y="2.75" width="12.5" height="10.5" rx="2" strokeWidth="1.3" /><path d="m4.5 6 2 2-2 2M8.5 10h3" strokeWidth="1.35" /></Icon>
);
export const IconSearch = (p: Props) => (
  <Icon {...p}>
    <circle cx="7" cy="7" r="4" />
    <path d="m10 10 3 3" />
  </Icon>
);
export const IconChevron = (p: Props) => (
  <Icon {...p}>
    <path d="m6 4 4 4-4 4" />
  </Icon>
);
export const IconChevronDown = (p: Props) => (
  <Icon {...p}>
    <path d="m4 6 4 4 4-4" />
  </Icon>
);
export const IconArrowUp = (p: Props) => (
  <Icon {...p}>
    <path d="M8 13V3.5M4 7l4-4 4 4" />
  </Icon>
);
export const IconArrowDown = (p: Props) => (
  <Icon {...p}>
    <path d="M8 3v9.5M4 9l4 4 4-4" />
  </Icon>
);
export const IconStop = (p: Props) => (
  <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" {...p}>
    <rect x="3" y="3" width="10" height="10" rx="2" fill="currentColor" />
  </svg>
);
export const IconFolder = (p: Props) => (
  <Icon {...p}>
    <path d="M2 4.5A1.5 1.5 0 0 1 3.5 3h2.6l1.5 1.8h4.9A1.5 1.5 0 0 1 14 6.3v5.2a1.5 1.5 0 0 1-1.5 1.5h-9A1.5 1.5 0 0 1 2 11.5z" />
  </Icon>
);
export const IconCopy = (p: Props) => (
  <Icon {...p}>
    <rect x="5.5" y="5.5" width="8" height="8" rx="1.5" />
    <path d="M10.5 3.5A1.5 1.5 0 0 0 9 2.5H4A1.5 1.5 0 0 0 2.5 4v5a1.5 1.5 0 0 0 1 1.4" />
  </Icon>
);
export const IconCheck = (p: Props) => (
  <Icon {...p}>
    <path d="m3.5 8.5 3 3 6-7" />
  </Icon>
);
export const IconTrash = (p: Props) => (
  <Icon {...p}>
    <path d="M3 4.5h10M6.5 4.5V3h3v1.5M4.5 4.5l.5 8.5h6l.5-8.5" />
  </Icon>
);
export const IconAlert = (p: Props) => (
  <Icon {...p}>
    <circle cx="8" cy="8" r="5.5" />
    <path d="M8 5v3.5M8 11h.01" />
  </Icon>
);
export const IconBan = (p: Props) => (
  <Icon {...p}>
    <circle cx="8" cy="8" r="5.5" />
    <path d="m4.2 4.2 7.6 7.6" />
  </Icon>
);
export const IconSun = (p: Props) => (
  <Icon {...p}>
    <circle cx="8" cy="8" r="2.75" />
    <path d="M8 1.75v1.5M8 12.75v1.5M1.75 8h1.5M12.75 8h1.5M3.6 3.6l1 1M11.4 11.4l1 1M3.6 12.4l1-1M11.4 4.6l1-1" />
  </Icon>
);
export const IconMoon = (p: Props) => (
  <Icon {...p}>
    <path d="M13.5 9.6A5.75 5.75 0 0 1 6.4 2.5a5.75 5.75 0 1 0 7.1 7.1" />
  </Icon>
);
export const IconShield = (p: Props) => (
  <Icon {...p}>
    <path d="M8 2 3 3.8v4C3 11 5.2 13.2 8 14c2.8-.8 5-3 5-6.2v-4z" />
  </Icon>
);
export const IconGear = (p: Props) => (
  <Icon {...p}>
    <g transform="scale(0.6667)" strokeWidth="2.1">
      <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
      <circle cx="12" cy="12" r="3" />
    </g>
  </Icon>
);
export const IconPanelRight = (p: Props) => (
  <Icon {...p}>
    <rect x="2" y="3" width="12" height="10" rx="2" />
    <path d="M9.5 3v10" />
  </Icon>
);
export const IconBranch = (p: Props) => (
  <Icon {...p}>
    <circle cx="4.5" cy="3.5" r="1.5" />
    <circle cx="4.5" cy="12.5" r="1.5" />
    <circle cx="11.5" cy="5" r="1.5" />
    <path d="M4.5 5v6M11.5 6.5c0 2.5-3 2.5-7 3.5" />
  </Icon>
);
export const IconPin = (p: Props) => (
  <Icon {...p}>
    <path d="M9.5 2.5 13.5 6.5 11 7.5 8.5 11 5 7.5 8.5 5zM6.5 9.5 3 13" />
  </Icon>
);
export const IconArchive = (p: Props) => (
  <Icon {...p}>
    <rect x="2.5" y="3" width="11" height="3" rx="1" />
    <path d="M3.5 6v6a1 1 0 0 0 1 1h7a1 1 0 0 0 1-1V6M6.5 8.75h3" />
  </Icon>
);
export const IconPencil = (p: Props) => (
  <Icon {...p}>
    <path d="m3 13 .6-2.6 7.2-7.2a1.4 1.4 0 0 1 2 2l-7.2 7.2zM9.8 4.2l2 2" />
  </Icon>
);
export const IconPaperclip = (p: Props) => (
  <Icon {...p}>
    <path d="m13 7.5-5.2 5.2a3 3 0 0 1-4.3-4.3l5.4-5.3a2 2 0 0 1 2.8 2.8l-5.3 5.3a1 1 0 0 1-1.4-1.4l4.8-4.8" />
  </Icon>
);
export const IconMinus = (p: Props) => (
  <Icon {...p}>
    <path d="M3.5 8h9" />
  </Icon>
);
export const IconReset = (p: Props) => (
  <Icon {...p}>
    <path d="M2.75 8a5.25 5.25 0 1 0 1.6-3.78L2.75 5.75" />
    <path d="M2.75 2.75v3h3" />
  </Icon>
);
export const IconClose = (p: Props) => (
  <Icon {...p}>
    <path d="m4 4 8 8M12 4l-8 8" />
  </Icon>
);
export const IconRetry = (p: Props) => (
  <Icon {...p}>
    <path d="M13 8a5 5 0 1 1-1.5-3.5M13 2.5V5h-2.5" />
  </Icon>
);
export const IconRewind = (p: Props) => (
  <Icon {...p}>
    <path d="M3 8a5 5 0 1 0 1.5-3.5M3 2.5V5h2.5M8 5.5V8l1.75 1.25" />
  </Icon>
);
export const IconFile = (p: Props) => (
  <Icon {...p}>
    <path d="M4 2.5h5l3 3v8H4zM9 2.5v3h3" />
  </Icon>
);
export const IconTerminal = (p: Props) => (
  <Icon {...p}>
    <rect x="2" y="3" width="12" height="10" rx="2" />
    <path d="m5 6.5 2 1.5-2 1.5M8.5 10H11" />
  </Icon>
);
export const IconDiff = (p: Props) => (
  <Icon {...p}>
    <path d="M8 2.5v5M5.5 5h5M5.5 11h5M4 13.5h8" />
  </Icon>
);
export const IconGlobe = (p: Props) => (
  <Icon {...p}>
    <circle cx="8" cy="8" r="5.5" />
    <path d="M2.5 8h11M8 2.5c1.7 1.6 2.5 3.4 2.5 5.5S9.7 11.9 8 13.5C6.3 11.9 5.5 10.1 5.5 8S6.3 4.1 8 2.5" />
  </Icon>
);
export const IconSparkle = (p: Props) => (
  <Icon {...p}>
    <path d="M8 2.5 9.3 6.7 13.5 8 9.3 9.3 8 13.5 6.7 9.3 2.5 8 6.7 6.7z" />
  </Icon>
);
export const IconKey = (p: Props) => (
  <Icon {...p}>
    <circle cx="5.5" cy="10.5" r="3" />
    <path d="m7.7 8.3 5.3-5.3M10.5 5.5l2 2" />
  </Icon>
);
/** The Atlas mark: an A carrying the arc of the sky. */
export const AtlasMark = ({ draw, ...p }: Props & { draw?: boolean }) => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" data-draw={draw || undefined} className="atlas-mark" {...p}>
    <path d="M5 21 12 8l7 13" pathLength="1" />
    <path d="M8.5 16h7" pathLength="1" />
    <path data-slot="sky" d="M4 7.5a8.5 8.5 0 0 1 16 0" pathLength="1" stroke="var(--atlas-ember-500)" />
  </svg>
);

// Side panel and summary popover.
export const IconArrowLeft = (p: Props) => (
  <Icon {...p}>
    <path d="M13 8H3.5M7.5 3.75 3.25 8l4.25 4.25" />
  </Icon>
);
export const IconArrowRight = (p: Props) => (
  <Icon {...p}>
    <path d="M3 8h9.5M8.5 3.75 12.75 8 8.5 12.25" />
  </Icon>
);
export const IconExpand = (p: Props) => (
  <Icon {...p}>
    <path d="M9.5 2.75h3.75v3.75M13.25 2.75 9 7M6.5 13.25H2.75V9.5M2.75 13.25 7 9" />
  </Icon>
);
export const IconShrink = (p: Props) => (
  <Icon {...p}>
    <path d="M13 6.25H9.75V3M9.75 6.25 13.5 2.5M3 9.75h3.25V13M6.25 9.75 2.5 13.5" />
  </Icon>
);
export const IconMore = (p: Props) => (
  <Icon {...p}>
    <circle cx="3.5" cy="8" r="1" fill="currentColor" stroke="none" />
    <circle cx="8" cy="8" r="1" fill="currentColor" stroke="none" />
    <circle cx="12.5" cy="8" r="1" fill="currentColor" stroke="none" />
  </Icon>
);
export const IconExternal = (p: Props) => (
  <Icon {...p}>
    <path d="M9 2.75h4.25V7M13.25 2.75 7.5 8.5M11.25 9.5v2.25a1.5 1.5 0 0 1-1.5 1.5H4.25a1.5 1.5 0 0 1-1.5-1.5v-5.5a1.5 1.5 0 0 1 1.5-1.5H6.5" />
  </Icon>
);
export const IconChat = (p: Props) => (
  <Icon {...p}>
    <path d="M13.25 7.5a5 4.5 0 0 1-7.4 3.95L2.75 12.5l.85-2.6A4.5 4.5 0 0 1 3 7.5a5 4.5 0 0 1 10.25 0Z" />
  </Icon>
);
export const IconSummary = (p: Props) => (
  <Icon {...p}>
    <rect x="2.25" y="2.75" width="3" height="3" rx="0.9" />
    <path d="M7.5 4.25h6M2.5 10.25l1.1 1.1 2-2.1M7.5 10.75h6" />
  </Icon>
);
export const IconLink = (p: Props) => (
  <Icon {...p}>
    <path d="M6.75 9.25a2.5 2.5 0 0 0 3.5 0l2-2a2.5 2.5 0 0 0-3.5-3.5l-.6.6M9.25 6.75a2.5 2.5 0 0 0-3.5 0l-2 2a2.5 2.5 0 0 0 3.5 3.5l.6-.6" />
  </Icon>
);

// Browser toolbar and views.
export const IconDownload = (p: Props) => (
  <Icon {...p}>
    <path d="M8 2.5v7.25M5 7l3 3 3-3M3 12.75h10" />
  </Icon>
);
export const IconPuzzle = (p: Props) => (
  <Icon {...p}>
    <path d="M6.5 3.5a1.5 1.5 0 0 1 3 0V5h2.25c.55 0 1 .45 1 1v2h-1.5a1.5 1.5 0 0 0 0 3h1.5v2c0 .55-.45 1-1 1H9v-1.5a1.5 1.5 0 0 0-3 0V14H3.75c-.55 0-1-.45-1-1V6c0-.55.45-1 1-1H6.5Z" />
  </Icon>
);
export const IconKey2 = (p: Props) => (
  <Icon {...p}>
    <circle cx="5.25" cy="10.75" r="2.5" />
    <path d="M7 9l5.75-5.75M10.75 5.25l1.5 1.5M9.25 6.75l1.25 1.25" />
  </Icon>
);
export const IconNote = (p: Props) => (
  <Icon {...p}>
    <path d="M3.5 2.75h9a.75.75 0 0 1 .75.75v6.25l-3.5 3.5H3.5a.75.75 0 0 1-.75-.75v-9a.75.75 0 0 1 .75-.75Z" />
    <path d="M9.75 13.25v-2.5a1 1 0 0 1 1-1h2.5M5.25 6h5.5M5.25 8.5h3" />
  </Icon>
);
export const IconHistory = (p: Props) => (
  <Icon {...p}>
    <path d="M2.75 8a5.25 5.25 0 1 0 1.6-3.75M2.75 2.75v2.5h2.5M8 5v3.25l2 1.25" />
  </Icon>
);
export const IconPrint = (p: Props) => (
  <Icon {...p}>
    <path d="M4.5 6V2.75h7V6M4.5 11.5h-1a.75.75 0 0 1-.75-.75v-3.5A.75.75 0 0 1 3.5 6.5h9a.75.75 0 0 1 .75.75v3.5a.75.75 0 0 1-.75.75h-1M4.5 9.5h7v3.75h-7Z" />
  </Icon>
);
export const IconDevice = (p: Props) => (
  <Icon {...p}>
    <rect x="4.5" y="2" width="7" height="12" rx="1.5" />
    <path d="M7 12h2" />
  </Icon>
);
export const IconCamera = (p: Props) => (
  <Icon {...p}>
    <path d="M2.75 5.25h2l1-1.5h4.5l1 1.5h2a.75.75 0 0 1 .75.75v6.25a.75.75 0 0 1-.75.75H2.75A.75.75 0 0 1 2 12.25V6a.75.75 0 0 1 .75-.75Z" />
    <circle cx="8" cy="8.75" r="2.25" />
  </Icon>
);
export const IconEye = (p: Props) => (
  <Icon {...p}>
    <path d="M1.75 8S4 3.75 8 3.75 14.25 8 14.25 8 12 12.25 8 12.25 1.75 8 1.75 8Z" />
    <circle cx="8" cy="8" r="1.75" />
  </Icon>
);
export const IconPause = (p: Props) => (
  <Icon {...p}>
    <path d="M5.75 3.5v9M10.25 3.5v9" />
  </Icon>
);
export const IconPlay = (p: Props) => (
  <Icon {...p}>
    <path d="M5 3.5v9l7.25-4.5Z" />
  </Icon>
);
export const IconChevronUp = (p: Props) => (
  <Icon {...p}>
    <path d="m4 9.75 4-4 4 4" />
  </Icon>
);
export const IconSearch2 = IconSearch;
export const IconList = (p: Props) => (
  <Icon {...p}>
    <path d="M3 4.5h10M3 8h10M3 11.5h10" />
  </Icon>
);
export const IconDriveFile = (p: Props) => (
  <Icon {...p}>
    <path d="M4 2.5h5l3 3v8H4Z" />
    <path d="M9 2.5v3h3" />
  </Icon>
);

/** Background jobs: a short list of running rows, each with its own marker. */
export const IconJobs = (p: Props) => (
  <Icon {...p}>
    <circle cx="3.75" cy="4.5" r="0.9" />
    <path d="M6.5 4.5h6.25" />
    <circle cx="3.75" cy="8" r="0.9" />
    <path d="M6.5 8h6.25" />
    <circle cx="3.75" cy="11.5" r="0.9" />
    <path d="M6.5 11.5h3.5" />
  </Icon>
);

/** An address: a pin over a short line. */
export const IconMapPin = (p: Props) => (
  <Icon {...p}>
    <path d="M8 13.5s4-3.5 4-7a4 4 0 1 0-8 0c0 3.5 4 7 4 7Z" />
    <circle cx="8" cy="6.5" r="1.4" />
  </Icon>
);

/** Wrap long lines: a line that turns back on itself. */
export const IconWrap = (p: Props) => (
  <Icon {...p}>
    <path d="M2.5 4h11" />
    <path d="M2.5 8h8.5a2 2 0 0 1 0 4H8" />
    <path d="m9.5 10.5-1.5 1.5 1.5 1.5" />
    <path d="M2.5 12h2.5" />
  </Icon>
);
export const IconPen = (p: Props) => (
  <Icon {...p}>
    <path d="M8 2.25l4 6.25-4 5.25-4-5.25z" />
    <circle cx="8" cy="8.25" r="1.1" />
    <path d="M8 2.25v4.9M5.5 13.75h5" />
  </Icon>
);
