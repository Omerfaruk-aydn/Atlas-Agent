// Icons of the design tool: layer types and tools, on the app's 16px grid.

import type { ReactNode, SVGProps } from "react";
import type { NodeType } from "./model";

type Props = SVGProps<SVGSVGElement>;

function Icon({ children, ...props }: Props & { children: ReactNode }) {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" {...props}>
      {children}
    </svg>
  );
}

export const IconMove = (p: Props) => (
  <Icon {...p}>
    <path d="M3.5 2.5l9 4.2-3.9 1.3-1.4 4-3.7-9.5z" />
  </Icon>
);
export const IconHand = (p: Props) => (
  <Icon {...p}>
    <path d="M5 8.5V4.25a1 1 0 0 1 2 0V7.5M7 7V3.25a1 1 0 0 1 2 0V7M9 7V4a1 1 0 0 1 2 0v4.5M11 6.5a1 1 0 0 1 2 0V9.5a4.5 4.5 0 0 1-4.5 4.5h-.6a4 4 0 0 1-3.3-1.8L3 9.6a1 1 0 0 1 1.6-1.2L5 9" />
  </Icon>
);
export const IconFrame = (p: Props) => (
  <Icon {...p}>
    <path d="M5 2v12M11 2v12M2 5h12M2 11h12" />
  </Icon>
);
export const IconRect = (p: Props) => (
  <Icon {...p}>
    <rect x="2.75" y="2.75" width="10.5" height="10.5" rx="1" />
  </Icon>
);
export const IconEllipse = (p: Props) => (
  <Icon {...p}>
    <circle cx="8" cy="8" r="5.5" />
  </Icon>
);
export const IconLine = (p: Props) => (
  <Icon {...p}>
    <path d="M3 13L13 3" />
  </Icon>
);
export const IconPolygon = (p: Props) => (
  <Icon {...p}>
    <path d="M8 2.5l5.5 10h-11z" />
  </Icon>
);
export const IconStar = (p: Props) => (
  <Icon {...p}>
    <path d="M8 2l1.8 3.9 4.2.5-3.1 2.9.8 4.2L8 11.4l-3.7 2.1.8-4.2L2 6.4l4.2-.5z" />
  </Icon>
);
export const IconText = (p: Props) => (
  <Icon {...p}>
    <path d="M3 4V3h10v1M8 3v10M6 13h4" />
  </Icon>
);
export const IconImage = (p: Props) => (
  <Icon {...p}>
    <rect x="2.5" y="3" width="11" height="10" rx="1.5" />
    <circle cx="6" cy="6.5" r="1.2" />
    <path d="M13.5 10.5l-3-3-6 5.5" />
  </Icon>
);
export const IconGroup = (p: Props) => (
  <Icon {...p}>
    <rect x="2.5" y="2.5" width="11" height="11" rx="1.5" strokeDasharray="2 2" />
  </Icon>
);
export const IconComponent = (p: Props) => (
  <Icon {...p}>
    <path d="M8 1.75l2.4 2.4L8 6.55 5.6 4.15zM8 9.45l2.4 2.4L8 14.25l-2.4-2.4zM4.15 5.6l2.4 2.4-2.4 2.4L1.75 8zM11.85 5.6l2.4 2.4-2.4 2.4L9.45 8z" />
  </Icon>
);
export const IconInstance = (p: Props) => (
  <Icon {...p}>
    <path d="M8 2l6 6-6 6-6-6z" />
  </Icon>
);
export const IconVector = (p: Props) => (
  <Icon {...p}>
    <path d="M3 13c2-8 8-8 10-10" />
    <rect x="1.75" y="11.75" width="2.5" height="2.5" />
    <rect x="11.75" y="1.75" width="2.5" height="2.5" />
  </Icon>
);
export const IconEye = (p: Props) => (
  <Icon {...p}>
    <path d="M1.75 8S4 3.75 8 3.75 14.25 8 14.25 8 12 12.25 8 12.25 1.75 8 1.75 8z" />
    <circle cx="8" cy="8" r="1.75" />
  </Icon>
);
export const IconEyeOff = (p: Props) => (
  <Icon {...p}>
    <path d="M2.5 2.5l11 11M6.6 4a6.8 6.8 0 0 1 1.4-.25C12 3.75 14.25 8 14.25 8a11 11 0 0 1-1.6 2.1M10.4 11.8A6 6 0 0 1 8 12.25C4 12.25 1.75 8 1.75 8a11.5 11.5 0 0 1 2.4-2.9" />
  </Icon>
);
export const IconLock = (p: Props) => (
  <Icon {...p}>
    <rect x="3.5" y="7" width="9" height="6.5" rx="1.25" />
    <path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2" />
  </Icon>
);
export const IconUnlock = (p: Props) => (
  <Icon {...p}>
    <rect x="3.5" y="7" width="9" height="6.5" rx="1.25" />
    <path d="M5.5 7V5a2.5 2.5 0 0 1 4.9-.7" />
  </Icon>
);
export const IconUndo = (p: Props) => (
  <Icon {...p}>
    <path d="M5.5 3.5L2.5 6.5l3 3M2.5 6.5h7a4 4 0 0 1 0 8H7" />
  </Icon>
);
export const IconRedo = (p: Props) => (
  <Icon {...p}>
    <path d="M10.5 3.5l3 3-3 3M13.5 6.5h-7a4 4 0 0 0 0 8H9" />
  </Icon>
);
export const IconAlign = {
  left: (p: Props) => (
    <Icon {...p}>
      <path d="M2.5 2v12M5 5h8M5 11h5" />
    </Icon>
  ),
  hcenter: (p: Props) => (
    <Icon {...p}>
      <path d="M8 2v12M3.5 5h9M5 11h6" />
    </Icon>
  ),
  right: (p: Props) => (
    <Icon {...p}>
      <path d="M13.5 2v12M3 5h8M6 11h5" />
    </Icon>
  ),
  top: (p: Props) => (
    <Icon {...p}>
      <path d="M2 2.5h12M5 5v8M11 5v5" />
    </Icon>
  ),
  vcenter: (p: Props) => (
    <Icon {...p}>
      <path d="M2 8h12M5 3.5v9M11 5v6" />
    </Icon>
  ),
  bottom: (p: Props) => (
    <Icon {...p}>
      <path d="M2 13.5h12M5 3v8M11 6v5" />
    </Icon>
  ),
};
export const IconDistH = (p: Props) => (
  <Icon {...p}>
    <path d="M2.5 2v12M13.5 2v12M6.5 5v6h3V5z" />
  </Icon>
);
export const IconDistV = (p: Props) => (
  <Icon {...p}>
    <path d="M2 2.5h12M2 13.5h12M5 6.5h6v3H5z" />
  </Icon>
);

