# Dokuz maddelik kapsam — tamamlayıcı doğrulama raporu (2026-10-30)

## Kapsam ve sonuç

Bu rapor `docs/implementation-verification-2026-10-29.md` raporundaki açık
kontrolleri tamamlar. Kodda yeni ürün mimarisi eklenmedi; tam repo race,
lint, Node entegrasyonu ve yerel release/restore doğrulamaları yapıldı.
Gerçek release, deploy, npm publish, commit/push, credential oluşturma veya
ücretli model çağrısı yapılmadı. Kullanıcının kirli çalışma ağacı korunurken
yalnız doğrulamanın açığa çıkardığı hatalar düzeltildi.

Yerel olarak doğrulanan ana sonuçlar:

- Tek komut `go test -race -p=2 -timeout=30m -failfast ./...`: **exit 0**.
  Kanıt: `artifacts/verification-2026-10-30/full-race.log` ve
  `full-race.exit`. Go paket cache'i kullanıldı; `-count=1` verilmedi.
- `go build ./...` ve `go vet ./...`: başarılı.
- `golangci-lint run --path-mode=abs --config=.golangci.yml --timeout=5m`:
  **0 issues**. `./scripts/check_log_capitalization.sh`: başarılı.
- Gerçek `node.exe` ile Windows npm doğrudan-process entegrasyon testi:
  başarılı, skip yok.
- Web typecheck, 21 birim testi ve production build: başarılı. Büyük chunk
  uyarıları devam ediyor; derleme hatası değil.
- `go mod tidy -diff`: değişiklik yok. Windows speech installer CI testi:
  başarılı.
- İki ayrı tag için dört platform binary'si, ayrı SPDX 2.3 SBOM'ları ve beş
  artifact'lık SHA-256 listeleri üretilip doğrulandı.
- Sekiz workflow YAML'i ayrıştırıldı; bütün `uses:` referansları 40
  karakterli commit SHA'sı ile sabitli.

## Bu turda açığa çıkan ve giderilen hatalar

### Credential screenshot kanıtının rastgele reddedilmesi

Kısa bir kayıtlı sır, `credential_image_redaction` içindeki hex SHA-256
özetinin tesadüfi alt dizesi olabiliyordu. Metadata redaction özeti bozuyor,
maskeli görüntünün meşru kanıtı reddediliyordu. Güvenilir browser/pipeline
kanıtı redaction öncesinde alınmakta, değişmeyen görüntü baytları redaction
sonrasında yeniden imzalanmaktadır. Özetin ilk baytlarını sır yapan kalıcı
regresyon testi eklendi.

Dosyalar: `internal/agent/tools/secret_filter.go`,
`internal/agent/tools/protected_media_test.go`.

### CLI Bridge iptalinin hazırlık aşamasında yutulması

İptal komut başlamadan preparation/permission aşamasında gerçekleşirse
`cli_bridge` sıradan model-facing response ile `nil` Go hatası
döndürebiliyordu. `execute` artık her aşamada caller cancellation'ı
`context.Canceled` zinciriyle korur.

Dosya: `internal/agent/tools/cli_bridge.go`.

### Mobil gateway kapanışının hata olarak raporlanması

Gateway paylaşılan dinleyiciyi kapattıktan sonra protocol listener'ın aynı
kapatma isteği Windows'ta `net.ErrClosed` döndürebiliyordu. İdempotent
kapanış artık bu beklenen hatayı başarı olarak normalleştirir; diğer hatalar
korunur.

Dosya: `internal/mobile/protocol_listener.go`.

### Release SBOM'unun checksum/provenance dışında kalması

`atlas-agent-*` deseni `atlas-agent.spdx.json` adını kapsamıyordu. Release ve
restore akışlarında checksum komutuna ve build provenance subject listesine
SBOM açıkça eklendi. `lint.yml` içindeki tag tabanlı üç action referansı da
immutable SHA ile sabitlendi; `golangci-lint-action` v8 SHA'sı upstream
`git ls-remote` ile doğrulandı.

Dosyalar: `.github/workflows/release.yml`,
`.github/workflows/release-restore.yml`, `.github/workflows/lint.yml`.

### Doğrulama altyapısı düzeltmeleri

`Taskfile.yaml` içinde Go 1.26.6'nın reddettiği `GOEXPERIMENT: null` değeri
boş string yapıldı. Log lint taraması `.git`, `.tools`, `node_modules` ve
`dist` dışlamalarıyla kaynak koda odaklandı. Corpus alt testlerine eksik
`t.Parallel()` eklendi.

Dosyalar: `Taskfile.yaml`, `scripts/check_log_capitalization.sh`,
`internal/permission/auto_approve_corpus_test.go`.

## Madde bazında kapanış

### H2 — Secret scan fail-closed

- **Kabul kriteri:** Silinmemiş fakat okunamayan/stat edilemeyen/non-regular
  değişen dosya taraması başarı sayılmamalı; hata `Unverified` olmalı.
