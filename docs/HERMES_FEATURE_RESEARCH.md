# Hermes Agent → Atlas özellik araştırması

Araştırma tarihi: **3 Ekim 2026**. İncelenen kaynak: NousResearch/hermes-agent, sabit revizyon **18fa38463490c07e7d1764520118097a5e5fdd42**.

## Kapsam ve kanıt sınırı

Hermes'in bu revizyondaki `website/docs/user-guide/features/` dizininin **56 Markdown sayfası** tarandı. Ayrıca mimari, ajan döngüsü, prompt oluşturma, araç runtime'ı, context compression, micro-compaction, bellek/context/browser/terminal sağlayıcı sözleşmeleri, hook'lar, oturum saklama ve trajectory formatı belgeleri incelendi. Profiller, oturumlar, TUI, import, checkpoint ve bot-mode belgeleri de karşılaştırmaya dahil edildi.

Bu bir **belgelenmiş özellik envanteri ve Atlas kaynak koduyla karşılaştırma** çalışmasıdır. Hermes'in bütün kaynak dosyaları satır satır denetlenmedi; bütün entegrasyonları kurulup çalıştırılmadı. Dokümanda açıklanan davranış ile canlı servisin gerçekten sunduğu davranış aynı kabul edilmedi. Özellikle model/fiyat listeleri, abonelik kapsamı ve dış servisler uygulama öncesinde ayrıca doğrulanmalıdır.

Aşağıdaki öneriler tasarımdır; tümünün Atlas'a eklendiği anlamına gelmez. Browserbase bulut oturum entegrasyonu kullanıcı isteğiyle kaldırıldı. Yerel tarayıcı ve mevcut CDP bağlantıları korunuyor.

## Atlas'ta zaten bulunan önemli parçalar

| Atlas'taki temel | Kaynak kanıtı | Hermes'ten alınabilecek fark |
|---|---|---|
| Kalıcı kullanıcı/proje belleği | [memory](D:/Atlas/internal/memory/memory.go) | Kaynak, geçerlilik ve harici sağlayıcı seçimi |
| FTS5 konuşma araması | [DB search](D:/Atlas/internal/db/search.go), [session_search](D:/Atlas/internal/agent/tools/session_search.go) | Arama sonuçlarından sınırlı, kaynaklı özet üretme |
| Hedef döngüsü ve bağımsız tamamlanma kontrolü | [goal](D:/Atlas/internal/agent/goal.go) | Kullanılan tur/bütçe ve duraklama durumunu yeniden başlatmaya dayanıklı tutma |
| Anahtar rotasyonu | [rotator](D:/Atlas/internal/credentials/rotator.go) | Cooldown, Retry-After, hesap sağlığı; mevcut rotasyon aktif stream sırasında anahtar değiştirmez |
| Model fallback | [fallback](D:/Atlas/internal/agent/model_fallback.go) | Yardımcı görev bazında fallback ve ayrı maliyet sınırı |
| Talebe göre araç şeması yükleme | [coordinator](D:/Atlas/internal/agent/coordinator.go), [tool_search](D:/Atlas/internal/agent/tools/tool_search.go) | Çok sorgu, ayrı describe, kontrollü batch çağrı; mevcut seçenek `defer_tool_schemas` |
| Beceriler ve oturum içi kullanım takibi | [tracker](D:/Atlas/internal/skills/tracker.go), [authoring](D:/Atlas/internal/skills/authoring.go) | Oturumlar arası bakım, pin, sürüm, arşiv ve geri yükleme |
| Uzmanlar, batch ve mimar/editör akışı | [agent modes](D:/Atlas/internal/agent/agent_modes.go), [batch](D:/Atlas/internal/engineering/agent_batches.go) | Kalıcı dispatcher, harici worker ve pano sahipliği |
| Checkpoint ve çalışma kanıtı | [checkpoint](D:/Atlas/internal/engineering/checkpoint.go), [knowledge](D:/Atlas/internal/engineering/knowledge.go) | Yeniden başlatma ve görev tesliminde aynı kanıtı kullanma |
| Kalıcı komut zamanlaması | [schedule](D:/Atlas/internal/cmd/schedule.go) | Ajan prompt'u çalıştıran cron ve aynı sohbet heartbeat'i; mevcut komut schedule ile eşdeğer değil |
| MCP, LSP, backend protokolü | [MCP](D:/Atlas/internal/agent/tools/mcp/), [LSP](D:/Atlas/internal/lsp/), [proto](D:/Atlas/internal/proto/) | Yeni core yerine protokol adaptörleri ve katalog yönetimi |
| Windows bilgisayar/tarayıcı otomasyonu | [computer](D:/Atlas/internal/computer/), [browser](D:/Atlas/internal/browser/) | Uzak bot ekranı, secret injection ve ek bulut sağlayıcıları |

