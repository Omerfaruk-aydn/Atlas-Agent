# Katalog tamamlama planı

Kapsam: Atlas'ın model seçiminde kullanabildiği güncel metin, araç çağırma,
görüntü anlama ve reasoning modelleri; API, coding/token plan ve çalışan
hesap girişleri. Sağlayıcının yayımlamadığı özellikler tahmin edilmez.

- [x] Açık model listelerini sağlayıcının fiyat/yetenek metadata'sıyla
  karşılaştır. Senkronizasyon bütün kaynaklar doğrulanmadan dosya yazmasın.
- [x] Alibaba Coding CN ile Personal/Team Token Plan CN/SGP kataloglarını,
  ayrı anahtarlarını, kayıtlarını ve reasoning parametrelerini tamamla.
- [x] Qiniu, NVIDIA, Avian, Kimi ve diğer bölgesel kataloglardaki güncel
  modelleri resmî listelerle karşılaştır; desteklenmeyen kayıtları ayır.
- [x] Giriş listesi ve model seçimindeki yinelenen kayıtları düzelt.
- [x] Yeni bağlantıların ürettiği HTTP yolu, kimlik doğrulama başlığı,
  thinking/araç geçmişi davranışını test et. Gerçek ücretli çağrı yapma.
- [x] Bütün testleri ve derlemeyi çalıştır, bağımsız kod incelemesini
  tamamla; kaynak, tarih ve teknik sınırlamaları rapora kaydet.

Sonuç: ilgili paketler ve yönlendirme testleri ile derleme geçti. Tam
test süiti çalıştırıldı, altı pakette hata verdi; tam süitin geçtiği
iddia edilmez. Ayrıntılar [son raporda](provider-catalog-fixes-2026-10-01.md).

İnceleme odağı: bölge/plan anahtarlarının karışması, gateway protokolünün
yanlış seçilmesi, zorunlu thinking'in kapatılması, router fiyatının yanlış
sağlayıcıya bağlanması ve eski kullanıcı seçimlerinin istemeden değiştirilmesi.
