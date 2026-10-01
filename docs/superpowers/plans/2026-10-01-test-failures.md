# Altı paket test hatasının düzeltilmesi

**Hedef:** Agent, araçlar, MCP, Muse OAuth, shell ve shellconfig
başarısızlıklarının kök nedenini gidermek; hata kontrollerini korumak.

**Yöntem:** Önce mevcut başarısız testler ve yeni sleep regresyonları
ile hatayı yeniden üret, ardından en dar düzeltmeyi uygula.

1. Agent test yapılandırmasını kullanıcı config/data/cache dizininden
   ayır. Prompt hook testini harici grep olmadan gerçek stdin üzerinde
   çalıştır. Ortak geçici dizin yerine t.TempDir kullan.
2. MCP hata teşhis testlerinde sh bağımlılığı yerine gerçek Go test
   subprocess'i kullan; argv0, çıktı ve çıkış kodu kontrollerini koru.
3. Shell ve source testlerinde yolları Bash kurallarına göre tırnakla;
   boşluk, ampersand ve apostrof içeren yolları kapsa. Shebang testinde
   çalışan native Bash seç; Windows WSL shim'ini Bash sanma.
4. Windows Go coreutils katmanında bulunmayan sleep için kesirli süre,
   s/m/h/d, çoklu süre, taşma kontrolü ve context iptali desteği ekle.
   Standart Unix external command çözümünü değiştirme.
5. Muse home fixture'ında HOME ve USERPROFILE ortamını birlikte izole et.
6. Go dosyalarını gofumpt ile biçimlendir. Altı paketi, tam test süitini
   ve CGO_ENABLED=0 / GOEXPERIMENT=greenteagc derlemesini doğrula.
   Sonuçları önceki katalog raporuna bağlanan yeni bir rapora kaydet.

Commit veya PR bu isteğin kapsamında değildir.