## Önce alınması gereken özellikler

### 1. Beceri bakım sistemi

Hermes Curator, özellikle ajan tarafından oluşturulan becerilerin kullanımını ve eskimesini takip eder; arşiv geri alınabilir, pin'lenen beceriler korunur. LLM ile birleştirme ayrı, isteğe bağlı bir adımdır. [Kaynak][curator].

**Atlas tasarımı:** `internal/skills` altında kalıcı kullanım kayıtları; skill kimliği + kaynak revizyonu + son kullanım + düzeltme/başarı kanıtı. İlk aşama yalnız `inspect` ve `dry-run`; ardından pin, archive, restore. Referans dosyalarıyla birlikte tüm skill paketi taşınmalı. Bir görev ya da zamanlama tarafından kullanılan beceri korunmalı. Ajanın kendi başarı iddiası tek başına kalite puanı olmamalı.

**Kullanıcıya yararı:** Benzer beceriler kataloğu şişirmez; eskimiş talimat fark edilir; yanlış bakım geri alınır.

**Kabul:** Pin ve görev bağımlılığı korunur, paket içi yollar bozulmaz, dry-run hiçbir şey yazmaz, paralel bakım tek kilit üzerinden yürür. LLM bakımı ayrı bütçeye tabi olur.

### 2. Siteye bağlı kimlik bilgisi kasası

Hermes, şifreyi modele göstermeden sayfaya doldurmayı ve maskeli kullanıcı girişini ayrı bir kasa katmanıyla sunuyor. 1Password/Bitwarden kaynakları ve farklı doğrulama türleri belgelenmiş. [Kaynak][credential-vault].

**Atlas tasarımı:** OS keyring veya kullanıcı tarafından açılan şifreli kasa; item'ın izinli **tam origin** listesi; secret handle üzerinden doldurma. Parola, TOTP seed ve kısa süreli kodlar conversation, trace, screenshot metadata ve log'a yazılmamalı. Browser ve computer için ortak secret-redaction sözleşmesi. Yetkili TOTP üretimi mevcut altyapıyı kullanabilir; donanım anahtarı ve telefon onayı ilgili kullanıcı cihazında tamamlanır.

**Kabul:** Yanlış origin reddedilir, yönlendirmeden sonra origin tekrar kontrol edilir, model çıktısında secret bulunmaz, kilitli kasa headless görevde beklemeden anlaşılır hata verir. Kart/ödeme işlevleri bu ilk sürümün kapsamına alınmamalı.

### 3. Aynı sohbeti takip eden heartbeat ve ajan cron

Hermes heartbeat mevcut konuşmaya yalnız boşta bir tur ekler. Cron ayrı oturumda çalışan kalıcı iş içindir. Kaçırılan heartbeat'ler birleştirilir; kullanıcı mesajı önceliklidir. [Kaynaklar][heartbeat], [cron][cron].

**Atlas tasarımı:** Mevcut schedule worker'ın kilit/persistence temelini kullanmak; `command`, `agent_job` ve `session_watch` türlerini ayrı tutmak. Sahip sohbet, workspace, schedule version ve teslim hedefi kayıt altına alınmalı. Pause/resume/clear, maliyet ve tur sınırı; sadece anlamlı değişiklik bildirimi. İş yeniden başlamadan önce güncel yetki ve iptal durumu kontrol edilmeli.

**Kabul:** Meşgul ajan kesilmez, kullanıcı turu önce girer, restart sonrası aynı tick iki kez çalışmaz, durdurulan eski watch başka sohbete taşınmaz. Zamanlanmış ajan kendiliğinden sınırsız yeni zamanlama üretemez.

### 4. Kalıcı görev panosu ve worker sözleşmesi

Hermes Kanban, task lifecycle ile yürütücü sürecin yaşamını ayırıyor. Worker lane'in kimliği, başlatma biçimi ve teslim sözleşmesi var. [Kaynaklar][kanban], [worker lanes][kanban-worker-lanes].

**Atlas tasarımı:** Mevcut engineering görev/journal/batch altyapısının üstüne SQLite pano; ready/running/review/blocked/done durumları, dependency, lease ve attempt ID. Tek dispatcher. Çökme sonrası lease recovery; eski worker yeni attempt'i tamamlayamamalı. Atlas ve kullanıcı tarafından seçilmiş dış CLI'lar aynı çıktı manifest'ini üretmeli.

