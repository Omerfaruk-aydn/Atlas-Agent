# App design reference findings (2026-11-04)

Sources: running OpenCode desktop (Electron, renderer bundle), Grok Bot desktop (Electron, renderer bundle),
Cursor 'workbench.desktop.main.css'. Screens: shots/. Raw CSS extracted from the apps' own bundles.
Limit: live interaction was NOT possible (screenshot tool returned blank, Cursor is typing-locked, Grok Bot
showed a desktop-agent consent prompt). Findings = static screens (OpenCode, Grok Bot) + shipped CSS/keyframes.
Cursor was not screenshotted (window minimized).

## OpenCode (light theme, 1296x808 window)
- Layout: custom titlebar (menu, grid-add, search, session tab pill, +), rounded white content card inset ~15px
  from window edge on off-white shell. Home: huge pixel 'opencode' wordmark (very pale grey), centered composer card
  (radius ~14, 1px hairline), below it a meta row: workspace chip / 'Yerel' + blue dot / git branch / path (right).
  Composer: placeholder, '+' attach, model picker ('Big Pickle v'), grey round send button. Footer hint link '75+ providers >'.
- Tokens: radii 2/4/6/8 dominant (6px most used), --font-family-sans + code mono, v2 token set (--text-weak/strong/weaker,
  --v2-state-fg-info, --radius-md).
- Easing: cubic-bezier(.2,.8,.2,1) x15 (main UI), (.22,1,.36,1) x16 (enter), (.4,0,1,1) exits, (.215,.61,.355,1).
  Durations: .15s dominant, .12s, .2s, .25-.28s panels, 85-90ms micro.
- Keyframes (exact):
  - text-shimmer-sweep: background-position 100% 0 -> 0 0. Component text-shimmer: per-char grid (base + shimmer layers
    crossfade .22s ease-out), --text-shimmer-step:45ms stagger, duration 1.2s, spread 5.2ch, base=text-weak, peak=text-strong.
    Used for 'thinking/running' labels. data-active toggles shimmer layer opacity.
  - markdown-word-enter: opacity 0->1 per streamed word (streaming text fade-in word by word).
  - loader-v2-spin 360deg; pulse-opacity .4<->1; blink 50% (caret); progress-indeterminate translate -100%->300%.
  - tooltipV2In / hover-card-open: opacity 0, scale .96 -> 1. composer-pill-panel-in: translateY(4px) scale(.98) fade.
  - slideDown (height 0 -> --kb-collapsible-content-height) for collapsibles (tool-call expand).
  - session-compaction gather/glint/enter: translate sweep -100%->250% + glint opacity .75 (context compaction moment).
  - home-session-skeleton-sweep (mask-position sweep), new-session-wordmark-shimmer, first-launch-wordmark-draw (svg stroke draw).
  - session tab popover in/out, titlebar-tab-fade-left/right (edge fades), composer-attachments-fade-left/right,
    session-dropzone (stripes drift), mobile-drawer backdrop in/out.

## Grok Bot (light, 1936x1056, built on Cursor's 'sand' design system - tokens named --cursor-*, --sand-*)
- Layout: narrow left rail (search + '+' round buttons, big circular agent avatar tile 'Fistik' w/ orange star badge,
  label under), bottom-left user initials chip + pill 'Uygulamalari bagla' with app icons. Center: thin top pill with
  agent avatar+name, single centered column (~560px) conversation. Timestamp centered small grey.