export const IconSection = (p: Props) => (
  <Icon {...p}>
    <path d="M2 4.5h5M2 4.5v8.5h12V6.5H9.5" />
    <path d="M4.5 9.5h7" />
  </Icon>
);
export const IconSlice = (p: Props) => (
  <Icon {...p}>
    <path d="M3 5.5V2.5h3M13 5.5V2.5h-3M3 10.5v3h3M13 10.5v3h-3" />
    <path d="M6 8h4" />
  </Icon>
);
export const IconEyedropper = (p: Props) => (
  <Icon {...p}>
    <path d="M10.5 2.5l3 3-2 2-3-3zM9 5l-6 6v2.5h2.5L11.5 7.5" />
  </Icon>
);

const BY_TYPE: Record<NodeType, (p: Props) => ReactNode> = {
  section: IconSection,
  slice: IconSlice,
  frame: IconFrame,
  group: IconGroup,
  rect: IconRect,
  ellipse: IconEllipse,
  line: IconLine,
  polygon: IconPolygon,
  star: IconStar,
  path: IconVector,
  text: IconText,
  image: IconImage,
  component: IconComponent,
  instance: IconInstance,
};

export function TypeIcon({ type }: { type: NodeType }) {
  const I = BY_TYPE[type];
  return (
    <span className="design-type" data-type={type}>
      <I />
    </span>
  );
}

export const IconPenTool = (p: Props) => (
  <Icon {...p}>
    <path d="M8 2.5l4.5 4-2 5.5H5.5l-2-5.5 4.5-4zM8 12v1.5M6.5 13.5h3" />
    <circle cx="8" cy="7" r="1" />
  </Icon>
);
export const IconPencil = (p: Props) => (
  <Icon {...p}>
    <path d="M10.5 3l2.5 2.5-7 7L3 13l.5-3 7-7zM9 4.5L11.5 7" />
  </Icon>
);

export const IconComment = (p: Props) => (
  <Icon {...p}>
    <path d="M3 3.5h10v7H7.5L4.5 13v-2.5H3z" />
  </Icon>
);
