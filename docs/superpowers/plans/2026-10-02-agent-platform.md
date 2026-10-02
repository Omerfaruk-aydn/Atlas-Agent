# Atlas Agent Platform Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Onaylanmış on iki özelliği mevcut Atlas runtime'ına eksiksiz entegre etmek.

**Architecture:** Coordinator tek yürütme otoritesi olarak kalır. Dört alt proje
aynı artifact, görev, izin, bütçe ve kaynak kimliği sözleşmelerini paylaşır.
Bu belge diğer dört planın sırasını ve bütünleşik teslim kontrolünü tanımlar.

**Tech Stack:** Go 1.26.6 uyumlu kod, mevcut SQLite/engineering store, LSP,
Bubble Tea v2, yapılandırılmış OCI runtime ve gerçek PTY backend'leri.

**Spec:** `docs/superpowers/specs/2026-10-02-agent-platform-design.md`.

## Global Constraints

- `CGO_ENABLED=0`, `GOEXPERIMENT=greenteagc`; Go modül tabanı Go 1.26.6.
- Mevcut 1 MiB execution state sınırı korunur; artifact başına 32 MiB.
- Kaynak/spec hash, idempotency, izin ve bütçe hiçbir otomasyonla atlanamaz.
- Önceki dirty-tree geliştirmeleri korunur; yalnızca HEAD'den oluşturulan yeni
  worktree başlangıç olarak kullanılmaz. Isolated execution seçilirse mevcut
  çalışma ağacının güvenli snapshot'ı alınır; credential/ignored çıktılar taşınmaz.
- Benchmark, model sıralaması, ücretli provider denemesi veya publish yoktur.
- Mevcut rol/model seçimi korunur; yeni özellikler kendiliğinden model değiştirmez.
- Her Go dosyası gofumpt ile, yoksa goimports/gofmt ile formatlanır.

## Review Focus

- Eski geliştirmeleri kaybetmeden uygulama ve commit sınırlarını koruma.
- Test fixture'ının passing olmasını canlı model başarısı sanmama.
- Host Windows ile Linux container kanıtını farklı platformlar olarak raporlama.
- CLI/local/server/TUI'nin aynı state revision'ını göstermesi.
- On iki özellikten biri unavailable olduğunda bütün projeyi complete saymama.

## Plan sırası ve kapsam eşlemesi

| Sıra | Plan | Görevler | Özellikler |
| --- | --- | --- | --- |
| 1 | [A: Kaynak ve bağlam](2026-10-02-agent-platform-a-context.md) | A0, A1, A2 | 1, 2 ve ortak artifact deposu |
| 2 | [B: Yürütme](2026-10-02-agent-platform-b-execution.md) | B1, B2, B3, B4 | 7, 11, 4, 3 |
| 3 | [C: Koordinasyon](2026-10-02-agent-platform-c-coordination.md) | C1, C2, C3 | 5, 6, 12 |
| 4 | [D: Deneyim](2026-10-02-agent-platform-d-experience.md) | D1, D2, D3 | 8, 9, 10 |
| 5 | Bu plan | E1 | Birlikte teslim ve regresyon |

## Doğrulama komutlarının ortak biçimi

Komutlar PowerShell'de sırayla yürür; birden fazla Go süreci aynı anda çalışmaz.
Go yolu PATH'te yoksa `C:/Users/Ömer&Ceylin/AppData/Local/Programs/go/bin/go.exe`
kullanılır. Her test öncesi ortam:

```powershell
$env:CGO_ENABLED='0'
$env:GOEXPERIMENT='greenteagc'
$env:GOMEMLIMIT='1GiB'
$env:GOMAXPROCS='2'
```

Planlarda `go test` bu ortamda `-count=1 -p 1 -ldflags='-w -s' -timeout 3m`
ile yürütülür. Red aşaması hedef davranışın eksikliğiyle FAIL olmalı; unrelated
ortam/derleme hatası red kanıtı olarak kabul edilmez. Platform integration testleri
runtime/PTY gerçekten yoksa skip gerekçesiyle ayrıca unavailable olarak kaydedilir.

## Commit ve baseline kuralları

