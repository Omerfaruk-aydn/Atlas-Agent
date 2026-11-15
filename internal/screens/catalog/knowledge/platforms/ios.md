# iOS (iPhone, 390 x 844 pt)

Written for the studio from Apple's published Human Interface Guidelines; numbers are the ones a layout depends on.

Frame and safe areas
- Top safe area 54 pt (Dynamic Island), bottom 34 pt (home indicator). Nothing tappable under either; backgrounds may extend behind them.
- Reading inset 16 pt, 20 pt for roomier screens. Keep one inset per app.
- Navigation bar: 44 pt content under the status bar. Large title (34 pt, bold) collapses into a 17 pt semibold inline title on scroll.
- Tab bar: 49 pt plus the bottom safe area, three to five tabs, each an icon with a short label, filled icon for the selected tab. Never a hamburger menu for primary navigation.
- Back is a chevron plus the previous title at the leading edge; swipe from the left edge also goes back.

Controls
- Minimum tap target 44 x 44 pt, including icon-only buttons. A 24 pt icon needs padding to reach it.
- List rows 44 pt minimum, 56 to 64 pt with a subtitle. Separators are hairlines inset to the text, not full-bleed boxes.
- Primary button: full width, 50 to 54 pt, 12 to 14 pt corner (continuous curve). One primary action per screen.
- Segmented control 32 pt; switches 51 x 31 pt; sheets slide up with a grabber and a 38 pt top radius; destructive choices go in an action sheet.

Type (San Francisco; if SF Pro is not available the studio draws a close substitute, so judge hierarchy, not letterforms)
- Large Title 34, Title 1 28, Title 2 22, Title 3 20, Headline 17 semibold, Body 17, Callout 16, Subheadline 15, Footnote 13, Caption 12 and 11. Never below 11 pt.
- Text scales with Dynamic Type: layouts must survive 130 percent text (use the stress option).

Icons and imagery
- SF Symbols style: icon stroke weight matches the neighbouring text weight, one size per context (tab bar 24, row 22, inline 17).
- Photos and material blur are acceptable behind bars; text on photos needs a scrim.

Behaviour to show
- Pull-to-refresh, swipe actions on rows, large-title scroll states, a sheet over the screen, an empty state with one action.