**TUI:** Pano görünümü, worker durumu, maliyet, son kanıt ve başarısız denemeyi yeniden yürütme. Reviewer yalnız “çalıştım” mesajını değil dosya değişikliği ve doğrulama kanıtını görür.

**Kabul:** İki worker aynı görevi sahiplenemez; bilinmeyen lane başka bir yürütücüye sessizce düşmez; kesilen görev tekrar çalıştırılabilir; review ve done arasında açık kabul koşulu vardır. Otomatik ayrı worktree zorunluluğu konmamalı; çalışma alanı tercihi kullanıcıya ait.

### 5. Kodla araç pipeline'ı

Hermes'in `execute_code` davranışı, alt süreçteki koddan RPC ile araç çağırıp yalnız seçilmiş son çıktıyı modele döndürür. Bu sıradan terminal komutundan farklıdır. [Kaynak][code-execution].

**Atlas tasarımı:** Platformdan bağımsız sınırlı RPC runner. Örneğin 30 dosyayı oku → filtrele → test et → tek JSON özeti çıkar. Her gerçek tool çağrısı normal permission, schema validation, hook, cancellation ve journal yolundan geçmeli. Script'in tool allow-list'i ana ajan yetkisinin alt kümesi olmalı. Ara sonuçlar yerel artifact'te saklanmalı; gerektiğinde yeniden okunabilmeli.

**Kabul:** İptal alt süreci durdurur, reddedilen edit pipeline içinden de reddedilir, çıktı ve çağrı sayısı sınırlıdır, Windows desteklenir. Token kazancı ölçülür; “ücretsiz” ya da “sıfır token” denmez.

### 6. Mevcut araç keşfini büyütmek

Hermes arama, şema açıklama ve çağırma işlerini ayırabiliyor; batch içindeki yerel çağrılar kendi denetimlerinden geçiyor. [Kaynak][tool-search].

**Atlas tasarımı:** Mevcut `tool_search` üzerinde çok sorgu, describe-by-name ve yetkili batch dispatch. Arama indeksini MCP listesi değişince yenilemek; isim çakışmalarını sunucu namespace'iyle çözmek. Kullanılmayan şemalar için token bütçesi ve keşfedilen araçların oturumda görünür durumu.

**Kabul:** Arama devre dışı aracı açamaz; sunucu kaldırıldığında stale tool çağrısı reddedilir; her batch elemanı kendi hook/izin sonucunu taşır. Bağımlı çağrılar sırayla, bağımsızlar mevcut concurrency politikasına göre çalışır.

### 7. Kaynaklı bellek ve bellek sağlayıcı arayüzü

Hermes yerel bellek yanında harici sağlayıcı adaptörleri sunuyor. [Kaynak][memory-providers].

**Atlas tasarımı:** Yerel bellek varsayılan kalır. Kullanıcı tercihi, proje gerçeği, eski çözüm ve doğrulanmış engineering bilgisi ayrı sınıflar olur. Kaynak session/file/revision, güncellik, delete/update ve scope alanları taşınır. İsteğe bağlı semantic backend yalnız seçilen scope'u alır. FTS5 oturum araması korunur.

**Kabul:** Workspace'ler arası veri karışmaz; silinen kayıt indeks ve uzak backend'den kaldırılır; stale bilgi gerçekmiş gibi prompt'a eklenmez. Sağlayıcı kesintisi yerel belleği çalışamaz hale getirmez.

### 8. Hedefin çalışma durumunu kalıcılaştırmak

Atlas hedef metnini saklıyor; `goalRun` içindeki kullanılan tur/bütçe ve runtime durumunun önemli kısmı bellekte. Hermes'in persistent-goal yaklaşımı burada geliştirme girdisi. [Kaynak][goals].

**Atlas tasarımı:** Goal state ve her attempt'in bütçe/kanıt kaydı transaction ile persist edilir. Yeniden açınca durum görünür; otomatik sürdürme ancak mevcut kullanıcı tercihi kapsamında olur. Tamamlanma yargıcı execution ve test kayıtlarına erişir; erişemediği kanıtı doğrulanmış saymaz.

**Kabul:** Restart bütçeyi sıfırlayıp fazladan maliyet yaratmaz; paused/completed yeniden başlamaz; aynı completion claim iki kez işlenmez.

### 9. Kontrollü micro-compaction

