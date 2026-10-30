# Faz 3 raporu — Bağlam yönetimi

## 1. Özet

- ✅ **3.1 Prompt cache koruma:** Goal, kontrat ve çalışma zamanı bağlamı değişken son kullanıcı mesajına taşındı; sabit system prompt goal değişiminde bayt düzeyinde korunuyor.
- ✅ **3.2 Context anxiety önlemi:** Yüksek dolulukta acele bitirme yerine kanıtlı ilerleme ve handoff yönlendirmesi veriliyor.
- ✅ **3.3 Uyarlamalı budama:** Eski büyük araç çıktıları yalnızca modele giden görünümde kısaltılıyor; son adımlar, hatalar ve tool-call/result eşleşmeleri korunuyor.
- ✅ **3.4 Boşta sıkıştırma:** Opt-in, iptal edilebilir eşik ve token tabanlı boşta sıkıştırma eklendi.
- ✅ **3.5 Büyük eklerde yol referansı:** Metin ekleri eşik üzerinde yol, boyut ve önizlemeye dönüşüyor; medya davranışı korunuyor.
- ✅ **3.6 Handoff:** Yapılandırılmış çalışma paketi, bağlantılı yeni oturum, kalıcı kanıt ve TUI `/handoff` akışı tamamlandı.

## 2. Tasarım ve değişen dosyalar

Sabit prompt ile değişken çalışma bağlamı ayrıldı. Budama kalıcı konuşma geçmişini değiştirmez; handoff ise özetin yerine kontrat, kararlar, açık işler ve doğrulama kanıtlarını taşır. Hata durumunda yeni oturum eskisine bağlı kalır ve çalışma ağacı geri alınmaz.

Başlıca yeni dosyalar: `internal/agent/dynamic_context.go`, `context_fit.go`, `context_window.go`, `idle_compaction.go`, `attachment_context.go`, `session_handoff.go`, `internal/ui/model/handoff.go` ve bunların testleri. Genişletilen mevcut dosyalar arasında `agent.go`, `budget_line.go`, `feature_handoff.go`, `message/content.go`, `ui/model/ui.go`, `workspace/app_workspace.go`, `config/config.go`, `shellconfig/options.go` ve `schema.json` bulunuyor.

Başlangıçta commit edilmemiş mevcut çalışma korunmuştur. Özellikle yukarıdaki mevcut dosyaların diff'leri daha önce kirli çalışma ağacıyla aynı dosyaları içerir; hiçbir değişiklik reset, checkout veya stash ile ayrıştırılmamıştır. Yeni `phase3` dosyaları bu fazda eklenmiştir.

## 3. Doğrulama kanıtı

Go 1.27.3, `CGO_ENABLED=0`, `GOEXPERIMENT=greenteagc`:

- `go test -p 1 ./internal/agent ...`: geçti; son ana paket koşusunda agent 71.354 s.
- `go test -p 1 ./internal/ui/model ./internal/config ./internal/cmd ...`: geçti; yalnızca kullanıcının hariç tuttuğu eski Windows `TestNpmCommandWindowsRunsNodeWithoutShellQuoting` atlandı.
- `go vet ./...`: geçti.
- `task lint:fix`: sıfır sorun.
- `git diff --check`: geçti; yalnızca mevcut `coder.md.tpl` CRLF→LF çalışma kopyası uyarısı.
- Handoff, prompt sabitliği, budama, ek referansı, boşta sıkıştırma ve TUI testleri ilgili paketlerde çalıştırıldı.
- Gerçek modelle bilgi-kayıbı karşılaştırması veya ücretli bench çalıştırılmadı; Faz 1'in deterministik retention eval altyapısı kullanılabilir.

## 4. Config ve varsayılanlar

`options.harness.attachment_reference_bytes`: varsayılan 16 KiB; `-1` kapatır. Küçük dosyalarda mevcut içerik davranışı korunur.

`options.harness.idle_compact_minutes`: varsayılan `0`, kapalı. `idle_compact_tokens`: varsayılan 32.000. Arka plan model çağrısı ve maliyeti ancak kullanıcı boşta sıkıştırmayı açarsa oluşur. Değerler JSON Schema ve atlasrc seçenekleriyle sunulur.

Handoff kullanıcı veya mevcut bağlam yönetimi tarafından tetiklenir; dosya geri alma yapmaz. Dinamik bağlam ve budama ek model çağrısı yapmaz.

## 5. Ölçüm

Mevcut trace/harness olaylarına compaction, handoff, aşama/kapamaya ilişkin karar ve kullanım verileri bağlandı. Model token ve cache kullanımı sağlayıcının döndürdüğü ölçümlerle kaydedilir. Yerel testlerde ücretli model maliyeti oluşmadı; gerçek cache isabet oranı sağlayıcıya ve model ailesine bağlıdır.

## 6. Riskler ve sınırlamalar

Budama isim/büyüklük tabanlı genel sezgiler kullanır; yanlış pozitif durumda model kalıcı geçmişteki kaynağı yeniden okuyabilir. Boşta sıkıştırma sağlayıcı cache süresini tahmin etmek yerine muhafazakâr kullanıcı eşiğine bağlıdır. Farklı modellerde handoff ile özetlemenin kalite farkı ancak onaylı gerçek-model A/B eval ile ölçülebilir.

## 7. Önerilen commit dilimleri

- `feat: preserve prompt cache with dynamic work context`
- `feat: add adaptive context pruning and attachment references`
- `feat: add cancellable idle compaction`
- `feat: hand off long sessions with durable evidence`
- `test: cover context lifecycle and handoff behavior`

Commit, push veya PR yapılmadı.

## 8. Sonraki faz

Kullanıcının toplu devam onayıyla Faz 4 uygulandı; ayrı onay beklenmedi.
