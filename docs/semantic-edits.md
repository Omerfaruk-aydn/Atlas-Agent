# Anlamsal düzenleme planları

`lsp_rename`, `lsp_replace_symbol` ve `lsp_rename_file` araçlarına
`preview: true` gönderildiğinde dosyalar değiştirilmez. Sonuç, UUID plan
kimliğini, bütün gerçek hedefleri ve değişmez plan/fark artifact referanslarını
içerir. Kaydedilmiş planı `lsp_edit_plan` ile inceleyebilir, uygulayabilir veya
yarım kalan işlemini kurtarabilirsiniz:

```json
{"action":"inspect","plan_id":"<plan UUID>"}
{"action":"apply","plan_id":"<plan UUID>"}
{"action":"recover","plan_id":"<plan UUID>"}
```

Uygulama, kaynak hashlerini ve görevin dosya sahipliğini doğrular; değişecek
her dosya için normal izin akışından geçer. İzin isteği gerçekten uygulanacak
eski/yeni içeriği gösterir. İzin sırasında kaynak değişirse hiçbir plan yazımı
başlatılmaz. Salt okunur uzmanlar uygulama veya kurtarma yapamaz.

UTF-8/UTF-16/UTF-32 konumları byte aralıklarına doğrulanarak çevrilir; yarım
Unicode karakterleri, çakışan düzenlemeler ve desteklenmeyen bare-CR satırlar
reddedilir. CRLF kaynakların satır sonları korunur. Dosya oluşturma, silme ve
taşıma ile referans düzenlemeleri aynı hedef listesinde ön kontrolden geçer.
Proje dışı hedefler, Git metadata, symlink yolları ve özel dosyalar reddedilir.
Dosya başına sınır 1 MiB, eski/yeni içerik toplamı ayrı ayrı 8 MiB, hedef sınırı
128 ve journal sınırı 32 MiB'dir.

Çok dosyalı uygulama bir işletim sistemi işlemi olarak atomik değildir. Her
dosya ayrı değiştirilir; önce kaydedilen journal, önceki içeriği ve her yazımın
durumunu saklar. Süreç yeniden başladığında tamamlanmamış işlem otomatik olarak
tekrarlanmaz. `inspect`, gerçek journal durumunu da gösterir. Açıkça başlatılan
`recover`, yalnızca hashleri kaydedilmiş yazımla eşleşen içerikleri geri alır.
Sonradan yapılan kullanıcı değişiklikleri korunur ve çakışma olarak bildirilir.
Journal hedefleri, ilk kaydedilmiş planla eşleşmek zorundadır.

Çözümlenmemiş uygulama/kurtarma veya çakışma görev/aşama sertifikasyonunu ve
resume hazır görevlerini engeller. Uygulama başarılı olsa bile test veya
bağımsız inceleme başarılı sayılmaz. Kurtarma uygulama bütçesi tükense de
çağrılabilir; dosya izinleri ve sahipliği yine gereklidir.

Windows üzerinde dosya içerikleri flush edilir ve süreç çökmesi kurtarması
test edilir. Ani elektrik kesilmesine karşı çok dosyalı dayanıklılık garantisi
verilmez. Proje dışındaki başka bir uygulamanın son hash kontrolü ile dosya
değiştirme arasına yazmasını işletim sistemi düzeyinde atomik CAS ile önleme
garantisi yoktur. Kaynak değişikliklerini uygulama boyunca en dar aralıklarda
yeniden kontrol ederiz; ilk LSP isteğinin dosyası da yanıt sırasında yeniden
kontrol edilir. LSP sunucusunun kendi eski analizini kullandığı bütün durumlar
bu hash kontrolleriyle kanıtlanamaz; gerçek doğrulama ve inceleme hâlâ gerekir.
