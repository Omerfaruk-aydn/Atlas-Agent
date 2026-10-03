# Kalıcı ajan platformu

Bu özellikler mevcut Atlas araç izinlerini, hook'larını, execution/journal ve SQLite altyapısını kullanır. Browserbase veya ücretli bulut tarayıcısı gerektirmez.

## 1. Kimlik bilgisi kasası

```powershell
atlas-agent vault add github --origin https://github.com --username kullanici
atlas-agent vault list
atlas-agent vault remove github
```

Parola terminalde maskeli girilir; komut argümanına, sohbete veya atlasrc'ye yazılmaz. Windows'ta kayıt kullanıcı hesabına bağlı DPAPI ile korunur. Windows dışındaki sistemlerde secret manager'dan sağlanan, base64 biçiminde 32 baytlık `ATLAS_VAULT_KEY` gerekir; anahtar veritabanına yazılmaz. Kasa çalışma alanı scope'undadır.

Browser açıkken ajan `vault_list` ile handle/origin/identifier görür. `vault_fill` çağrısı `credential_id` ve güncel password input ref/selector kullanır. Doldurma aynı JavaScript işlemi içinde tam HTTPS origin ve tek, görünür, etkin password input kontrolünden sonra yapılır; HTTP ve origin uyuşmazlığı reddedilir. Bu işlem formu göndermez ve başarılı giriş iddiasında bulunmaz.

Parola araç sonuçlarına dönmez. Sonraki metin sonuçlarında bilinen parola ve JSON-escaped biçimi redakte edilir. Görüntüler metin redaksiyonuyla güvenli hale getirilemediği için credential kullanımından sonra bu süreçte model medya gözlemleri, interaction preview ve trace image kayıtları kapatılır. Yeni süreçte yeniden başlar. Üçüncü taraf sitelerin davranışı ve kullanıcı hesabında çalışan başka yazılımlar kasanın güvenlik sınırının dışındadır; bu bir sandbox veya cihaz ele geçirilmesine karşı koruma değildir. Kasa ilk sürümde parola doldurur; passkey/telefon onayı atlatmaz, ödeme bilgisi yönetmez.

## 2. Heartbeat ve ajan cron

```powershell
atlas-agent agent-jobs add ci --kind cron --prompt "CI durumunu kontrol et" --every 10m --max-runs 10 --enabled
atlas-agent agent-jobs add deploy --kind heartbeat --session FULL_SESSION_ID --prompt "Deploy durumunu takip et" --every 5m --enabled
atlas-agent agent-jobs list
atlas-agent agent-jobs pause ci
atlas-agent agent-jobs resume ci
atlas-agent agent-jobs worker
atlas-agent agent-jobs worker --once
```

CLI ile eklenen iş varsayılan olarak paused olur; `--enabled` açık kullanıcı tercihiyle etkinleştirir. Ajanın `agent_jobs add` aracı normal izin talebiyle çalışır. Heartbeat aynı konuşmaya, cron yeni bir konuşmaya girer. Interval en az 60 saniyedir; varsayılan timeout 5 dakika ve maksimum çalıştırma sayısı 10'dur.

Atlas veya explicit worker süreci açıkken 15 saniyelik poll ile işler çalışır. Bu bir işletim sistemi servisi değildir; Atlas kapalıyken model çalışmaz. Kalıcı kayıtlar yeniden açılınca kullanılabilir; kaçırılan tick'ler birleştirilir. Kullanıcı işi meşgulken heartbeat ertelenir; aktif hedef döngüsüyle yarışmaz. Cron/heartbeat hata alınca duraklar. Çökme sonrası kayıtlı attempt otomatik tekrar edilmez: lease süresi dolunca `recover`, inceleme sonrası `resume` kullanılır. Run bütçesi yeni model çalıştırmasından önce rezerve edilir.

Unattended işler yeni izin diyaloğu beklemez: mevcut explicit tool allowlist, hook grant ve diğer ön izinler korunur; yeni kullanıcı onayı gerekiyorsa iş anlaşılır hata ile duraklar. Worker'da soru aracı açık değildir. Zamanlanan ajan yeni zamanlama oluşturamaz. Sonuçlar SQLite/job görünümünde ve ilgili oturumda kalır; dışarıya mesaj gönderilmez. `NO_CHANGE` kısa raporu kaydedilebilir; heartbeat aynı konuşmada normal bir turdur.

TUI: `/automation`; p duraklatır, r sürdürür, x süresi dolmuş attempt'i kurtarır. Bu işlemler güncel snapshot revizyonuna bağlıdır.

## 3. Kalıcı görev panosu

```powershell
atlas-agent task-board add fix-login --title "Login hatası" --prompt "Hatayı düzelt ve test et" --acceptance "Regresyon testi geçsin"
atlas-agent task-board list
atlas-agent task-board run fix-login --worker atlas --evidence proof/login-tests.txt
atlas-agent task-board accept fix-login
```

Görev: ready → running → review → done. Blocked/retry/recover yolları ayrıca vardır. Kabul kriteri ve mevcut prerequisite task ID'leri saklanır. Worker claim, attempt token ve süreli lease üretir; aynı task iki worker'a verilemez. Dış worker'lar `claim/renew/submit/block` CLI komutlarını aynı kontratla kullanabilir. Worker lane kendiliğinden başka CLI'ya veya ayrı worktree'ye geçmez.

