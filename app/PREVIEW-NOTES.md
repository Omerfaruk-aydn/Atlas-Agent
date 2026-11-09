# Atlas masaüstü önizlemesi

## Uygulananlar

- Referans görsellerdeki gibi tek satırlık mesaj kutusu; uzun metinde büyüyen alan ve altında proje/izin/bağlam kontrolleri.
- `/` ile yerleşik komutlar ve projenin gerçek komut/becerileri: arama, ok tuşları, Enter/Tab ile seçme, Escape ile kapatma.
- Artı menüsünde Planla, Hata ayıkla, Çoklu görev ve Sor; dosya/görsel ekleme, komutlara ve MCP listesine erişim.
- Model düğmesinin üzerinde açılan aramalı model listesi; son kullanılanlar, akıl yürütme seviyesi ve sağlayıcı/bağlam/fiyat bilgisi.
- Otomatik model seçimi: etkinleştirildiğinde o anda yapılandırılmış büyük/küçük modelleri hatırlar. Eki olmayan 240 karakterden kısa istemlerde küçük modeli, diğerlerinde büyük modeli kullanır. Küçük model yoksa büyük modele döner. Çalışan bir tur sırasında model değiştirmez.
- MCP araması, gerçek etkinlik anahtarları, hata bilgisi, yeniden deneme ve kimlik doğrulaması gereken sunucularda tarayıcıyla bağlanma. Anahtarlar proje kapsamındaki ayarı değiştirir; çalışan bir tur varken değişiklik reddedilir.
- Gerçek mikrofon seviyesinden dalga çizimi; süre, iptal, onay ve yerel Vosk ile sesli yazı. Ses taslağa eklenir, kendiliğinden gönderilmez. Kayıt 120 saniyeyle sınırlıdır; mikrofon iş bitince veya pencere kapanınca serbest bırakılır. Geçici WAV dosyası silinir.
- Kamera izni verilmez. Mikrofon yalnızca Atlas'ın yerel sayfasına açılır.
- Açık/koyu tema, dar pencere ve ekran okuyucu kontrolleri.

Çalışma biçimleri modele verilen yönergeyi değiştirir. Planla ayrıca sunucunun plan izin modunu kullanır. Çoklu görev seçeneği işi bölme yönergesidir; bağımsız birden çok ajan başlatma garantisi değildir.

## Sonraki geliştirmeler için öneriler

1. **Değişiklikleri gözden geçirme:** dosya bazında diff, test sonucu ve seçili değişikliği geri alma; dosya kaybını önleyen önizleme gerekir.
2. **Görev başına çalışma ağacı:** ayrı branch/worktree seçimi ve görevler arası geçiş; aynı dosyaya eşzamanlı yazmayı önlemek için yararlıdır.
3. **Bağlam yönetimi:** eklenmiş dosyaların token tahmini, oturum boyunca sabitleme ve gönderilmeden önce bağlam önizlemesi.
4. **MCP araç izinleri:** sunucu listesinin yanında araç bazında izin, bağlantı testi ve son hata zamanı. Kimlik bilgileri arayüze gönderilmemelidir.
5. **Ses tercihleri:** mikrofon cihazı seçimi, basılı tutarak konuşma ve otomatik sessizlik sonlandırması. Sessizlik eşiği gerçek cihazlarla doğrulanmalıdır.

## Araştırma kaynakları

- [Cursor komutları](https://prod.cursor.com/docs/cli/reference/slash-commands): komut keşfi ve MCP erişimi.
- [Cursor modları](https://docs.cursor.com/en/agent/modes): çalışma biçimlerinin görevlerle ilişkisi.
- [Cursor MCP](https://prod.cursor.com/docs/cli/mcp): sunucu ve araç yönetimi.
- [MDN AudioWorkletNode](https://developer.mozilla.org/en-US/docs/Web/API/AudioWorkletNode): ses işleme iş parçacığında PCM yakalama.
- [MDN AnalyserNode](https://developer.mozilla.org/en-US/docs/Web/API/AnalyserNode): gerçek ses seviyesinden görselleştirme.
- [Electron oturum izinleri](https://www.electronjs.org/docs/latest/api/session): yerel mikrofon erişimini sınırlama.

## Test sürümü

Açılış sırasında gelen ek başlatma istekleri sunucu hazır olana kadar bekler. Arayüzün React kabuğunu yüklediği ayrıca doğrulanır; yükleme hatası veya render süreci çökmesinde bir kez yeniden denenir. Kalıcı hatada boş pencere yerine yeniden deneme/kapatma bildirimi gösterilir. Windows'ta sürücüye bağımlı boş ekranları önlemek için 2D arayüz yazılımla çizilir. Yerel açılış tanıları `Atlas Preview/startup.log` dosyasına yazılır; bağlantı anahtarları ve sohbet içeriği kaydedilmez.

Önizleme sürümü ayrı `Atlas Preview` profili kullanır ve normal Atlas'ın yanında çalışabilir. Model sağlayıcıları ve projeler mevcut Atlas çekirdeği üzerinden kullanılır; bu nedenle burada gönderdiğiniz gerçek istemler normal sağlayıcı kullanımınıza sayılır. Bir GitHub/npm sürümü yayımlanmadı.
