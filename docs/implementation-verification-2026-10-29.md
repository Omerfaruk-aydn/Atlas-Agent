# Dokuz maddelik uygulama ve doğrulama raporu — 2026-10-29

## Sonuç ve doğrulama sınırı

Dokuz maddenin kod ve workflow uygulaması çalışma ağacında mevcuttur. Taşınabilir
Go 1.26.6 ve MinGW/GCC 16.2.0 `.tools` altına kuruldu; normal Chrome
`C:\Program Files\Google\Chrome\Application\chrome.exe` ile gerçek browser
koşuları yapıldı. Ücretli model, credential, release, deploy, commit veya push
kullanılmadı. Başlangıçtan beri kirli olan çalışma ağacı resetlenmedi veya toplu
biçimlendirilmedi.

Bu rapor “sıfır hata” iddiası değildir. Tek `go test -failfast ./...` komutu araç
süresi sınırını aştığı için paketler gruplara ayrıldı. Bir paralel tools grubunda
credential testi flaky başarısız olup izole koşuda geçti. Bütün paketler için tek
race koşusu, golangci-lint ve gerçek CI/release çalıştırılmadı.

### Başarılı ana kontroller

- `go build ./...`: son biçimlendirme sonrasında yeniden çalıştırıldı, başarılı.
- `go vet ./...`: son biçimlendirme sonrasında yeniden çalıştırıldı, başarılı.
- Kapsamdaki `internal/config/config.go` ve
  `internal/permission/auto_approve.go` için `gofumpt -l`: temiz.
- Son biçimlendirme sonrası hedefli config/permission/agent testleri: başarılı.
- Hedefli agent, config, hooks, permission, MCP, tools ve webtest testleri:
  başarılı.
- Kritik MCP/hooks/permission race testleri (`CGO_ENABLED=1`): başarılı.
- Go paket grupları: `cmd`, agent, providers, engineering, evaluation, UI,
  webui ve diğer gruplar başarılı; ayrıntılar ve istisnalar aşağıdadır.
- Browser benchmark’ı: tek başına 87.8 saniyede başarılı.
- `TestLive*`: 115.5 saniyede başarılı.
- `npm --prefix web run typecheck`: başarılı.
- `npm --prefix web test`: 3 dosya, 21 test başarılı.
- `npm --prefix web run build`: başarılı; mevcut chunk boyutu uyarıları var.
- Atlas browser-use: üç senaryonun tamamı normal Chrome ile başarılı.
- Playwright: 12/12 test başarılı.
- `git diff --check`: whitespace hatası yok; bazı mevcut CRLF dosyalarında LF
  dönüşüm uyarıları var.

## H2 — Changed-file secret scan fail-closed

**Kabul kriteri:** Okunamayan, stat edilemeyen, nil-info veya regular olmayan
değişen dosya taraması başarı sayılmamalı; yalnız gerçekten silinmiş dosya
atlanmalı ve tarama hatası `Unverified` üretmeli.

**Uygulama:** `scanChangedSecretsWithStat` yalnız `fs.ErrNotExist` durumunu
atlıyor. Diğer stat/scan hataları ve non-regular dosyalar fail-closed. Bulgu
içeriği continuation'a kopyalanmıyor.

**Değişen dosyalar:** `internal/agent/completion_pipeline.go`,
`internal/agent/completion_secret_test.go`.

**Doğrulama:** `TestChangedFileSecretScan*` hedefli agent koşusunda ve agent
paket koşusunda geçti. Build/vet başarılı.

**Kalan engel:** Agent paketinin tamamı için race koşusu yapılmadı.

## H3 — Mandatory hook failure policy

**Kabul kriteri:** Required PreToolUse hatası tool dispatch'i önlemeli;
required PostToolUse hatası turn'ü durdurmalı fakat rollback iddiası
oluşturmamalı; dedup required politikasını zayıflatmamalı; atlasrc ve şema aynı
davranışı sunmalı.

