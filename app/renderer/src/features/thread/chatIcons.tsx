// Icons for the chat extras, on the same 16px grid as primitives/icons.tsx.

import type { SVGProps } from "react";

type Props = SVGProps<SVGSVGElement>;

function Icon({ children, ...props }: Props) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" {...props}>
      {children}
    </svg>
  );
}

export const IconThumbUp = (p: Props) => (
  <Icon {...p}>
    <path d="M5 7.25 7.5 2a1.6 1.6 0 0 1 1.6 1.9L8.7 6h3.2a1.3 1.3 0 0 1 1.27 1.6l-.9 4.4a1.6 1.6 0 0 1-1.57 1.25H5m0-6v6m0-6H3a.75.75 0 0 0-.75.75v4.5c0 .41.34.75.75.75h2" strokeWidth="1.3" />
  </Icon>
);
export const IconThumbDown = (p: Props) => (
  <Icon {...p}>
    <path d="m5 8.75 2.5 5.25a1.6 1.6 0 0 0 1.6-1.9L8.7 10h3.2a1.3 1.3 0 0 0 1.27-1.6l-.9-4.4A1.6 1.6 0 0 0 10.7 2.75H5m0 6v-6m0 6H3a.75.75 0 0 1-.75-.75v-4.5c0-.41.34-.75.75-.75h2" strokeWidth="1.3" />
  </Icon>
);
export const IconMarkdown = (p: Props) => (
  <Icon {...p}>
    <rect x="1.75" y="3.5" width="12.5" height="9" rx="2" strokeWidth="1.3" />
    <path d="M4.25 10V6l1.75 2 1.75-2v4M10.5 6v4m-1.25-1.5 1.25 1.5 1.25-1.5" strokeWidth="1.2" />
  </Icon>
);
export const IconGrip = (p: Props) => (
  <Icon {...p}>
    <circle cx="6" cy="4" r=".9" fill="currentColor" stroke="none" />
    <circle cx="10" cy="4" r=".9" fill="currentColor" stroke="none" />
    <circle cx="6" cy="8" r=".9" fill="currentColor" stroke="none" />
    <circle cx="10" cy="8" r=".9" fill="currentColor" stroke="none" />
    <circle cx="6" cy="12" r=".9" fill="currentColor" stroke="none" />
    <circle cx="10" cy="12" r=".9" fill="currentColor" stroke="none" />
  </Icon>
);
export const IconUndo = (p: Props) => (
  <Icon {...p}>
    <path d="M5.5 3.5 2.75 6.25 5.5 9M3 6.25h5.5a4 4 0 0 1 0 8H6" strokeWidth="1.4" />
  </Icon>
);
export const IconSteer = (p: Props) => (
  <Icon {...p}>
    <path d="M2.5 8h8.25M8 4.5 11.5 8 8 11.5M13.5 3v10" strokeWidth="1.4" />
  </Icon>
);
export const IconSendNow = (p: Props) => (
  <Icon {...p}>
    <rect x="3.5" y="3.5" width="9" height="9" rx="2" fill="currentColor" stroke="none" />
  </Icon>
);
