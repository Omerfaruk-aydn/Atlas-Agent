// Line icons drawn on a 16px grid with a 1.5px stroke.

import type { JSX } from "solid-js"

const base = (size: number, children: JSX.Element) => (
  <svg width={size} height={size} viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    {children}
  </svg>
)

export const Icon = {
  plus: (s = 16) => base(s, <path d="M8 3.5v9M3.5 8h9" />),
  close: (s = 16) => base(s, <path d="M4 4l8 8M12 4l-8 8" />),
  send: (s = 16) => base(s, <path d="M8 12.5v-9M4 7.5l4-4 4 4" />),
  stop: (s = 16) => (
    <svg width={s} height={s} viewBox="0 0 16 16" aria-hidden="true">
      <rect x="4" y="4" width="8" height="8" rx="2" fill="currentColor" />
    </svg>
  ),
  mic: (s = 16) => base(s, <><rect x="5.75" y="1.75" width="4.5" height="8" rx="2.25" /><path d="M3.25 7.5a4.75 4.75 0 009.5 0M8 12.25v2" /></>),
  speaker: (s = 16) => base(s, <><path d="M2.5 6v4h2.5l3.5 3V3L5 6z" /><path d="M11 5.5a3.5 3.5 0 010 5M12.75 3.75a6 6 0 010 8.5" /></>),
  mute: (s = 16) => base(s, <><path d="M2.5 6v4h2.5l3.5 3V3L5 6z" /><path d="M11 6l3.5 3.5M14.5 6L11 9.5" /></>),
  gear: (s = 16) => base(s, <><circle cx="8" cy="8" r="2.25" /><path d="M8 1.75v1.5M8 12.75v1.5M14.25 8h-1.5M3.25 8h-1.5M12.4 3.6l-1.06 1.06M4.66 11.34L3.6 12.4M12.4 12.4l-1.06-1.06M4.66 4.66L3.6 3.6" /></>),
  shield: (s = 16) => base(s, <path d="M8 1.75l5.25 1.9v3.9c0 3.1-2.2 5.6-5.25 6.7-3.05-1.1-5.25-3.6-5.25-6.7v-3.9z" />),
  bolt: (s = 16) => base(s, <path d="M8.75 1.75L3.5 9h4l-.75 5.25L12.5 7h-4z" />),
  activity: (s = 16) => base(s, <path d="M1.75 8h2.75l1.75-4.5 3.5 9 1.75-4.5h2.75" />),
  folder: (s = 16) => base(s, <path d="M1.75 4.25c0-.55.45-1 1-1h3.1l1.4 1.5h6c.55 0 1 .45 1 1v6.5c0 .55-.45 1-1 1H2.75c-.55 0-1-.45-1-1z" />),
  file: (s = 16) => base(s, <><path d="M4 1.75h5l3 3v9.5H4z" /><path d="M9 1.75v3h3" /></>),
  user: (s = 16) => base(s, <><circle cx="8" cy="5.5" r="2.75" /><path d="M2.75 14c.6-2.6 2.75-4 5.25-4s4.65 1.4 5.25 4" /></>),
  sparkle: (s = 16) => base(s, <path d="M8 2v3M8 11v3M2 8h3M11 8h3M4 4l1.5 1.5M10.5 10.5L12 12M12 4l-1.5 1.5M5.5 10.5L4 12" />),
  voice: (s = 16) => base(s, <path d="M2.5 6.5v3M5 4.5v7M7.75 2.5v11M10.5 5v6M13.25 6.75v2.5" />),
  play: (s = 16) => (
    <svg width={s} height={s} viewBox="0 0 16 16" aria-hidden="true">
      <path d="M5 3.5v9l7.5-4.5z" fill="currentColor" />
    </svg>
  ),
  check: (s = 16) => base(s, <path d="M3 8.5l3 3 7-7" />),
  clock: (s = 16) => base(s, <><circle cx="8" cy="8" r="6.25" /><path d="M8 4.75V8l2.25 1.5" /></>),
  trash: (s = 16) => base(s, <path d="M2.75 4.25h10.5M6.25 4.25V2.75h3.5v1.5M4 4.25l.6 9h6.8l.6-9" />),
  menu: (s = 16) => base(s, <path d="M2.5 4.5h11M2.5 8h11M2.5 11.5h11" />),
  sliders: (s = 16) => base(s, <path d="M2.5 4.5h6M11.5 4.5h2M2.5 11.5h2M7.5 11.5h6M10 3v3M6 10v3" />),
  key: (s = 16) => base(s, <><circle cx="5.25" cy="10.75" r="2.75" /><path d="M7.25 8.75l6-6M11 5l1.75 1.75M9.5 6.5l1.25 1.25" /></>),
  panel: (s = 16) => base(s, <><rect x="1.75" y="2.75" width="12.5" height="10.5" rx="1.5" /><path d="M10 2.75v10.5" /></>),
  clip: (s = 16) => base(s, <path d="M13 7.5l-5.1 5.1a3.1 3.1 0 01-4.4-4.4l5.6-5.6a2.05 2.05 0 012.9 2.9L6.5 11a1 1 0 01-1.45-1.45L9.75 4.85" />),
  pen: (s = 16) => base(s, <path d="M10.25 2.75l3 3-7.75 7.75H2.5v-3zM8.75 4.25l3 3" />),
  copy: (s = 16) => base(s, <><rect x="5.25" y="5.25" width="8" height="8" rx="1.5" /><path d="M10.75 5.25V3.75c0-.55-.45-1-1-1h-6c-.55 0-1 .45-1 1v6c0 .55.45 1 1 1h1.5" /></>),
  retry: (s = 16) => base(s, <><path d="M13.25 8a5.25 5.25 0 11-1.55-3.7" /><path d="M13.25 2.5v3h-3" /></>),
  share: (s = 16) => base(s, <><path d="M9.5 3.25h3.25V6.5M12.75 3.25L7.5 8.5" /><path d="M11.25 9.5v3.25H3.25V4.75H6.5" /></>),
  search: (s = 16) => base(s, <><circle cx="7" cy="7" r="4.25" /><path d="M10.25 10.25l3.25 3.25" /></>),
  brain: (s = 16) => base(s, <><path d="M8 3.25c-.8-1.2-3.3-1.1-3.75.6-1.6.2-2.3 2.1-1.2 3.15-.9 1.1-.4 2.9 1 3.2.2 1.9 2.6 2.6 3.95 1.3" /><path d="M8 3.25c.8-1.2 3.3-1.1 3.75.6 1.6.2 2.3 2.1 1.2 3.15.9 1.1.4 2.9-1 3.2-.2 1.9-2.6 2.6-3.95 1.3V3.25z" /></>),
  bell: (s = 16) => base(s, <><path d="M4 11.25V7.5a4 4 0 018 0v3.75l1.25 1.25H2.75z" /><path d="M6.5 13.75a1.6 1.6 0 003 0" /></>),
  eye: (s = 16) => base(s, <><path d="M1.75 8s2.25-4.25 6.25-4.25S14.25 8 14.25 8 12 12.25 8 12.25 1.75 8 1.75 8z" /><circle cx="8" cy="8" r="1.75" /></>),
  globe: (s = 16) => base(s, <><circle cx="8" cy="8" r="6.25" /><path d="M1.75 8h12.5M8 1.75c1.7 1.8 2.5 3.9 2.5 6.25S9.7 12.45 8 14.25C6.3 12.45 5.5 10.35 5.5 8S6.3 3.55 8 1.75z" /></>),
  webhook: (s = 16) => base(s, <><circle cx="4.25" cy="11.5" r="1.75" /><circle cx="11.75" cy="11.5" r="1.75" /><circle cx="8" cy="4.5" r="1.75" /><path d="M6 11.5h3.9M7.1 6l-2 3.9M8.9 6l2 3.9" /></>),
  terminal: (s = 16) => base(s, <><rect x="1.75" y="2.75" width="12.5" height="10.5" rx="1.5" /><path d="M4 6.25l2.25 1.75L4 9.75M8 10h4" /></>),
}