**Uygulama:** Hook config'e `Required` eklendi. Required hataları deny ve
`required_failure` metadata üretiyor. Pre hook inner tool'u çalıştırmıyor; post
hook hatayı turn'e yayıyor. Dedup required bayrağını koruyor. `hook ...
--required` atlasrc desteği ve dokümantasyon eklendi.

**Değişen dosyalar:** `internal/config/config.go`, `internal/hooks/hooks.go`,
`internal/hooks/runner.go`, `internal/hooks/required_test.go`,
`internal/agent/hooked_tool.go`, `internal/agent/required_hook_test.go`,
`internal/shellconfig/hook.go`, `docs/hooks/README.md`, `schema.json`.

**Doğrulama:** Required hook hedefli testleri, hooks paketi ve hedefli race
koşusu geçti. Build/vet başarılı.

**Kalan engel:** CI üzerinde bütün agent/hook race matrisi çalıştırılmadı.

## H4 — Test, race ve release task tutarlılığı

**Kabul kriteri:** Test task'ları bütün paketleri ve ek CLI argümanlarını
çalıştırmalı, race CGO ile açık olmalı, yanlış release precondition
bulunmamalı.

**Uygulama:** `test` ve `test:race` `-failfast ./... {{.CLI_ARGS}}` kullanıyor;
race task'ı `CGO_ENABLED=1`. Yanlış release precondition kaldırıldı. Windows
npm entegrasyon testinin gerçek ön koşulu doğrudan çalıştırılabilir `node.exe`
olarak belgelendi; yalnız `node.cmd` bulunan ortamda açık skip üretiyor.

**Değişen dosyalar:** `Taskfile.yaml`,
`internal/cmd/update_windows_test.go`.

**Doğrulama:** YAML ayrıştırma başarılı. Go ve GCC kuruldu; hedefli race
koşusu geçti. `internal/cmd` paketi geçti; node.exe olmadığından ilgili
entegrasyon fikstürü skip edildi. Diğer paketler gruplar halinde çalıştırıldı.

**Kalan engel:** Tek komutla bütün repo test/race matrisi ve `task test`,
`task test:race` çalıştırılmadı. Uzun tools paketinin tek `go test ./...`
içindeki toplam süresi 300 saniyelik araç sınırını aştı.

## H6 — Release supply chain

**Kabul kriteri:** Action'lar immutable SHA ile sabitlenmeli; release
artifact'ları SPDX SBOM, doğrulanan SHA-256 checksum ve build provenance
içermeli; restore birden çok etiketi birbirine karıştırmamalı.

**Uygulama:** Release ve restore akışlarına SBOM/checksum/provenance eklendi.
Restore etiket başına matrix job kullanıyor. `actions/checkout@v4` ve çok
satırlı `tag_name` hatası giderildi. Artifact listelerine SBOM ve SHA256SUMS
eklendi.

**Değişen dosyalar:** `.github/workflows/release.yml`,
`.github/workflows/release-npm.yml`, `.github/workflows/release-restore.yml`,
`.github/workflows/build.yml`.

**Doğrulama:** Beş workflow YAML ayrıştırması ve action referanslarının 40
karakterli SHA denetimi başarılı.

**Kalan engel:** Gerçek CI/release/restore, SBOM ve provenance üretimi
çalıştırılmadı; kullanıcının release/deploy yasağı korundu.

## O1 — Auto-approve corpus ve CI gate

**Kabul kriteri:** Riskli komutlar modele sorulmadan reddedilmeli/gri alan
fail-closed olmalı; geniş, benzersiz, etiketli corpus CI'da zorunlu olmalı.

**Uygulama:** Deterministik risk sınıflandırması ve 261 komutluk corpus
eklendi. Build matrix'te race'li corpus gate var. `.env` yollarını sınır
karakterleriyle yakalama eklendi.

**Değişen dosyalar:** `internal/permission/auto_approve.go`,
`internal/permission/auto_approve_corpus_test.go`,
`internal/permission/testdata/auto_approve_corpus.json`,
`.github/workflows/build.yml`.

**Doğrulama:** Corpus testleri ve race koşusu geçti. JSON içinde 261/261
benzersiz komut, 141 risky ve 120 unknown doğrulandı. Playwright izin
fikstürü, güvenli yazmaların artık otomatik onaylanmasından bağımsız olarak
riskli komutla gerçek izin kartını doğruluyor.

**Kalan engel:** CI gate'in GitHub Actions üzerindeki koşusu doğrulanmadı.

## O2 — Opt-in güvenli transient fallback

**Kabul kriteri:** Varsayılan davranış değişmemeli; opt-in fallback yalnız
geçici hata, başlamamış stream, farklı provider, uyumlu capability ve sınırlı
bütçeyle olmalı; auth/diğer 4xx/caller cancel fallback olmamalı; 429 eski
politikayı korumalı.

**Uygulama:** `fallback_policy.on_transient` ve `max_providers` eklendi.
408/5xx/transport/UnexpectedEOF/net-timeout sınıflandırması, capability uyumu,
stream başlangıç kilidi ve farklı provider bütçesi agent döngüsüne bağlandı.
429 mevcut fallback yolunu kullanıyor.

**Değişen dosyalar:** `internal/config/config.go`,
`internal/agent/model_fallback.go`, `internal/agent/transient_fallback.go`,
`internal/agent/transient_fallback_test.go`, `internal/agent/agent.go`,
`internal/agent/coordinator.go`, `schema.json`.

**Doğrulama:** Transient/fallback hedefli testleri ve agent paket koşusu
geçti. Build/vet başarılı. Ücretli canlı model çağrısı yapılmadı.

**Kalan engel:** CI fake-provider kontrat koşusu ve agent genel race koşusu
yapılmadı.

## O3 — Risk tabanlı review/check ve completion timeout

**Kabul kriteri:** Review riski somut verification komutlarından mevcut
permission classifier ile belirlenmeli; risky/required reviewer veya state
hatasında fail-closed olmalı; optional eski davranışı korumalı; her check
sınırlı timeout'a sahip olmalı.

**Uygulama:** `optional|risky|required` review politikası eklendi. Geçersiz
değer `risky` oluyor. Required review anahtarı optional review ile
paylaşılmıyor. Reviewer unavailable/state/verdict/budget hataları
`Unverified`. Her check varsayılan 120 saniye, en çok 600 saniye ile
çalışıyor.

**Değişen dosyalar:** `internal/permission/auto_approve.go`,
`internal/config/config.go`, `internal/config/completion_timeout_test.go`,
`internal/agent/plan_review_policy.go`,
`internal/agent/plan_review_policy_test.go`, `internal/agent/plan_review.go`,
`internal/agent/completion_pipeline.go`, `internal/shellconfig/options.go`,
`schema.json`.

**Doğrulama:** Completion timeout ve plan review hedefli testleri, config ve
agent paket koşuları geçti. Build/vet başarılı.

**Kalan engel:** Agent genel race ve CI koşusu yapılmadı.

## O4 — MCP workspace/reconnect izolasyonu

**Kabul kriteri:** Channel mesajı yalnız exact canonical workspace ve current
connection generation'a ulaşmalı; eski/reconnect event'i ve bilinmeyen kimlik
düşürülmeli; gerçek app subscriber workspace seçmeli.

**Uygulama:** Event'e workspace ve generation eklendi. Transport ve buffered
publish scope taşıyor. Subscriber exact workspace/current generation dışını
düşürüyor. Bağımsız bağlantılarda `cfg=nil` olabilen workspace okuması
nil-güvenli yapıldı.

**Değişen dosyalar:** `internal/agent/tools/mcp/init.go`,
`internal/agent/tools/mcp/channel.go`,
`internal/agent/tools/mcp/channel_scope_test.go`, `internal/app/app.go`.

**Doğrulama:** Workspace/channel, unknown identity, stale generation ve
sessionless `cfg=nil` testleri geçti. MCP hedefli race koşusu geçti.

**Kalan engel:** CI üzerinde bütün reconnect/platform matrisi çalıştırılmadı.

## 5 / Browser-use — Web regresyon ve CI

**Kabul kriteri:** İkinci browser motoru olmadan sabit senaryolar fake model
ile tekrarlanmalı; yüksek riskli akışlar açık assertion'lara sahip olmalı;
alternatif recovery required hatayı başarı saydırmamalı; her koşu izole
profil/veri kullanmalı; timeout, cleanup, redacted screenshot/ledger, JUnit ve
nonzero exit olmalı; runner/assertion kontrat testleri bulunmalı; Playwright
kapsam eşdeğerliği olmadan korunmalı; desteklenmeyen platform skip/pass
olmamalı.

**Uygulama:** Go runner production workflow compiler/pipeline üzerinden
çalışıyor. Bağımsız evaluator her compiled action/verification için
`status=succeeded` ister; recovered failure veya alternatif adım kabulü
değiştirmez. Her senaryo yeni headless Chrome profili, state ve artifact
dizini alır. Runner deadline, `CloseAll`, JSON/JUnit, protected clean
screenshot ve redaction içerir. Ubuntu CI preflight/install ve kontrat
testlerini zorunlu kılar.

Fake model artık yalnız son kullanıcı mesajından sonraki tool sonucunu
özetliyor; aynı oturumda sıralı tool çağrıları doğru çalışıyor. `[[error]]`
kullanıcıya görünen HTTP 400 `Bad Request` durumunu, `[[permission]]` ise
gerçek riskli komut iznini üretiyor. Playwright ekran fikstürü bu davranışı
kullanıyor. Normal Chrome otomatik algılanıyor.

**Değişen dosyalar:** `internal/agent/tools/browser_regression.go`,
`internal/webui/webtest/browser_regression.go`,
`internal/webui/webtest/browser_regression_test.go`,
`internal/webui/webtest/webregress/main.go`,
`internal/webui/webtest/fakellm.go`, `web/e2e/browser-use-regress.ts`,
`web/e2e/screens.spec.ts`, `web/e2e/stack.ts`, `web/package.json`,
`web/tsconfig.json`, `Taskfile.yaml`, `.github/workflows/build.yml`,
`docs/browser-regression.md`.

**Doğrulama:** Browser workflow/pipeline/regression kontrat testleri geçti.
Normal Chrome ile şu senaryolar ve tüm adımları geçti:

1. `session-stream-tool-and-reopen`: proje/oturum, prompt, streaming, tool
   sonucu, reload, tek kullanıcı mesajı ve tek tool sonucu.
2. `cancel-and-model-error`: ilerleyen stream, Stop, iptal sonrası idle ve
   kullanıcıya görünen `Bad Request`.
3. `project-and-workspace-isolation`: ikinci projenin açılması, yeni oturum,
   yanıt ve yalnız geçerli workspace mesajının görünmesi.

`web/test-results/browser-use/*/report.json` ve `junit.xml` üretildi. Daha
önceki başarısız koşularda gerçek nonzero exit ve korunan failure screenshot
gözlendi. Son Playwright koşusu 12/12 geçti; ekran görüntüleri dahil edildi.

**Kalan engel:** GitHub Actions Ubuntu browser job'u çalıştırılmadı. Kapsam
eşdeğerliği kanıtlanmadığı için Playwright zorunlu kalır. Birleşik tools
paralel grubunda `TestCredentialFilteredScreenshotWorksAfterSignIn` bir kez
başarısız, izole koşuda başarılı oldu; paralel durum izolasyonu flaky riski
olarak açık kalıyor.

## Açık kalan kontroller ve riskler

- Tek `go test -failfast ./...` ve tek `go test -race -failfast ./...` tam
  başarı koşusu yok; hedefli race ve ayrı paket grupları kullanıldı.
- `TestCredentialFilteredScreenshotWorksAfterSignIn` paralel grupta flaky;
  izole geçmesi tüm paralel paylaşım sorununu çözüldüğünü kanıtlamaz.
- `node.exe` bulunmadığından Windows npm doğrudan-process entegrasyon testi
  skip edildi; başarılı çalıştırılmış gibi sayılmadı.
- `golangci-lint` ve log lint çalıştırılmadı.
- Gerçek CI, release, restore, SBOM/provenance ve deploy çalıştırılmadı.
- Kullanıcının başlangıçtaki diğer değişiklikleri korunmuştur; commit/push
  yapılmamıştır.