Hermes birikmiş geçmişi küçük adımlarla özetlemeyi seçenek olarak sunuyor; doküman bunun ek model çağrısı yaptığını ve cache prefix'ini bozabildiğini açıkça belirtiyor. [Kaynak][micro-compaction].

**Atlas tasarımı:** Mevcut compression yolu korunur. İsteğe bağlı incremental summary; kullanıcı kısıtları, task contract, açık hata ve artifact işaretçileri ayrı korunur. Raw transcript silinmez. Özet hangi tur aralığından üretildiğini taşır.

**Kabul:** Tool-call/result çiftleri geçerli kalır; kaybolan karar/constraint sayısı ve cache dahil toplam maliyet sabit görev setinde ölçülür. Uzun bir duraklamayı azaltmak tek başına başarı sayılmaz.

### 10. Belge çıkarımı ve teslim manifest'i

Hermes dosya okumada PDF/Office/notebook gibi formatları metne çeviriyor; taranmış PDF'de kapsam eksikliğini bildiriyor. Mesaj gateway'inde deliverable'lar doğal ek olarak teslim edilebiliyor. [Kaynaklar][document-extraction], [deliverable][deliverable-mode].

**Atlas tasarımı:** `view` için document extractor interface; dosya boyutu, decompression ve sayfa/cell bütçesi. PDF sayfası, DOCX bölüm ve XLSX sheet/cell konumu korunur. OCR isteğe bağlı. Generated artifact manifest'i path, MIME, size, source, checksum ve doğrulama kaydı taşır; preview/export bu manifest'i kullanır.

**Kabul:** Görüntü PDF'si boş metin diye başarılı sayılmaz; bozuk/arşiv bombası belge sınırlı kaynakla hata verir; hiçbir converter kendiliğinden kurulum yapmaz. “Dosya oluştu” ile “teslim edildi” ayrı durumdur.

### 11. Eklenti SDK'sı ve kaynak doğrulaması

Hermes plugin ve katalog yapısı araç, hook ve entegrasyonları paketliyor. [Kaynaklar][plugins], [catalog][plugin-catalog].

**Atlas tasarımı:** Manifest, kaynak revizyonu, sürüm, capabilities, config schema, dependency ve kaldırma/rollback kaydı. Üçüncü taraf kod için mevcut MCP veya ayrı süreç RPC tercih edilir. Python modüllerini Go çekirdeğe doğrudan taşımak uygun değil. Katalogdaki “incelenmiş” etiketi kullanılan sürüme bağlı olmalı.

**Kabul:** Eklenti yetkisini yükseltemez; kurulum ve güncelleme diff'i görünür; başarısız güncelleme geri döner; adı aynı iki tool sessizce birbirini ezmez.

### 12. Profiller, yardımcı model yönlendirmesi ve hesap sağlığı

Atlas usage profile ve model rolleri zaten var. Hermes'in daha kapsamlı profil izolasyonu ile yardımcı görev yönlendirmesi bunları geliştirebilir. [Profiller][profiles], [credential pools][credential-pools], [MoA][mixture-of-agents].

**Atlas tasarımı:** Usage profili ile veri izolasyon profili ayrı kavramlar olur. İkincisi credentials, bellek, tools ve schedules scope'unu tanımlar. Compression, vision, judging ve skill bakımına ayrı model rolü/bütçe. Anahtar havuzuna cooldown/Retry-After ve transient/auth error ayrımı. MoA isteğe bağlı preset; bütün danışman çağrıları gerçek toplam maliyete dahil edilir.

**Kabul:** Profil değişimi başka profilin kasasını/belleğini açmaz; 401 alan hesap sınırsız dönmez; aktif stream'in ortasında sessiz model/anahtar değişimi yapılmaz.

## İkinci aşama adayları

| Aday | Atlas'a somut katkısı | İlk sürüm sınırı |
|---|---|---|
| ACP adaptörü | Editörlerde Atlas oturumu, izin isteği, diff ve cancel | Tek core coordinator; ikinci ajan runtime'ı yaratma |
| OpenAI uyumlu API facade | Harici frontend'lerin mevcut backend'i kullanması | Kimlik doğrulama, workspace scope, SSE cancel ve tool sonucu sözleşmesi |
| Mesaj gateway'i | Telegram/Discord/Slack gibi yüzeylerden aynı görev platformu | Başlangıçta tek platform; hesap eşleme, izin, teslim retry ve explicit kullanıcı yetkisi |
| Web dashboard | TUI dışından görev/oturum/maliyet/kanıt görünümü | Önce read-only; TUI ile aynı backend ve tutarlı state |
| Uzak execution sağlayıcıları | SSH/container/cloud ortamlarında aynı araç kontratı | Provider capabilities; timeout, release, path mapping, session isolation |
| Bot screen | Kullanıcı masaüstünü bölmeden ayrı çalışma ekranı | Uzak oturum yaşam döngüsü, opt-in görüntü, input sahipliği |
| Web arama sağlayıcıları | Mevcut aramanın kesintiye dayanıklılığı ve uzun belge araştırması | Provider registry, bounded cache, URL kaynak bilgisi, artifact pointer |
| JSONL değerlendirme runner'ı | Prompt/rol/araç değişikliklerini ölçmek | Sabit fixture görevleri, checkpoint/resume, redaksiyon ve toplam maliyet |

