# Android (Material 3, Pixel 8: 412 x 915 dp)

Written for the studio from Google's published Material 3 guidelines.

Frame
- Status bar about 24 to 40 dp; gesture navigation bar 24 dp. Edge-to-edge is the default: content draws under both and insets keep controls clear.
- Margins 16 dp on compact widths. Use a 4 dp grid.
- Top app bar: small 64 dp, medium 112 dp, large 152 dp, title left aligned. Collapsing on scroll.
- Navigation bar (bottom): 80 dp, three to five destinations, icon 24 dp over a 12 dp label; the selected destination sits in a 64 x 32 dp pill indicator in the secondary container colour.
- Floating action button 56 dp with a 16 dp corner (extended FAB has a label); one per screen, bottom right.

Controls
- Minimum touch target 48 x 48 dp.
- Buttons 40 dp high, fully rounded; filled for the main action, tonal or outlined for the rest, text for tertiary.
- List items 56 dp (one line), 72 dp (two lines), 88 dp (three lines), leading 40 dp avatar or 24 dp icon.
- Dialogs 28 dp radius, snackbars at the bottom above the navigation bar, bottom sheets with a drag handle.
- Chips 32 dp high, 8 dp radius; text fields 56 dp.

Shape and colour
- Shape scale: extra small 4, small 8, medium 12, large 16, extra large 28, full. Components pick one step; do not mix.
- Colour is tonal: primary, secondary, tertiary each with container and on-container roles; surfaces use five container tones rather than shadows. Elevation is a tint, not a grey drop shadow.
- State layers: hover 8 percent, focus and pressed 10 percent of the on-colour over the surface.

Type
- Display 57/45/36, Headline 32/28/24, Title 22/16/14 (medium weight for the last two), Body 16/14/12, Label 14/12/11. Roboto Flex or a similar neutral sans is the default; a brand face may replace it for display only.

Do not carry over from iOS: a centred title, a bottom tab bar with 49 pt height, chevron-led list rows, a segmented control used as tabs.
