# Faz 4 raporu — Model yönlendirme

## 1. Özet

- ✅ **4.1 Aşamaya göre model/effort:** Keşif, planlama, uygulama, doğrulama ve inceleme için opt-in model rolleri eklendi; aşama geçişleri gözlenebilir.
- ✅ **4.2 Architect/editor:** Yapılandırılmış plan, plan dışına çıkma kontrolü ve architect'e dönüş akışı mevcut subagent altyapısına bağlandı.
- ✅ **4.3 Oracle:** Salt-okunur, bütçeli ve yapılandırılabilir `oracle` aracı; otomatik advisor/escalator ile aynı model çözümleme düzenini kullanıyor.
- ✅ **4.4 Farklı aileden inceleyici:** Uygun ve yetkili alternatif sağlayıcı varsa evaluator ailesi yazardan farklı seçiliyor; yoksa mevcut davranışa düşüş açıkça kaydediliyor.

## 2. Tasarım ve değişen dosyalar

Aşama seçimi araç kullanımı ve kontrat kanıtından türetilir; her adımda model değiştirilmez. Roller yalnızca açıkça yapılandırıldığında çözümlenir, böylece ek maliyet ve cache kırılması varsayılan olarak oluşmaz. Architect planı veri olarak taşınır. Oracle kısıtlı adım ve araç setiyle çalışır. Model yokluğu yeni bir sağlayıcıya gizlice bağlanmaya yol açmaz.

Yeni başlıca dosyalar: `internal/agent/phase_models.go`, `phase_routing.go`, `architect_plan.go`, `oracle.go`, `evaluator_family.go` ve testleri. Mevcut entegrasyonlar: `coordinator.go`, `agent.go`, `evaluator.go`, `advisor.go`, `attempt_model.go`, `model_fallback.go` ve `config/config.go`.

Başlangıçtan beri kirli olan bu mevcut dosyalar korunarak genişletildi; kullanıcı işi geri alınmadı. Yeni yardımcı dosyalar faz kapsamında eklendi.

## 3. Doğrulama kanıtı

- `go test -p 1 ./internal/agent`: geçti.
- Aşama çözümleme, aşama geçişleri, architect plan doğrulama, oracle sınırları ve evaluator aile seçimi deterministik/mock testlerle geçti.
- `go test -p 1 ./internal/config ./internal/cmd`: geçti; bilinen ve açıkça hariç tutulan Windows npm testi hariç.
- `go vet ./...`, `task lint:fix`, `git diff --check`: geçti.
- Gerçek veya ücretli model çağrısı yapılmadı; Antigravity bench sağlayıcısı kullanılmadı.

## 4. Config ve varsayılanlar

Aşama rolleri `options.model_roles` altında `exploration`, `planning`, `implementation`, `verification` ve `review` adlarıyla tanımlanır. Tanımsız roller oturum modelini kullanır. Oracle/escalator mevcut yapılandırılmış model rollerini kullanır; otomatik model satın alma veya yeni sağlayıcı yetkilendirme yapmaz. `harness.stall_oracle` varsayılan `false`; duraklamada ek model çağrısı opt-in'dir. Evaluator için farklı aile tercihi yalnızca mevcut, yetkili ve kullanılabilir modeller arasından yapılır.

## 5. Ölçüm

Aşama geçişi, model seçimi/fallback, oracle kullanımı ve reviewer çağrıları mevcut trace ve harness metriklerine yazılır. Model çağrılarının token, maliyet ve süresi var olan telemetriyle kaydedilir. Bu fazın yerel testleri sahte sağlayıcı kullandığından gerçek maliyet ölçümü yoktur.

## 6. Riskler ve sınırlamalar

Aşama sezgisel sınıflandırması sınır durumlarda yanlış ad verebilir; bu, yetki veya kontratı değiştirmez. Farklı model ailesi her ortamda bulunmayabilir. Architect/editor kalitesi seçilen rollere bağlıdır ve üretim kalite karşılaştırması onaylı bench gerektirir. Ek çağrıların maliyet etkisi config'e bağlıdır; varsayılan tek-model davranışı korunur.

## 7. Önerilen commit dilimleri

- `feat: route agent phases to configured model roles`
- `feat: add structured architect editor handoff`
- `feat: add bounded read-only oracle consultations`
- `feat: prefer independent evaluator model families`
- `test: cover phase routing and model fallbacks`

Commit, push veya PR yapılmadı.

## 8. Sonraki faz

Toplu devam onayıyla Faz 5 uygulandı.