Kaynaklar: [ACP][acp], [API][api-server], [dashboard][web-dashboard], [terminal providers][terminal-environment-plugin], [bot screen][bot-screen], [web search][web-search], [batch][batch-processing].

Ses, image generation ve X araması yararlı opsiyonel adaptörler olabilir. Wake-word, Spotify ve maskot Atlas'ın büyük projelerde daha güvenilir çalışmasına göre düşük öncelikli. Nous abonelik gateway'ini bir SDK fonksiyonu gibi “kopyalayarak” edinmek mümkün değil; hizmet erişimi ve ücretlendirme ayrı konudur.

## 56 sayfanın tamamının karşılaştırma envanteri

“Yeni” burada incelenen Atlas kaynaklarında eşdeğer birleşik özellik bulunmadığı anlamına gelir; MCP ile bağlanabilir bir servisin hiç kullanılamayacağını söylemez. “Kısmi” davranış eşdeğerliği iddiası değil, yeniden kullanılabilecek bir temel bulunduğunu ifade eder.

| Hermes başlığı | Atlas durumu | Karar / fark |
|---|---|---|
| [Genel özellik haritası][overview] | Envanter | Tek başına özellik değil; kapsam kontrolü için kullanıldı. |
| [Araçlar ve toolset’ler][tools] | Mevcut | İzinli araç kümeleri var; sağlayıcı yeteneklerini tek kayıt üzerinden açıklamak geliştirilebilir. |
| [Beceriler ve Skills Hub][skills] | Mevcut / geliştirme | Keşif, yazma ve talebe göre yükleme var; sürüm, kaynak ve güncelleme kanalı eklenebilir. |
| [Beceri bakım sistemi][curator] | Yeni katman | Kalıcı kullanım kaydı, pin, eskime, arşivleme ve geri yükleme. |
| [Kalıcı bellek ve oturum araması][memory] | Mevcut | USER/project belleği ve FTS5 araması var; kaynaklı ve derecelendirilmiş hatırlama geliştirilebilir. |
| [Harici bellek sağlayıcıları][memory-providers] | Yeni adaptörler | Yerel belleğin üstüne seçilebilir sağlayıcı sözleşmesi. |
| [Honcho kullanıcı modelleme][honcho] | Yeni, isteğe bağlı | Harici kişiselleştirme; proje gerçeklerinden ve kullanıcı tercihlerinden ayrı tutulmalı. |
| [Proje bağlam dosyaları][context-files] | Mevcut | AGENTS.md ve diğer proje yönergeleri destekleniyor. |
| [Dosya/dizin/diff/URL referansları][context-references] | Kısmi karşılık | Atlas referans ve ek altyapısını diff/URL kaynak kaydıyla genişletmek; bütün Hermes sözdizimi eşdeğerliği doğrulanmadı. |
| [SOUL.md ve persona][personality] | Kısmi karşılık | Atlas rol ve sistem prompt’ları var; sürümlü kullanıcı üslubu katmanı ayrı tutulabilir. |
| [Kalıcı hedefler][goals] | Mevcut / geliştirme | Atlas hedef döngüsü ve bağımsız tamamlanma yargıcı var; yeniden başlatma sonrası bütçe/progress kaydı geliştirilmeli. |
| [Aynı sohbeti takip eden heartbeat][heartbeat] | Yeni katman | Boşta çalıştırma, kullanıcı mesajına öncelik, kaçırılan tick’leri birleştirme. |
| [Tekrarlayan oturum döngüleri][loops] | Yeni zamanlama davranışı | Sabit veya değişime göre yavaşlayan takip; hedef döngüsünü çoğaltmadan zamanlayıcıya eklenmeli. |
| [Zamanlanmış ajan görevleri][cron] | Kısmi karşılık | Atlas kalıcı komut schedule altyapısı var; ajan prompt’u, skill ve sonuç teslimi ayrı genişleme. |
| [Alt ajan devretme][delegation] | Mevcut | Uzmanlar, görev sözleşmeleri ve paralel çalışma var; süreç sahipliği ve sonuç teslimi iyileştirilebilir. |
| [Kalıcı çok ajanlı görev panosu][kanban] | Kısmi karşılık | Atlas görev/batch/journal temeli var; ayrı panolar, worker lease ve kalıcı dispatcher yeni katman. |
| [Kanban kullanım senaryoları][kanban-tutorial] | Tasarım girdisi | Tek geliştirici, çok worker ve rol zinciri senaryoları kabul testine dönüştürülebilir. |
| [Harici worker kanalları][kanban-worker-lanes] | Yeni entegrasyon | Atlas/Codex/Claude/OpenCode süreçleri ortak görev kontratına bağlanabilir; kullanıcı seçimi gerekir. |
| [Çok gateway’li pano][kanban-multi-gateway] | İleri aşama | Tek dispatcher, profil sahipliğinde teslim ve kiracı ayrımı; ilk sürüm için gereksiz dağıtık karmaşıklık. |
| [Veri seti ve trajectory üretimi][batch-processing] | Kısmi karşılık | Atlas batch görevleri var; JSONL değerlendirme, resume ve redakte trajectory dışa aktarma farklı genişleme. |
| [Seçilebilir çok modelli MoA][mixture-of-agents] | Kısmi karşılık | Atlas çok ajan/rol araçları var; model seçiciden kullanılan danışman→sentezleyici preset’i ayrıca eklenebilir. |
| [Yaşam döngüsü hook’ları][hooks] | Mevcut / geliştirme | Shell ve ajan hook’ları var; gözlemci, karar veren ve işi başlatan hook türleri açık ayrılmalı. |
| [Birlikte gelen bakım eklentileri][built-in-plugins] | Yeni paketleme | Bakım işleri var; opt-in eklenti manifest’i ve yaşam döngüsü kaydı ortaklaştırılabilir. |
| [Araç/hook/entegrasyon eklentileri][plugins] | Yeni sözleşme | Atlas MCP ve skills uzantıları var; genel eklenti SDK’sı bunları manifest ile bağlamalı. |
| [İncelenmiş eklenti kataloğu][plugin-catalog] | Yeni | Sabit kaynak revizyonu, yetki listesi, doğrulama, güncelleme ve rollback. |
| [MCP sunucuları][mcp] | Mevcut | Atlas MCP desteği var; katalog, tool filter ve sağlık görünümü geliştirilebilir. |
| [Talebe göre araç keşfi][tool-search] | Mevcut / geliştirme | Atlas tool_search ve defer_tool_schemas var; çok sorgu, describe ve ayrı denetimli batch dispatch geliştirilebilir. |
| [Kodla çok adımlı araç çağırma][code-execution] | Yeni RPC katmanı | Shell çalıştırmadan farklı: her çağrı normal Atlas izin/hook yoluna giren sınırlı pipeline. |
| [Semantik tanı][lsp] | Mevcut | Atlas LSP ve kod grafı var; sadece Hermes’te varmış gibi yeniden yapılmamalı. |
| [Yerel ve bulut tarayıcı][browser] | Yerel/CDP mevcut | Atlas yerel tarayıcı ve CDP bağlantısı var; yönetilen bulut oturumu entegrasyonu kaldırıldı. |
| [Masaüstü otomasyonu][computer-use] | Mevcut / genişleme | Windows UIA/OCR ve etkileşim izleri var; uzak Linux/macOS masaüstü ayrı yetenek. |
| [Ayrı bot ekranı][bot-screen] | Yeni yüzey | Kullanıcının odağını almayan uzak masaüstü ve kontrollü görüntü/etkileşim aktarımı. |
| [Şifre ve siteye bağlı secret doldurma][credential-vault] | Yeni kasa katmanı | Atlas OAuth/TOTP altyapısı var; origin-bound kasa, maskeli giriş ve modelden gizli injection farklı özellik. |
| [API/OAuth hesap havuzları][credential-pools] | Mevcut / geliştirme | Atlas anahtar rotasyonu var; sağlık, cooldown ve hesap kotası farkındalığı eklenebilir. |
| [Sağlayıcı/model fallback][fallback-providers] | Mevcut | Atlas model fallback var; yardımcı görevlere ayrı hata/bütçe politikası genişletilebilir. |
| [OpenRouter yönlendirme tercihleri][provider-routing] | Uyarlama adayı | Maliyet/hız/sağlayıcı tercihleri; her sağlayıcıya aynı seçenek dayatılmamalı. |
| [Abonelikten uyumlu API uç noktası][subscription-proxy] | Yeni, sağlayıcıya bağlı | Yetkili OAuth token yenileme ve API köprüsü; hesap sahipliği ve vendor desteği ayrıca doğrulanmalı. |
| [Nous araç gateway’i][tool-gateway] | Harici servis | Tek faturada araç erişimi ticari altyapıdır; kaynak kopyalamak servis erişimi sağlamaz. |
| [Editörler için ACP][acp] | Yeni protokol adaptörü | Atlas backend/proto üstüne session, izin ve dosya değişimi sözleşmesi. |
| [OpenAI uyumlu ajan API’si][api-server] | Kısmi karşılık | Atlas backend/server var; OpenAI uyumlu facade, kimlik doğrulama ve oturum sınırları ayrı iş. |
| [Codex runtime kullanma][codex-app-server-runtime] | Yeni, isteğe bağlı | Ajan yürütücüsünü dış runtime’a devretme; model/araç sahipliği ve özellik farkları açık gösterilmeli. |
| [Web yönetim paneli][web-dashboard] | Yeni yüzey | Atlas TUI var; aynı backend üzerinden oturum, görev, bellek, araç ve kullanım paneli. |
| [Panel tema/eklenti slot’ları][extending-the-dashboard] | Panel sonrası | Önce sağlam web paneli; ardından sınırlı UI slot ve yetkili backend route SDK’sı. |
| [Dosyaları doğal ek olarak teslim][deliverable-mode] | Kısmi karşılık | Atlas export ve preview var; manifest, dosya türü, kaynak ve mesaj platformu eki genişletilebilir. |
| [PDF/Office/notebook çıkarımı][document-extraction] | Yeni birleşik araç davranışı | Yapısal çıkarım, kapsam uyarısı, sayfa/hücre kaynağı ve isteğe bağlı OCR. |
| [Arama ve sayfa çıkarımı][web-search] | Mevcut / geliştirme | Atlas DuckDuckGo araması var; sağlayıcı kayıt sistemi, TTL cache ve uzun sayfa artifact’i. |
| [X/Twitter araması][x-search] | Yeni vendor adaptörü | Resmî API/OAuth erişimiyle opsiyonel araştırma aracı; genel web aramasından ayrı. |
| [Görseller ve panodan yapıştırma][vision] | Kısmi karşılık | Atlas multimodal görsel/ek altyapısı var; platformlara göre clipboard deneyimi ayrıca kontrol edilmeli. |
| [Görsel üretimi][image-generation] | Yeni medya adaptörü | Vendor yetenek ve model listesi canlı doğrulanmalı; dokümandaki fiyat/model tablosu güncel katalog sayılmamalı. |
| [Metinden sese ve transkripsiyon][tts] | Yeni medya adaptörü | Önce dosya tabanlı STT/TTS; streaming ve playback ayrı yetenek. |
| [Gerçek zamanlı ses][voice-mode] | İleri aşama | Kesme, tool sırasında ses durumu, kaynak ve gecikme yönetimi gerekir. |
| [Sesle uyandırma][wake-word] | Düşük öncelik | Mikrofon ve arka plan kaynak kullanımı nedeniyle açık kullanıcı tercihi. |
| [Dil paketleri][language-packs] | Yeni ürün katmanı | Önce Türkçe/İngilizce TUI string kataloğu, fallback ve değişken doğrulaması. |
| [Tema ve skin][skins] | Mevcut / geliştirme | Atlas token temaları var; kullanıcı skin şeması ve uyumluluk kontrolü eklenebilir. |
| [Animasyonlu maskot][pets] | Düşük öncelik | Kodlama başarısına katkısı sınırlı; renderer ve erişilebilirlik maliyeti. |
| [Spotify entegrasyonu][spotify] | Düşük öncelik | Genel ajan platformunda opsiyonel MCP/eklenti; Atlas çekirdeğine gömülmemeli. |

