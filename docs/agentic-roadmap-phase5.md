# Faz 5 raporu — İş akışı ve izinler

## 1. Özet

- ⚠️ **5.1 Spec modu:** Üç belge, sıralı/digest korumalı onay, düzenleme/reddetme ve görev-JSON eşitlemesi tamamlandı. `/spec` manuel akışı var; büyük işleri otomatik yakalayan ayrı eşik yok.
- ✅ **5.2 Steer modu:** `/steer` aktif tura yönlendirme, `/queue` ayrı tur; kullanıcı kısıtı kalıcı çalışma bağlamına aktarılıyor.
- ✅ **5.3 Runtime izin yükseltme:** Çağrı/tur/oturum kapsamları, redde fail-closed davranış ve headless politika desteği eklendi.
- ✅ **5.4 Auto-approve:** Önce deterministik sınıflandırma, gri bölgede model, hatada sor; yıkıcı işlemler otomatik onaylanmaz.
- ⚠️ **5.5 Output styles:** Dört yerleşik stil ve proje/kullanıcı Markdown dosyaları dinamik bağlamda. `/style` yerleşik stilleri döndürür; özel dosyaları keşfedip listeleme arayüzü yok.
- ✅ **5.6 Safe mode:** CLI bayrağı, görünür banner, özelleştirmelerin kapatılması ve `atlas doctor` içinde kurtarma önerisi tamamlandı.

## 2. Tasarım ve değişen dosyalar

`work.json` görevlerin tek doğruluk kaynağıdır. `tasks.md` yalnızca aynı kimlik/başlık/doğrulama komutlarıyla checkbox senkronu kabul eder; requirements/design kullanıcı metnini korur. Onaylar kontrat ve belge digest'ine bağlıdır. İzin istekleri süreli ve kapsamlıdır; bilinmeyen veya başarısız sınıflandırma güvenli onay üretmez. Output style sabit prompt'a değil değişken bağlama eklenir.

Yeni başlıca dosyalar: `internal/workplan/spec.go`, `permission/auto_approve.go`, `permission/scoped.go`, `permission/headless.go`, `agent/runtime_permission.go`, `permission_classifier.go`, `steering.go`, `output_style.go`, `config/safe_mode.go`, TUI spec/steering/handoff dosyaları ve testleri. `internal/cmd/doctor.go`, `run.go`, `permission.go`, `ui/dialog/spec.go`, `ui/model/spec.go`, `i18n/catalog.txt` ve config entegrasyonları genişletildi.

Mevcut kirli çalışma ağacındaki dosyalar korundu. UI değişiklikleri `internal/ui/AGENTS.md` kurallarına uygun olarak IO'yu `tea.Cmd` içinde tutar.

## 3. Doğrulama kanıtı

- `go test -p 1 ./internal/permission ./internal/workplan`: geçti.
- `go test -p 1 ./internal/agent ./internal/ui/model ./internal/ui/dialog ./internal/config ./internal/cmd`: geçti; kullanıcının hariç tuttuğu eski Windows npm testi atlandı.
- Etiketli auto-approve test setinde yanlış-güvenli sayısı `0`.
- `go test ./internal/i18n`: geçti; yeni TUI metinlerinin altı dilde kapsamı doğrulandı.
- `go vet ./...`, `task lint:fix`, `git diff --check`: geçti.

## 4. Config ve varsayılanlar

`options.output_style`: varsayılan `default`; `concise`, `explanatory`, `learning` veya geçerli bir dosya adı. Proje `.atlas/output-styles/<name>.md`, kullanıcı config dizini altındaki `output-styles/<name>.md`; proje kullanıcı dosyasını geçersiz kılar, yerleşikleri kılamaz. Dosya en fazla 16 KiB.

`--safe-mode`: varsayılan kapalı; provider kimliği ve model seçimi kalırken hooks, skills, MCP, LSP, izin özelleştirmeleri, context/subagent/workflow yolları, output style ve role/fallback özelleştirmeleri kapatılır. `--yolo` ile birleştirilemez.

Headless `allowed_tools` açık allowlist'tir. Auto-approve model hatası ve zaman aşımı fail-closed'dur. Yeni izin kapsamı kullanıcı onayı olmadan verilmez.

## 5. Ölçüm

Otomatik onay/red, model sınıflandırma hatası, izin kapsamı, safe-mode ve stil seçimleri mevcut izin/trace kayıtlarına bağlanır. Etiketli test corpus'unun yanlış-güvenli oranı `0/N` değil, mevcut 11 vakada `0/11` olarak ölçüldü. Bu küçük regresyon seti gerçek dünya güvenlik garantisi değildir.

## 6. Riskler ve sınırlamalar

Büyük işlerde otomatik spec başlangıç eşiği henüz yoktur; manuel `/spec` ve kontrat akışı kullanılmalıdır. `/style` özel stil dosyalarını listelemez; geçerli ad config ile verilebilir. Auto-approve yalnızca izin katmanını daraltır, komutları sandbox'a almaz. Safe mode CLI yerel başlangıç içindir; önceden çalışan uzak server'ın config'ini değiştirmez.

## 7. Önerilen commit dilimleri

- `feat: add reviewable specification workflow`
- `feat: persist explicit steering and queued follow-ups`
- `feat: add scoped runtime permission grants`
- `feat: add fail-closed automatic permission classification`
- `feat: add configurable output styles and safe mode`
- `test: cover specification permissions and localization`

Commit, push veya PR yapılmadı.

## 8. Sonraki faz

Toplu devam onayıyla Faz 6 uygulandı; kalan iki küçük UX sınırlaması burada açıkça korunmuştur.
