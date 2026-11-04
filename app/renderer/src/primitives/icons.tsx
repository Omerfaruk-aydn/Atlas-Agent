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
