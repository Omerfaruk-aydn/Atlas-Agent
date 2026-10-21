// Atlas-only strings for the Atlas Agent-derived screens. Atlas Agent's own keys
// come from ./en and ./tr unchanged; add a key here only for something
// Atlas Agent has no word for.

export const atlasEn: Record<string, string> = {
  "atlas.auth.title": "Sign-in link needed",
  "atlas.auth.body": "Open this web UI with the one-time link printed by the command below, on the computer running Atlas.",
  "atlas.offline.title": "Atlas is not reachable",
  "atlas.offline.body": "The web UI could not reach its Atlas server. Check that `atlas-agent web` is still running.",
  // Atlas Agent's review extension strings (gui-extensions/review/i18n), which
  // the copied app has no dictionary for.
  "review.tab.title": "Review",
  "review.tab.count.one": "Files Changed {{count}}",
  "review.tab.count.other": "Files Changed {{count}}",
  "review.loadingChanges": "Loading changes...",
  // The button grants the tool for this session only, as the terminal's
  // "Allow for session" does; "always" would promise more than it does.
  "ui.permission.allowAlways": "Allow for session",
}

export const atlasTr: Record<string, string> = {
  "atlas.auth.title": "Giriş bağlantısı gerekli",
  "atlas.auth.body": "Bu web arayüzünü, Atlas'ın çalıştığı bilgisayarda aşağıdaki komutun yazdırdığı tek kullanımlık bağlantıyla açın.",
  "atlas.offline.title": "Atlas'a ulaşılamıyor",
  "atlas.offline.body": "Web arayüzü Atlas sunucusuna ulaşamadı. `atlas-agent web` komutunun hâlâ çalıştığını kontrol edin.",
  "review.tab.title": "İnceleme",
  "review.tab.count.one": "{{count}} dosya değiştirildi",
  "review.tab.count.other": "{{count}} dosya değiştirildi",
  "review.loadingChanges": "Değişiklikler yükleniyor…",
  "ui.permission.allowAlways": "Oturum için izin ver",
}
