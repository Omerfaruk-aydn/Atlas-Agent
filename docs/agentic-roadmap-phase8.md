# Faz 8 raporu — Harness öz-iyileştirmesi

## 1. Özet

- ✅ **8.1 Kanıtlı öneri üretimi:** Failure corpus vakaları normalize edilmiş neden/imza ve proje türüne göre kümeleniyor.
- ✅ Her öneri en az iki bağımsız kanıt vakasına ve oturum kimliklerine bağlı; tek olaydan genel kural üretilmiyor.
- ✅ `ABRequired=true`, `AutoApply=false`; gerçek-model A/B ve kullanıcıya diff sunumu için açık onay politikası CLI çıktısında yer alıyor.
- ✅ `atlas eval corpus suggest` makine-okunur JSON üretiyor; hiçbir prompt, kural, araç veya varsayılan otomatik değiştirilmiyor.

## 2. Tasarım ve değişen dosyalar

Öneri motoru corpus kayıtlarındaki gerçek başarısızlıkları gruplar, kanıt kimliklerini saklar ve sınırlı iyileştirme taslakları üretir. Kanıtsız veya tek vakalı grup öneri olmaz. Uygulama katmanı bulunmaz; kullanıcı vakaları export edip Faz 1 ablasyon/bench altyapısıyla A/B ölçebilir.

Yeni: `internal/harnessmetrics/suggestions.go`, `suggestions_test.go`. Genişletilen: `internal/cmd/eval_corpus.go` ve corpus testleri. Mevcut kirli çalışma korundu.

## 3. Doğrulama kanıtı

- `go test -p 1 ./internal/harnessmetrics`: geçti.
- `go test -p 1 ./internal/cmd`: geçti; yalnızca kullanıcının açıkça hariç tuttuğu eski Windows npm testi atlandı.
- Tek vaka reddi, yinelenen vaka kümesi, kanıt kimlikleri, A/B zorunluluğu ve otomatik uygulamama testleri geçti.
- `go vet ./...`, `task lint:fix`, `git diff --check`: geçti.
- Gerçek model A/B veya ücretli benchmark çalıştırılmadı; bu açık kullanıcı onayına bırakıldı.

## 4. Config ve varsayılanlar

Yeni config alanı veya arka plan görevi yok. `atlas eval corpus suggest` kullanıcı çalıştırınca mevcut data-dir corpus'unu okur. Varsayılan çıktı yalnızca öneri JSON'u ve inceleme politikasıdır; hiçbir öneri kendiliğinden uygulanmaz.

Kullanım `docs/automation-and-distribution.md` içinde belgelenmiştir.

## 5. Ölçüm

Her öneri kaynak vaka sayısı ve kanıt oturumlarıyla denetlenebilir. Sonraki A/B ölçümü Faz 1'in pass@1/pass@k, token, maliyet, süre ve fail-open karşılaştırmasını kullanır. Bu fazda gerçek-model ölçümü yapılmadığı için performans kazancı iddia edilmez.

## 6. Riskler ve sınırlamalar

Kümeleme kayıtlı corpus kalitesiyle sınırlıdır; yinelenme nedensellik kanıtı değildir. Öneriler insan incelemesi ve kontrollü A/B olmadan üretim değişikliği sayılmaz. Corpus'ta yeterli kanıt yoksa boş sonuç doğru davranıştır.

## 7. Önerilen commit dilimleri

- `feat: suggest evidence-bound harness improvements`
- `test: require recurring evidence for corpus proposals`
- `docs: document review-only improvement workflow`

Commit, push veya PR yapılmadı.

## 8. Sonraki adım

Faz 8 yol haritasının son fazıdır. Kapanış genel regresyon ve kalite kapılarının gerçek sonuçlarıyla yapılır; gerçek-model A/B için ayrı onay gerekir.
