# Faz 6 raporu — Paralel doğruluk

## 1. Özet

- ⚠️ **6.1 Paralel deneme + turnuva:** Mevcut iki-worktree alternatif akışı kanıt özetleri, deterministik eleme ve turnuva seçimiyle genişletildi. Yol 3–5 bağımsız denemeyi genel bir kullanıcı seçeneği olarak değil, mevcut iki alternatifli dar akışta sunuyor.
- ✅ **6.2 Önce keşif, sonra zenginleştirme:** Alternatiflere proje haritası/etki ve doğrulama bağlamı taşınıyor; seçim yalnızca modelin kısa yanıtına bırakılmıyor.
- ✅ **6.3 Ara noktadan replay:** Başarısız deneme son yeşil checkpoint'e bağlı izole worktree'de tek sınırlı ve farklı stratejili replay alabiliyor; ana checkout korunuyor.

## 2. Tasarım ve değişen dosyalar

Her aday yaklaşım, dosyalar, kontroller, regresyon ve riskleri içeren `AlternativeSummary` üretir. Sıfır regresyonlu adaylar deterministik olarak elenir/sıralanır; model turnuvası yalnızca kalanlar arasında kimlik seçer ve geçersiz cevap mevcut deterministik kazananı korur. Kazanan değişiklik ana ağaca patch ile uygulanır ve kontratın yeniden doğrulaması gerekir. Replay kullanıcı kirli ana ağacını resetlemez.

Yeni: `internal/agent/alternative_tournament.go` ve testleri. Genişletilen: `contract_alternatives.go`, `workplan/checkpoint.go`, `harness_extras_test.go` ve ilgili harness metrik entegrasyonları.

Mevcut kirli dosyalardaki kullanıcı çalışması korundu; testlerin oluşturduğu git commit'leri yalnızca `t.TempDir()` altındaki geçici depolarda yapıldı.

## 3. Doğrulama kanıtı

- `go test -p 1 ./internal/agent`: geçti.
- Turnuva eleme, geçersiz model kararında fail-closed incumbent, aday özeti, worktree uygulaması ve replay testleri geçti.
- `TestReplayFromGreenPreservesMainCheckout`: geçti; ana ağaçtaki kullanıcı içeriği korunuyor.
- `go test -p 1 ./internal/workplan`: geçti.
- `go vet ./...`, `task lint:fix`, `git diff --check`: geçti.
- Gerçek model turnuvası veya ücretli benchmark çalıştırılmadı.

## 4. Config ve varsayılanlar

Mevcut `harness.parallel_alternatives` varsayılan açık davranışı korunur; tetikleme art arda kontrat başarısızlığına bağlıdır. Yeni model turnuvası için bağımsız otomatik mod eklenmedi. Mevcut subagent eşzamanlılık ve model/bütçe sınırları geçerlidir. Kaybeden worktree'ler mevcut yaşam döngüsüyle temizlenir; replay sınırlıdır.

## 5. Ölçüm

Alternatif oluşturma, kontrol sonucu, kazanan, replay ve hata/fail-open olayları harness ölçümlerine yazılır. Model karşılaştırma çağrıları token/süre telemetrisini kullanır. Yerel ölçüm gerçek para harcamamıştır.

## 6. Riskler ve sınırlamalar

Mevcut üretim yolu iki alternatifle sınırlı; yol haritasındaki 3–5 genel turnuva havuzu tam genişletilmedi. Model yanlış veya kullanılamaz olursa deterministik aday seçimi kullanılır. Kazanan patch'in uygulanması tek başına tamamlanma kanıtı değildir; kontrat yeniden yeşil olmalıdır. Keşif kalitesi mevcut proje haritasının kapsamına bağlıdır.

## 7. Önerilen commit dilimleri

- `feat: rank worktree alternatives with evidence tournaments`
- `feat: replay failed alternatives from green checkpoints`
- `test: preserve user changes during alternative replay`
- `docs: record alternative tournament limits`

Commit, push veya PR yapılmadı.

## 8. Sonraki faz

Toplu devam onayıyla Faz 7 uygulandı.