- **Ana dosyalar:** `internal/agent/completion_pipeline.go`,
  `internal/agent/completion_secret_test.go`; ek credential koruması
  `internal/agent/tools/secret_filter.go` ve `protected_media_test.go`.
- **Doğrulama:** İlgili testler tam repo race içinde geçti. Credential kanıt
  regresyonu ve CLI iptali ile birlikte hedefli testler `-race -count=5`
  altında geçti.
- **Kalan engel:** Uzak GitHub Actions matrisi çalıştırılmadı.

### H3 — Mandatory hook failure policy

- **Kabul kriteri:** Required PreToolUse dispatch'i önlemeli; required
  PostToolUse turn'ü durdurmalı fakat rollback iddiası olmamalı; dedup ve
  atlasrc required politikasını zayıflatmamalı.
- **Ana dosyalar:** `internal/config/config.go`, `internal/hooks/hooks.go`,
  `internal/hooks/runner.go`, `internal/hooks/required_test.go`,
  `internal/agent/hooked_tool.go`, `internal/agent/required_hook_test.go`,
  `internal/shellconfig/hook.go`, `docs/hooks/README.md`, `schema.json`.
- **Doğrulama:** Agent ve hooks paketleri tam repo race içinde geçti;
  build/vet/lint başarılı.
- **Kalan engel:** Uzak CI matrisi çalıştırılmadı.

### H4 — Test/race/release task düzeltmeleri

- **Kabul kriteri:** Test task'ları tüm paketleri ve ek CLI argümanlarını
  çalıştırmalı; race CGO ile açık olmalı; Node ön koşulu gerçekçi olmalı.
- **Ana dosyalar:** `Taskfile.yaml`, `internal/cmd/update_windows_test.go`,
  `internal/permission/auto_approve_corpus_test.go`,
  `scripts/check_log_capitalization.sh`.
- **Doğrulama:** Tek komut tam repo race exit 0. Node.js 22.20.0 ile
  `go test -count=1 -v ./internal/cmd -run 'Test.*Npm|Test.*Node|Test.*Update'`
  geçti; `TestNpmCommandWindowsRunsNodeWithoutShellQuoting` artık skip değil.
  `golangci-lint` 0 issue ve log lint başarılı.
- **Kalan engel:** `task test` ve `task test:race` sarmalayıcılarının kendisi
  değil, eşdeğer doğrudan Go komutları çalıştırıldı. Uzak CI yok.

### H6 — Release supply chain

- **Kabul kriteri:** Immutable action SHA'ları, SPDX SBOM, doğrulanmış
  SHA-256 checksum ve etiket başına izole restore bulunmalı.
- **Ana dosyalar:** `.github/workflows/build.yml`, `release.yml`,
  `release-npm.yml`, `release-restore.yml`, `lint.yml`.
- **Doğrulama:** `v0.0.0-verification` ve `v0.0.0-restore` dizinlerinin her
  birinde Windows/amd64, Linux/amd64, Darwin/amd64 ve Darwin/arm64 binary'si
  üretildi. Windows binary'leri doğru ve farklı sürümleri raporladı.
  Syft 1.55.0 ile her tag için ayrı SPDX 2.3 SBOM üretildi; her biri 14.453
  paket içeriyor. Her `SHA256SUMS` dört binary ve SBOM olmak üzere beş girdi
  için `sha256sum -c` kontrolünden geçti. SBOM belgeleri ve tag binary'leri
  birbirinden farklı. Yerel, imzasız in-toto subject manifestleri üretildi;
  bunlar imzalı provenance değildir.
- **Artifact kökü:** `artifacts/verification-2026-10-30/release/`.
- **Kalan engel:** GitHub OIDC/attestation ortamı olmadan imzalı provenance
  üretilmedi. Uzak release/restore ve npm publish çalıştırılmadı.

### O1 — Auto-approve corpus ve CI gate

- **Kabul kriteri:** Riskli komutlar modele sorulmadan reddedilmeli/gri alan
  fail-closed olmalı; benzersiz etiketli corpus CI'da zorunlu olmalı.
- **Ana dosyalar:** `internal/permission/auto_approve.go`,
  `auto_approve_corpus_test.go`, `testdata/auto_approve_corpus.json`,
  `.github/workflows/build.yml`.
- **Doğrulama:** Permission paketi tam repo race içinde geçti; lint'in
  `tparallel` bulgusu giderildi. Corpus'un önceki doğrulaması 261/261 benzersiz
  komut, 141 risky ve 120 unknown idi.
- **Kalan engel:** CI gate'in uzak GitHub Actions koşusu yok.

### O2 — Opt-in güvenli fallback

- **Kabul kriteri:** Varsayılan davranış değişmemeli; fallback yalnız geçici
  hata, başlamamış stream, farklı provider, capability uyumu ve sınırlı
  bütçeyle olmalı; auth/diğer 4xx/cancel fallback olmamalı.
