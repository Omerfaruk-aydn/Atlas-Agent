# Codex side panel: what the screenshots show

Captured from the running ChatGPT (Codex) desktop app at 1920x1040, dark
theme. Files are `codex-*.png` in this folder. Sizes are read off the
screenshots, so treat them as close, not exact.

## Layout
- The thread header and the panel's tab strip share one row (about 56px, below
  a 42px title bar). There is no card around the panel: a 1px divider separates
  it from the thread, and it runs to the window edge.
- Thread header, right side: `...`, a summary-list toggle, and `+` when the panel
  is closed (`+` opens the panel). With the panel open, the `+` is gone and the
  panel has its own controls.
- Dragging the divider resizes live. The thread stops at about 320px; the panel
  takes the rest (`codex-14-resized-wide.png`). The composer drops its labels
  to icons at that width.
- Expand covers everything, including the sidebar, and leaves only the title bar
  (`codex-07-expanded.png`). The expand button stays highlighted.

## Tab strip
- Active tab: bordered rounded card about 238x32 with icon, title and close.
  Inactive tabs: flat, no border, separated by a thin line, no visible close.
- Tabs shrink evenly as more open. `+` follows the last tab. Expand and close
  sit at the far right, after a thin separator.
- Tab titles: Değişiklikler, Dosya aç, the shell path for a terminal, Yan sohbet,
  the page title for a website.

## Browser tab (`codex-08`, `codex-09`, `codex-10`, `codex-11`)
- Toolbar: back and forward, a thin separator and reload inside one pill; on
  web pages a "Not ekle" pill; a centred address pill; a pill with Uzantılar and
  İndirilenler; a round `...` button. Each has a tooltip.
- `...` menu: Sayfada bul, Yazdır, Yakınlaştırma (minus, %100, plus, reset),
  Cihaz araç çubuğu >, Ekran görüntüsü al, Çerezleri ve parolaları içe aktar...,
  Şifreler ve otomatik doldurma >, İndirilenler, Geçmiş, Göz atma verilerini
  temizle, Tarayıcı ayarları.
- Uzantılar popup: Uzantılar (0), Kurulu uzantı yok, Uzantıları yönet, Web
  Mağazasını Ziyaret Et, Araç çubuğundan gizle.

## New tab page (`codex-01`, `codex-05`)
- "Araçlar": two columns of rows with a shortcut chip. A tool that is already
  open leaves the list, and the rest reflow. Terminal stays listed, because
  several can be open, and has its own `...`.
- "Önerilen": four centred tiles, icon above the title, truncated titles.

## Tools
- Değişiklikler (`codex-02`): a "Branch" pill with +/- totals, a `main -> origin/main`
  pill, an icon toolbar on the right; diff area on the left; on the right a
  filterable tree of changed files with file-type icons, per-file +/- and a
  status square.
- Dosyalar (`codex-03`): tab title "Dosya aç"; a drive chip at top left; two
  icon toggles at top right; right-hand tree with "Dosyaları filtrele..." and
  "Tüm değişiklikler"; empty state in the middle: icon, "Dosya aç", "Çalışma
  alanı ağacından bir dosya seç".
- Terminal (`codex-04`): flush, no toolbar, monospace on the panel background.
- Yan sohbet (`codex-06`): an empty thread with a full composer at the bottom.

## Top-right popover (`codex-00`, `codex-12`, `codex-13`)
- Opens under the list toggle, right-aligned to it, about 300px wide, about 16px
  radius, raised surface. The toggle stays highlighted while open.
- Sections, in order: project name with `...`; Değişiklikler with green and red
  totals; Yan sohbetler; Alt otonom ajanlar (a row of small agent icons and
  "26 tane bitti"); Kaynaklar with `+`, listing web links with site icons and
  "Web araması"; a muted Tümünü görüntüle.
- A section with nothing in it is not shown.
- Project `...` menu, opening to the left of the popover: Yerel ortam kurulumu,
  Terminal aç, separator, Çalışma ağacına taşı.