## Uygulama sırası ve çıkış ölçütleri

1. **Güvenilir çekirdek:** kasa/origin injection, kalıcı goal state, skill kullanım kaydı ve tool discovery geliştirmesi. Çıkış: izin/scope/cancel testleri ve restart recovery.
2. **Uzun çalışma:** heartbeat/agent cron, kalıcı pano ve worker lease. Çıkış: iki dispatcher/worker yarış testi, çökme kurtarma, bütçe ve duplicate-dispatch kontrolü.
3. **Büyük işlerde verim:** programmatic pipeline, belge çıkarımı, kaynaklı bellek, seçilebilir micro-compaction ve değerlendirme runner'ı. Çıkış: aynı görevlerde kalite/maliyet karşılaştırması.
4. **Yeni yüzeyler:** ACP/API, dashboard, tek mesaj platformu, uzak execution ve bot screen. Çıkış: kimlik/scope, teslim, reconnect ve baştan sona contract testleri.
5. **Opsiyonel medya/kişiselleştirme:** STT/TTS, görsel üretimi, dil paketleri ve skin'ler. Çekirdek çalışma doğruluğunun önüne geçirilmemeli.

Önerilen değerlendirme seti: küçük bug fix, büyük refactor, 40 modüllük batch review, kesilip devam eden uzun görev, karmaşık browser flow ve dosya teslimi. Her senaryoda görev başarısı, kaçan kullanıcı kısıtı, toplam input/output/cached token, gerçek faturalanan ücret, p50/p95 gecikme, insan müdahalesi ve recovery sonucu kaydedilir. Önceden ölçülmeden yüzde performans/kazanç vaadi verilmez.