İlk uygulama işleminden önce mevcut git diff ve untracked dosya listesi yerel
baseline artifact'ı olarak kaydedilir; dosya içerikleri console'a dökülmez.
Önceki uncommitted kod commit'e yanlışlıkla dahil edilmez. Yeni dosyalar ve yeni
hunk'lar exact path/patch ile stage edilir; `git add .` kullanılmaz. Hunk ayrımı
güvenilir değilse commit bekletilir, ürün değişikliği kaybedilmez. Her alt görevin
önerilen semantic commit'i ilgili planda bulunur; commit öncesi staged diff
incelenir ve yalnızca doğrulanmış görev kapsamını içerdiği kontrol edilir.

### Task E1: Birleşik teslim ve son bağımsız inceleme

**Files:** Create `internal/agent/platform_integration_test.go`,
`docs/agent-platform.md`, `docs/agent-platform-verification.json`.
Modify `docs/engineering-runtime.md`, `docs/delivery-system.md`,
`docs/prompt-engineering.md`.

**Interfaces:** Dört planın public sözleşmeleri; mevcut workflow/delivery gate.

- [x] **Step 1:** `TestPlatformIntegrationRecoveryAndRemediation` yaz: mock
  provider ile iki uzman görevi/ortak contract, gerçek başarısız check, repair,
  finding ve restart oluştur. `require.False` eski source için stage pass;
  `require.True` yalnızca yeni matching checks ve bağımsız review sonrası pass.
- [x] **Step 2:** Hedef testi çalıştır; eksik bütünleşme nedeniyle FAIL gözlemle.
- [x] **Step 3:** Fixture'da görülen eksik wiring'i ilgili modülün sınırında düzelt;
  kontrol paneli ve CLI snapshot revision'larının eşitliğini aynı testte doğrula.
- [x] **Step 4:** `go test -count=1 -p 1 -ldflags='-w -s' -timeout 3m ./internal/agent -run TestPlatformIntegration`
  çalıştır; 0 fail/exit 0 gerekir.
- [x] **Step 5:** Dört planın paketleri dahil `go test -count=1 -p 1 -ldflags='-w -s' -timeout 3m ./...` çalıştır;
  paket sonuçlarını kaydet. Yeni değişiklik olmadan aynı full suite tekrar edilmez.
- [x] **Step 6:** `go build -ldflags='-w -s' -o <temp>/atlas-platform.exe .`
  çalıştır; Windows host build exit 0 gerekir.
- [x] **Step 7:** Gerçek CLI fixture smoke: graph query, environment inspect,
  recipe validate/run --plan-only, checkpoint/resume plan ve workflow status; paid provider
  çağrısı sayısı 0, kaynak mutation inspect için 0 olmalı.
- [x] **Step 8:** B1 OCI ve D2 gerçek PTY acceptance kontrollerini çalıştır;
  observed/pass/unavailable ve host/container OS bilgilerini ayrı kaydet.
- [x] **Step 9:** Seçilen execution yöntemiyle bağımsız inceleme yaptır; açık
  blocking bulgular varsa kapatıp yalnızca etkilenen doğrulamayı tekrarla.
- [x] **Step 10:** Her özelliğin çalışan giriş noktasını ve test kanıtını
  docs'a yaz; mock sonuçlarını canlı model kalite garantisi olarak sunma.
- [x] **Step 11:** Staged diff kontrolünden sonra
  `test: verify integrated agent platform workflows` semantic commit'i hazırla.

## Plan öz incelemesi ve yürütme kapısı

On iki özellik görevlerle eşlendi. Büyük alt sistemler ayrı planlandı; ortak
arayüz isimleri diğer planlarda aynı kullanılıyor. Testler kaynak değişimi,
restart, concurrency, izin reddi ve eksik araç koşullarını kapsıyor.
Bu planlar uygulama talimatıdır; henüz yürütülmediler. Kullanıcının plan
incelemesi ve native/subagent-driven execution seçimi beklenir.

Uygulama ilerleyişi bu dosyaların checkbox'larında tutulur. Kullanıcı native
yöntemi seçerse aynı oturumda görev sırasıyla uygulama ve son bağımsız inceleme;
subagent-driven seçerse her göreve ayrı uygulayıcı ve bağımsız inceleme kullanılır.
Execution yöntemi seçimi, ürün içindeki Atlas uzman ajanlarının çalışmasını
etkilemez; bu geliştirme işinin nasıl yürütüleceğini belirler.

Execution closure: see docs/agent-platform-progress.md and docs/agent-platform-verification.json. Checkboxes track closure under recorded rulings; unavailable platform acceptance is not a passing runtime test. E1 aggregate RED was waived because its initial failure was a test assertion, not missing runtime behavior. The graph smoke uses the production tool fixture rather than inventing a CLI command.