- Message style: assistant messages are soft grey rounded rectangles (#f0f0f1-ish, radius ~12), NO avatar per message,
  grouped consecutive messages with tight 2px gaps. Rich inline cards: question card with input row (blue 'B' chip +
  answer), connector card (Gmail logo, verified badge, black pill 'Baglan' button) with tiny caption below.
  Under message: meta row 'Sesli sohbet 00:02' + thumbs up/down icons. Inline system note 'Adi Fistik olarak degistirildi'.
- Composer: full-pill (radius 999) bottom-centered, '+' circle left, placeholder 'X icin mesaj yazin', mic icon,
  black circular voice-waveform button right. Very minimal, no borders heavier than hairline.
- Keyframes (named): hover-preview-in (scale .96 fade), ui grid-template-rows 1fr<->0fr (smooth collapse w/o JS height),
  shimmer background-position 100%->-100% and 200%->-200%, opacity pulses .4<->1 / .5<->1, caret blink,
  enter: opacity0+translateY(8px); exit: translateY(-14px) fade; scale .97->1 popover; blur(2px)+scale(.6) icon morph-out.
- Easing: (.2,0,0,1) x11 (Material emphasized), (.22,1,.36,1) x7, (.23,1,.32,1), spring-ish (.34,1.56,.64,1) for pop.
  Durations: .12 / .15 / .16 / .2 / .3s. Radii: 8, 12, 999.

## Cursor (workbench css, 78 keyframes)
- Agent/composer-specific: composer-status-ripple-fade, composer-sync-pulse, composer-cloud-transfer-pulse/spin,
  composer-action-notification-fade-in, cursor-loading-pulse, cursor-icon-spin, loading-dot-pulse, ellipsis-dot,
  text-shimmer, shine/shine2, blob-core/outline/subtle/support (agent 'blob' orb), wave1-3 (dictation waveform),
  recording-pulse, ui-icon-morph-in, review-changes-border-pulse, usage-limit-dot-bounce, fadeInSlideUp, scale-in,
  pretty-dialog-fade-in-modal/parent, agent-sidebar-update-button-pulse/spin, hard-pulse, ripple, wiggle.
- Easing: (.165,.84,.44,1) x4 (easeOutQuart), (.23,1,.32,1) x3, (.4,0,.2,1), (0,0,.2,1), (.16,1,.3,1), (.2,0,0,1).

## Cross-app consensus (use these)
1. Primary ease-out: cubic-bezier(.22,1,.36,1) or (.2,.8,.2,1); exits (.4,0,1,1); Material emphasized (.2,0,0,1).
2. Timings: hover/press .12-.15s; popovers/tooltips .15-.2s scale .96->1; panels/collapse .25-.3s; shimmer loop 1.2s.
3. Streaming text = per-word opacity fade (OpenCode) + blinking caret; thinking label = per-char shimmer (45ms stagger).
4. Collapsibles: height or grid-template-rows 0fr<->1fr transition; never jump.
5. Entry of new message: opacity 0 + translateY 8px -> 0; exit translateY -14px fade.
6. Loading: spinner 360deg linear + indeterminate bar + skeleton mask sweep; compaction gets its own sweep+glint.
7. Chat: Grok = soft grey bubbles, no per-message avatar, grouped tight; OpenCode = flat blocks w/ tool rows.
8. Composer: rounded card/pill, '+' left, model picker chip, round send button (grey disabled -> dark enabled).
9. Wordmark/brand moments: stroke-draw or mask shimmer on first launch / new session.

## ADDENDUM - live screens captured (shots/cursor-*.png, oc-model.png, oc-search.png, grok-connect.png)
Cursor Agents window (dark, #141414 bg; 1936x1056):
- Left sidebar ~260px: New Chat (active pill), Automations, Customize; Projects > New Project; Repositories (filter + add-folder
  icons) > repo > chat rows with status dot + relative time ('1h'); hover shows pin/archive icons + rich hover card (title, repo,
  path, 'Local'). Bottom: 'Getting Started 3/3' progress ring card w/ 'Connect Slack' button, user chip + gear.
- Home: centered composer card (radius ~12, 1px border), context row ABOVE it (repo v, branch v, 'This PC' v), placeholder
  'Plan, Build, / for skills, @ for context', '+' left, model chip ('Grok 4.7 Medium' v), mic, white round waveform button.
  Under it 3 hairline pill chips ('Plan New Idea  shift+Tab', 'Multitask', 'Run in Cloud'). Bottom promo strip (icon .... icon).
- Chat: user message = rounded dark card w/ small 'revert' icon right. Assistant text is plain (no bubble). Per-turn action row:
  thumbs up/down, copy, fork + '1h ago'. TOOL CALLS ARE PLAIN TEXT ROWS (no icons, no boxes): bold-ish verb + dim argument:
  'Read assistant.go L700-819', 'Grepped refusal|... in ui', 'Thought 4s', 'Explored 2 files, 2 searches'.
  Whole turn collapses under 'Worked for 2m 34s v' (chevron appears on hover); the summary line
  'Edited 3 files, explored 9 files, 7 searches, ran 1 command +21 -71' expands into the per-call list.
  Edited-files summary = bordered card '3 Files Changed' + 'Review' link, per-file rows with +green/-red counts.
  Right rail ('On Atlas'): Changes +N -N / Browser / Terminal / Files. Left edge: tick-mark timeline scrubber for turns.
  Bottom: floating chips (Changes +N -N, 1 Terminal, Create Branch & Commit v), scroll-to-bottom round arrow button,
  follow-up composer, branch + 'This PC' selectors, bottom fade mask over scrolling content.
OpenCode: model dialog = centered modal 640px, title + X, list rows 'Model [Ucretsiz badge]' with check on selected, footer panel
  (light grey) with 2-col provider cards (icon + name + desc) + '70+ providers' link; backdrop dim ~40%. Search = small 480px
  palette ('Oturum ara', empty state 'Sonuc bulunamadi'). No sessions existed, so live tool-call/streaming UI NOT observed
  (only from CSS above).
Grok Bot: 'Uygulamalari bagla' = large modal 900px: title, '3 bagli' avatars stack, search field + 'Bot sablonlari' button,
  filter chips (active = black pill), sections w/ 2-column app rows (logo 40px radius 10, name, grey pill 'Baglan' button),
  'Tumunu gor' links. Window controls stay above the dim backdrop.
STILL NOT OBSERVED LIVE: streaming text, web-search tool animation, tool call in progress in any of the three (no running
agent session). Derived from CSS only; the design route must mark these as 'inferred'.

## ADDENDUM 2 - LIVE streaming / tool / web-search observed (shots/oc-live, oc-live2, grok-live, grok-live2; each has sheet.png contact sheet + f00..fNN frames)
Test prompt in both apps: 'Web'de arama yap: Go dilinin son surumunde neler yeni?' (OpenCode also asked to list the folder).
Cursor could NOT be driven live (typing is blocked for IDE-tier apps) - Cursor remains static screens + CSS only.

### OpenCode live (light)
- Send: composer docks to bottom; send button (grey up-arrow) -> black ROUNDED-SQUARE STOP button while running.
  Header (right): round progress ring + token counter ('7 b' -> '25 b') spinning while busy; layout/sidebar toggle icons.
  Session tab title auto-generated from first prompt ('Go'nun yeni surumu ve klasor ozeti'); breadcrumb 'workspace / title ...'.
- User message: right-aligned light-grey rounded bubble (radius ~12), max ~570px.
- Busy label: 'Calisiyor' text with shimmer sweep, sits under the last block (this is text-shimmer component).
- TOOL GROUP LINE (collapsed context group): '[dim]Ikinci el[/dim] [1] [dark]Web aramasi ve Kabuk[/dark]' =
  dim role/prefix + count + dark title, single line, no icon/box; separate lines per group ('1 Web icerigini getir');
  assistant prose paragraphs sit BETWEEN tool-group lines (interleaved narration, 'Web aramasi ilk denemede basarisiz oldu...').
  Group line counts increase live as tools are added ('1 Web aramasi' -> '2 Web aramasi ve Kabuk').
- PERMISSION DOCK: when a tool needs consent, the composer is REPLACED by a bordered card (radius ~14) at bottom:
  title ('Ucuncu taraf web aramasi'), grey description, dropdown ('Herhangi biri v'), secondary text button
  ('Aramayi kullanma') + dark pill primary ('Etkinlestir'). Also a hint 'Arka plana tasi CTRL+B' under running shell.
  (We declined; no setting was changed.)
- STREAMING TEXT: markdown renders incrementally; the newest words appear GREY and darken to full text (word fade, matches
  markdown-word-enter); headings/bold/code/list items appear block by block; page auto-scrolls and top of conversation
  slides under a soft fade as it grows (sticky user bubble collapses to a 1-line strip '... dosyalari listele' at top).
- Inline code = pale grey chip with mono font, radius 4. Lists: standard bullets, bold lead-ins.

### Grok Bot live (light)
- User message: BLACK pill bubble, white text, right-aligned, radius ~18 (opposite of Cursor/OpenCode grey). Day separator
  'Bugun 15:04' centered small grey.
- INSTANT ACK: first assistant bubble 'Hemen bakiyorum.' (short grey rounded bubble) appears before any work.
- AGENT MASCOT AS STATUS INDICATOR: the sidebar avatar tile (black circle, face w/ two vertical-pill eyes) animates per state:
  start = three dots loading, then face; eyes blink / look sideways / 'pause' glyph (two bars) while searching / squint while
  writing. The SAME mascot (26px) appears inline under the last bubble next to a grey status text that CHANGES per phase:
  'Web'de arama yapiyor' -> 'Web'de ariyor' -> 'Yaziyor...' (text uses shimmer/pulse). No spinner, no tool cards at all.
- Final answer = single grey rounded block (radius ~12-16) with bold lead-ins, bullets, inline mono code chips; arrives
  complete-ish (no visible per-word fade in our frames, appears in chunks); sources shown ONLY as an inline citation chip
  (favicon + link text 'Go 1.27 surum notlarinda'), not a source list.
- Composer unchanged while running (waveform button stays); top of scroll area has soft fade under floating agent pill.

### Cross-app lessons for Atlas (live-confirmed)
1. Two valid tool philosophies: Cursor/OpenCode = quiet text rows/lines that expand; Grok = hide tools entirely behind a
   personality (mascot + changing status phrase). Atlas can do both: 'quiet mode' (rows) + 'Dots persona mode' (mascot status).
2. Always show something within <200ms: instant ack bubble (Grok) / 'Calisiyor' shimmer + stop button (OpenCode).
3. Send button must morph to stop button; header shows live token/progress ring.
4. Permission requests should REPLACE the composer (dock) with 2 actions, primary dark pill - not a blocking modal.
5. Interleave assistant narration between tool lines; counts update live; group by kind with dim prefix.
6. Streaming: block-by-block markdown + grey->dark newest words; keep auto-scroll with top fade.
7. Web results: inline citation chips with favicon; optional expandable source list (neither app shows cards).
