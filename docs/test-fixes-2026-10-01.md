# Altı paket test hatasının kök nedenleri ve düzeltmeleri

Bu çalışma, katalog güncellemesinin ardından tam süitte başarısız olan
altı paketi düzeltir. Hata kontrolleri korunmuştur; başarısız testler
atlatılmadı ve zaman sınırları artırılarak gizlenmedi.

| Paket | Kök neden | Düzeltme |
| --- | --- | --- |
| `internal/agent` | Hermetic test helper gerçek kullanıcının global yapılandırmasını ve kayıtlı model rollerini okuyordu. Prompt hook testi PATH'teki harici `grep` komutuna bağımlıydı. | Config/data/cache dizinleri `t.Setenv` ve `t.TempDir` ile izole edildi. Hook gerçek stdin'i Bash builtins ile okur; eşleşen ve eşleşmeyen prompt doğrulanır. Ortak `/tmp` alanı kaldırıldı. |
| `internal/agent/tools` | Windows Go coreutils katmanı `sleep` sağlamıyordu. Beklemesi gereken komutlar 127 koduyla hemen bitiyor, arka plan ve bekleme kontrolleri gerçek işi sınayamıyordu. | Gömülü coreutils katmanına context ile iptal edilebilir `sleep` eklendi. Normal Unix PATH çözümlemesi ve coreutils'i kapatma seçeneği korundu. |
| `internal/agent/tools/mcp` | Test fixtures Windows PATH'inde olmayan `sh` ile çalışıyordu. | Gerçek Go test executable'ı child process olarak kullanılır. Tam argv, stdout/stderr teşhisi ve orijinal çıkış kodu kontrol edilir. Üretim transport davranışını değiştirmek gerekmedi. |
| `internal/oauth/muse` | Test sadece `HOME` değerini değiştiriyordu; Windows `os.UserHomeDir` için `USERPROFILE` kullanır. | Her iki home ortam değişkeni izole edildi; gerçek platform davranışı korunur. |
| `internal/shell` | Tırnaksız geçici yollar içindeki `&` shell operatörü oluyordu. PATH'teki Windows `bash.exe` çalışan Git Bash yerine WSL shim'i olabiliyordu. | Shell yolları `syntax.Quote` ile yazılır. Boşluk, `&` ve apostrof test edilir. Shebang testi native Bash'i gerçek bir komutla doğrular ve testin PATH'ine ekler. |
| `internal/shellconfig` | `source` komutuna verilen tırnaksız dosya yolu shell tarafından bölünüyordu. | Include yolu Bash kurallarıyla tırnaklanır; metakarakterli dizinde her iki sağlayıcının da yüklendiği doğrulanır. |

## Sleep sözleşmesi

Go coreutils etkin olduğunda `sleep` harici bir executable gerektirmez.
Kesirli süreler, çoklu süre toplamı ve `s/m/h/d` son ekleri desteklenir.
Eksik, negatif, NaN/Inf veya taşan süreler açık hata ve çıkış kodu 1
üretir. Context iptali uzun beklemeyi sonlandırır; timer temizlenir.

Regresyon testleri boş PATH ile çalışır: kurulu bir `sleep` executable'ı
testi yanlışlıkla geçiremez. Süre bekleme, cancellation ve geçersiz
girdi testleri düzeltmeden önce başarısız, sonra başarılı oldu.

## Doğrulama

Agent, MCP, Muse, shell ve shellconfig paketleri; araç paketindeki
üç önceki başarısızlık ve yeni sleep regresyonları yeniden geçti.
**Tam test süiti geçti:** `go test -p 1 -timeout 3m ./...` çıkış kodu 0; 96 test paketi başarılı. `CGO_ENABLED=0 GOEXPERIMENT=greenteagc` ile son derleme de geçti.

Tam proje testinin ve son derlemenin sonucu
[makine okunabilir kayıtta](test-fixes-verification-2026-10-01.json) bulunur.

Yeni ve değiştirilen Go dosyaları gofumpt ile biçimlendirildi.
Bağımsız kod incelemesi; iptal, süre taşması, platform davranışı ve
test izolasyonunda önemli bir sorun bulmadı.
