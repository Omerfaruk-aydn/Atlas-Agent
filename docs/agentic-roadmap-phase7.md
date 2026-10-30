# Faz 7 raporu — Otonomi ve dağıtım

## 1. Özet

- ✅ **7.1 Goal modu:** Kalıcı bütçe/checkpoint akışına `atlas goal status|stop` eklendi; stop iptal eder ve goal'u temizler, dosyaları geri almaz.
- ✅ **7.2 Headless CI + Action:** `text|json|stream-json`, JSONL, 0/1/2/3 çıkış kodları, fail-closed izin politikası ve GitHub Action paketi tamamlandı.
- ✅ **7.3 PR bot:** `gh` tabanlı iki geçişli kanıt doğrulama, önem eşiği ve idempotent satır içi yorumlar eklendi.
- ✅ **7.4 Uzak devir:** Maskeli oturum + diff paketi, paired/pinned TLS node aktarımı ve uzak dal yönlendirmesi; otomatik merge/push yok.
- ✅ **7.5 Proaktif ajan:** Mevcut `schedule`, `watch_comments` ve `notify` altyapısı denetlendi; izinli olaylardan öneri/taslak üretimi korunuyor, kendiliğinden push/merge yok.
- ✅ **7.6 Plugin paketleri:** Manifest, init/install/list/remove/validate, kaynak önizleme ve açık onay eklendi; safe mode özelleştirmeleri kapatıyor.
- ✅ **7.7 Thread paylaşımı:** Bağımsız HTML, katlanabilir araç çıktıları, maskeleme ve `--preview`; ağ yüklemesi yok.

## 2. Tasarım ve değişen dosyalar

Makine çıktısı `internal/runoutput` altında tek sözleşmedir. Yerel olmayan server'a per-process izin politikası emanet edilmez. PR publish ayrı ve opt-in'dir; ikinci geçiş doğrulanmamış bulguyu yayınlayamaz. Uzak devir yalnızca mevcut kimlik doğrulamalı node planını kullanır. Plugin install önce manifest ve çalıştırılabilir içerik uyarısı gösterip `--yes` ister.

Yeni: `internal/runoutput/`, `reviewbot/`, `plugin/`, `session/export/html.go`, `redact.go`, `cmd/goal.go`, `node_handoff.go`, `review_pr.go`, `plugin.go`, `deploy/github-action/` ve testler. Genişletilen: `app/app.go`, `cmd/root.go`, `run.go`, `review.go`, `session_export.go`, `eval_corpus.go`, `node.go`, `config/safe_mode.go` ve harness entegrasyonları.

Başlangıçtan kirli mevcut dosyalar korundu; remote komutları testlerde gerçek kullanıcı oturumu veya uzak makineye gönderilmedi.

## 3. Doğrulama kanıtı

- `go test -p 1 ./internal/runoutput ./internal/reviewbot ./internal/plugin ./internal/session/export ./internal/nodes ./internal/app ./internal/cmd`: geçti; bilinen Windows npm testi kullanıcı onayıyla atlandı.
- `go test -p 1 ./internal/agent`: goal/checkpoint davranışıyla geçti.
- JSONL eşzamanlı yazım, çıkış kodları, izin fail-closed, ikinci geçiş/idempotence, manifest patolojileri, HTML kaçış/maskeleme testleri geçti.
- `go vet ./...`, `task lint:fix`, `git diff --check`: geçti.
- Gerçek GitHub PR yorumu, git clone, uzak node çalıştırması veya ücretli model çağrısı yapılmadı; bu sınırlar deterministik testlerle kapsandı.

## 4. Config ve varsayılanlar

`atlas run --output-format`: varsayılan `text`; eski `--json` korunur ve iki seçenek birlikte reddedilir. `--permission-policy` JSON `allowed_tools` içerir, local mode zorunludur ve prompt'ları kapatır. Exit `0` başarı, `1` hata, `2` doğrulanamadı, `3` engel.

`atlas review --pr N --min-severity medium` önizleme; `--publish` açık yayın onayıdır. Plugin `install` onaysız durur; `--replace` ayrıca gereklidir. `atlas session export --format html` yerel üretim, `--preview` dosya yazmadan maskedelenmiş çıktıyı gösterir. Safe mode varsayılan kapalıdır.

Kullanım ayrıntıları `docs/automation-and-distribution.md` içindedir.

## 5. Ölçüm

Goal tüketimi/bütçesi, headless terminal sonucu, PR doğrulama/publish, uzak devir ve export olayları mevcut iz ve harness kayıtlarına bağlanır. Action token ve sırları provider telemetrisinde maskelenir; HTML ve uzak pakette ayrıca redaction uygulanır. Yerel test maliyeti gerçek model içermez.

## 6. Riskler ve sınırlamalar

Action ve PR bot gerçek GitHub ortamında uçtan uca çalıştırılmadı; `gh` kimliği/repo izinleri çalışma ortamının sorumluluğudur. Uzak dal üretimi uzak ajanın kontratına bağlıdır, otomatik merge yapılmaz. Secret redaction bilinen biçimleri yakalar, her özel sırrı garanti edemez; export önizlemesi zorunlu iş akışı olarak belgelenmiştir. Proaktif kaynaklar yalnızca açık config ile çalışır.

## 7. Önerilen commit dilimleri

- `feat: add stable headless JSONL execution contracts`
- `feat: add verified idempotent pull request reviews`
- `feat: add secure remote session handoff`
- `feat: package customizations as reviewed plugins`
- `feat: export redacted standalone session HTML`
- `feat: expose persistent goal status and stop controls`
- `docs: document unattended and distributed workflows`

Commit, push veya PR yapılmadı.

## 8. Sonraki faz

Toplu devam onayıyla Faz 8 uygulandı.