- **Ana dosyalar:** `internal/config/config.go`,
  `internal/agent/model_fallback.go`, `transient_fallback.go`,
  `transient_fallback_test.go`, `agent.go`, `coordinator.go`, `schema.json`.
- **Doğrulama:** Agent paketi tam repo race içinde geçti; build/vet/lint
  başarılı. Ücretli canlı model testi yapılmadı.
- **Kalan engel:** Uzak fake-provider CI matrisi çalıştırılmadı.

### O3 — Risk tabanlı review/check ve completion timeout

- **Kabul kriteri:** Review riski verification komutlarından belirlenmeli;
  risky/required reviewer veya state hatası fail-closed olmalı; optional eski
  davranışı korumalı; her check sınırlı timeout'a sahip olmalı.
- **Ana dosyalar:** `internal/permission/auto_approve.go`,
  `internal/config/config.go`, `completion_timeout_test.go`,
  `internal/agent/plan_review_policy.go`, `plan_review_policy_test.go`,
  `plan_review.go`, `completion_pipeline.go`,
  `internal/shellconfig/options.go`, `schema.json`.
- **Doğrulama:** Agent/config/permission paketleri tam repo race içinde
  geçti; build/vet/lint başarılı.
- **Kalan engel:** Uzak CI matrisi çalıştırılmadı.

### O4 — MCP workspace/session izolasyonu

- **Kabul kriteri:** Mesaj yalnız exact canonical workspace ve current
  generation'a ulaşmalı; eski reconnect event'i ve bilinmeyen kimlik
  düşürülmeli; app subscriber workspace seçmeli.
- **Ana dosyalar:** `internal/agent/tools/mcp/init.go`, `channel.go`,
  `channel_scope_test.go`, `internal/app/app.go`.
- **Doğrulama:** MCP ve app paketleri tam repo race içinde geçti;
  build/vet/lint başarılı.
- **Kalan engel:** Uzak platform/reconnect CI matrisi çalıştırılmadı.

### 5 — Browser-use web regresyon ve CI entegrasyonu

- **Kabul kriteri:** Mevcut workflow/assert/wait/Verify/OnError/retry ve
  evidence altyapısı yeniden kullanılmalı; sabit fake-model senaryoları izole,
  süreli, temizlenebilir, makinece doğrulanabilir ve nonzero exit üretmeli;
  Playwright kapsam eşdeğerliği olmadan korunmalı.
- **Ana dosyalar:** `internal/agent/tools/browser_regression.go`,
  `internal/webui/webtest/browser_regression.go`,
  `browser_regression_test.go`, `webregress/main.go`, `fakellm.go`,
  `web/e2e/browser-use-regress.ts`, `screens.spec.ts`, `stack.ts`,
  `web/package.json`, `web/tsconfig.json`, `Taskfile.yaml`,
  `.github/workflows/build.yml`, `docs/browser-regression.md`; ek kanıt
  düzeltmesi `internal/agent/tools/secret_filter.go`.
- **Doğrulama:** Önceki rapordaki üç normal Chrome senaryosu ve Playwright
  12/12 sonucu korunuyor; bu turda browser kodu değişmedi. İlgili Go paketleri
  tam repo race içinde geçti. Credential screenshot hatasının kök nedeni
  giderildi ve hedefli race regresyonu beş tekrar geçti.
- **Kalan engel:** GitHub Actions Ubuntu browser job'u uzakta çalışmadı.
  Kapsam eşdeğerliği kanıtlanmadığından Playwright zorunlu kalır.

## Ortam notları ve açık sınırlar

- Araçlar repo içinde `.tools` altına kuruldu: Go, MinGW/GCC, Node.js,
  golangci-lint, gofumpt ve Syft.
- Testlerde repo içi `.tools/tmp`, `TMP/TEMP/GOTMPDIR` olarak kullanılmadı;
  aksi durum Git kaynak parmak izi testlerinde yanlış başarısızlık üretiyordu.
- Git Bash'te `npm` kabuk girişi script'leri sessizce exit 1 ile bitirdi;
  `npm.cmd` ile typecheck/test/build başarılı oldu. Bu ürün test hatası
  değil, yerel Windows kabuk çağırım farkıdır.
- İlk tam race denemeleri 10 dakikalık tools timeout'u, CLI cancellation ve
  mobil listener kapanış hatalarını yakaladı. Başarısız loglar aynı artifact
  dizininde `full-race-cancel-failure.log` ve `full-race-mobile-failure.log`
  olarak saklandı; başarılı sonuç bunların yerine geçmiş gibi değil, son
  düzeltmelerin ardından elde edildi.
- Uzak GitHub Actions build/lint/browser/release matrisleri ve imzalı OIDC
  provenance, değişiklikler push edilmediği için doğrulanmadı. Gerçek
  release/deploy/publish yapılmadı.