`run` mevcut Atlas modelini ayrı oturumda çalıştırır ve en fazla bir saat sürer; explicit evidence yollarını üretmesini ister. Normal izin politikası ve maliyet/step sınırları uygulanır. Çıktı dosyaları gerçekten varsa hash'leri kaydedilip review'a alınır. Worker başarısızsa blocked olur; process çökmesinde lease sonrası recover gerekir. Accept sırasında kanıtların hash'leri yeniden kontrol edilir. Dosyanın varlığı test başarısını kanıtlamaz: raporu ve execution kayıtlarını reviewer değerlendirir. Ajan aracı accept yapamaz; CLI veya TUI'deki kullanıcı yapar.

TUI: `/board`; Enter ayrıntıları, a review kabulü, r retry, x expired worker recovery. Bu pano mevcut oturum todo grafiğinin yerine geçmez; workspace genelinde görev sahipliği ve teslim katmanıdır.

## 4. Araç pipeline'ı

```json
{
  "steps": [
    {"id":"read","tool":"view","items":["a.go","b.go"],"arguments":{"file_path":"$item"}},
    {"id":"check","tool":"test_run","arguments":{"packages":"./...","count":1},"if_success":"read"}
  ],
  "return":["check"]
}
```

`tool_pipeline`, JSON ile tanımlanmış sınırlı bir araç programıdır; keyfi Python/JavaScript çalıştırıcısı değildir. Her tool çağrısı mevcut authorized palette, schema, hook, permission, ownership, execution, journal ve timeout yolundan geçer. `$item` yalnız eşit string değerinde değiştirilir; shell interpolation yoktur. Her aracın gerçek parametre şemasını `tool_search` ile doğrulayın.

32 step, toplam 64 invocation ve 5 dakika sınırı vardır. Sonuç başına metin 8 KiB, metadata 4 KiB, toplam seçilmiş output 64 KiB ile sınırlıdır. Hata/deny/halt programı durdurur; mutation tekrar oynatılmaz. Pipeline, job/board/goal kontrolünü veya kendisini çağıramaz. Gerçek tool çıktıları journal kontratıyla izlenir; return seçimi intermediate sonuçların modele taşınmasını azaltır. Token kazancı garanti değildir.

## 5. Kaynaklı bellek

`source_memory add`: id, text, sources (1–16 proje dosyası), isteğe bağlı Unix saniyesi `valid_until`. Kaynak session, kayıt zamanı ve SHA-256 fingerprint saklanır. list/search: current/stale/expired/superseded/unchecked. Kayıtlar workspace'ler arasında paylaşılmaz. Read-only kaynak kontrolünde symlink ve root dışı yol reddedilir; dosya başına 512 KiB sınırı vardır.

```powershell
atlas-agent source-memory
atlas-agent source-memory "veritabanı"
```

TUI: `/source-memory`. Prompt hazırlanırken yalnız current olan en fazla dört kayıt ve 8 KiB claim metni eklenir; kaynak bilgisi taşınır. Hash eşleşmesi claim'in doğru olduğunu kanıtlamaz. Belirleyici kararlarda kaynak tekrar incelenmelidir. Her aramada source read bütçesi 8 MiB'dir; bütçe sonrasındaki kayıtlar unchecked olur. Dar query kullanılarak yeniden kontrol edilebilir.

Eski USER.md/proje belleği korunur; bu kaynaklı claim katmanı ayrı araçtır. Remove bir kaydı superseded yapar; eski kanıt ve provenance kaybolmaz.

## 6. Kalıcı hedef ilerlemesi

Mevcut `/goal` ve `goal` aracının tur bütçesi, used, completion claim, stopped/done ve run kimliği SQLite'a yazılır. Bir tur başlamadan önce sayısı rezerve edilir; süreç çökmesi bütçeyi ücretsiz sıfırlamaz. Yeni coordinator eski used/budget değerini yükler. Başka süreç veya yeni hedef tarafından değiştirilen run'ın eski kaydı yeni hedefi ezemez.

Tamamlanma kontrolü mevcut goal judge üzerinden çalışır. Stop/clear ve budget exhaustion otomatik yeni bütçe başlatmaz. Hedef kaydı tutulsa da devam etmek için ilgili oturumun çalıştırılması gerekir; bütün eski sohbetler kendiliğinden açılmaz. Aktif tur sürerken hedef değiştirmek reddedilir; önce tur durdurulmalıdır.

## 7. Belge çıkarımı

Mevcut `view` aracına entegredir:

- DOCX: paragraph konumu; XML metni, tab ve satır sonları.
- XLSX: sheet/cell konumu, shared/inline strings ve cached formula değerleri.
- PPTX: slide/paragraph konumu.
- Jupyter nbformat 4: cell/code/markdown ve metin output konumu; kod çalıştırılmaz.
- PDF: Poppler `pdftotext` ile page konumu; boş sayfalar OCR ihtiyacını bildirir.

Office ZIP expansion, üye sayısı, belge ve çıktı boyutu sınırlıdır. Dosya maksimum 50 MiB, çıkarılan metin 2 MiB; view pagination uygulanır. PDF converter otomatik kurulmaz. Görsel, çizim, rich notebook output ve PDF görsel tamlığı garanti edilmez; eski binary Office formatları (.doc/.xls) bu extractor kapsamına dahil değildir. Bozuk veya şifreli belgeler başarılıymış gibi gösterilmez.

## Kalıcılık ve kaynak sınırları

Yeni migration mevcut atlas.db'ye agent_state tablosunu ekler; eski session/message/memory verileri korunur. Her kayıt revizyon ile compare-and-swap güncellenir ve 256 KiB ile sınırlıdır. Job/board/source-memory/vault namespace'leri 256 kayda kadar desteklenir; removed/superseded kayıtlar da bu sınıra dahildir. Goal kayıtları bu workspace kataloğu sınırından bağımsızdır. Ücretli yeni model çağrıları existing session cost ve step sınırlarına, job sayısı/timeout sınırlarına tabidir.