## Kaynak ve lisans yaklaşımı

Hermes'in incelenen revizyondaki [LICENSE][license] dosyası MIT lisanslı, telif Nous Research'e ait. Fikirleri Atlas'ın Go mimarisine bağımsız uyarlamak ile kaynak kodu taşımak farklı işlemlerdir. Kod alınırsa kaynak path/revizyon ve gerekli telif/lisans metni korunmalı; bir dosya portunun API contract ve testleri Atlas için yeniden doğrulanmalı. Bu araştırmada Python kaynak kodu Atlas çekirdeğine kopyalanmadı.

Kaynak bağlantıları sabit commit'e sabitlenmiştir. Böylece daha sonra Hermes main değişse de bu raporun hangi dokümana dayandığı görülebilir.

[overview]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/overview.md
[tools]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/tools.md
[skills]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/skills.md
[curator]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/curator.md
[memory]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/memory.md
[memory-providers]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/memory-providers.md
[honcho]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/honcho.md
[context-files]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/context-files.md
[context-references]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/context-references.md
[personality]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/personality.md
[goals]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/goals.md
[heartbeat]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/heartbeat.md
[loops]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/loops.md
[cron]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/cron.md
[delegation]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/delegation.md
[kanban]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/kanban.md
[kanban-tutorial]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/kanban-tutorial.md
[kanban-worker-lanes]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/kanban-worker-lanes.md
[kanban-multi-gateway]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/kanban-multi-gateway.md
[batch-processing]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/batch-processing.md
[mixture-of-agents]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/mixture-of-agents.md
[hooks]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/hooks.md
[built-in-plugins]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/built-in-plugins.md
[plugins]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/plugins.md
[plugin-catalog]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/plugin-catalog.md
[mcp]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/mcp.md
[tool-search]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/tool-search.md
[code-execution]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/code-execution.md
[lsp]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/lsp.md
[browser]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/browser.md
[computer-use]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/computer-use.md
[bot-screen]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/bot-screen.md
[credential-vault]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/credential-vault.md
[credential-pools]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/credential-pools.md
[fallback-providers]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/fallback-providers.md
[provider-routing]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/provider-routing.md
[subscription-proxy]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/subscription-proxy.md
[tool-gateway]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/tool-gateway.md
[acp]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/acp.md
[api-server]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/api-server.md
[codex-app-server-runtime]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/codex-app-server-runtime.md
[web-dashboard]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/web-dashboard.md
[extending-the-dashboard]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/extending-the-dashboard.md
[deliverable-mode]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/deliverable-mode.md
[document-extraction]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/document-extraction.md
[web-search]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/web-search.md
[x-search]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/x-search.md
[vision]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/vision.md
[image-generation]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/image-generation.md
[tts]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/tts.md
[voice-mode]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/voice-mode.md
[wake-word]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/wake-word.md
[language-packs]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/language-packs.md
[skins]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/skins.md
[pets]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/pets.md
[spotify]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/features/spotify.md
[micro-compaction]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/developer-guide/micro-compaction.md
[terminal-environment-plugin]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/developer-guide/terminal-environment-plugin.md
[profiles]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/website/docs/user-guide/profiles.md
[license]: https://github.com/NousResearch/hermes-agent/blob/18fa38463490c07e7d1764520118097a5e5fdd42/LICENSE

