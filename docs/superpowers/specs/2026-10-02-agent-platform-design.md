# Atlas agent platformu: on iki özellik için teknik tasarım

Tarih: 2026-10-02.
Durum: Sohbetteki tasarım ve bu yazılı şartname kullanıcı tarafından onaylandı.
Uygulama planları ayrıca incelenecek; ürün uygulaması henüz başlamadı.
Bu belge uygulama veya testlerin tamamlandığına ilişkin bir rapor değildir.

## Amaç ve kapsam

Atlas'ın büyük projeleri daha güvenilir tamamlamasını sağlamak: doğru kod
bağlamını seçmek, uzmanların çıktısını bütünleştirmek, hatayı kanıtla çözmek,
kesintiden güvenilir devam etmek ve gerçek kullanıcı davranışını doğrulamak.
Kullanıcı aşağıdaki on iki özelliğin tamamının kaliteli ve profesyonel biçimde
entegre edilmesini istedi ve ortak coordinator tabanlı mimariyi onayladı.

1. Kod ilişkilerini anlayan proje belleği.
2. Göreve göre kaynakları seçen bağlam paketleri.
3. Kontrollü teşhis ve onarım döngüsü.
4. Kesinti sonrası doğrulanmış devam noktaları.
5. Uzmanlar arası sürümlü sözleşmeler.
6. İnceleme bulgularından onarım görevleri üretme.
7. Dosya/ağ erişimini gerçekten sınırlayan komut çalıştırma izolasyonu.
8. Önizleme ve tutarlılık kontrolüyle anlamsal kod düzenleme.
9. Web ve gerçek terminal kullanıcı senaryoları.
10. Agent kontrol paneli.
11. Tekrarlanabilir ortam denetimi ve hazırlama.
12. Sürümlü, çalıştırılabilir proje iş akışları.

Bu çalışma model kataloğu, sağlayıcı kimlik doğrulaması, benchmark veya model
sıralaması değiştirmez. Yeni bir agent motoru ya da ikinci bir görev veritabanı
oluşturmaz. Mevcut görevler, roller, izinler, bütçeler ve kalite kapıları korunur.

## İncelenen başlangıç durumu

İnceleme anında HEAD `038af96`; çalışma ağacında önceki, henüz commit edilmemiş
agent/delivery geliştirmeleri vardır. Uygulama bu dosyaları koruyacak ve onları
başlangıç noktası kabul edecek; yalnızca HEAD üzerinden kurulan worktree onların
içeriğini otomatik taşımaz. İzolasyon için kullanılacak başlangıç kaynağı ayrıca
belirlenmeden önceki geliştirmeler kaybedilmiş bir checkout üzerinde çalışılmaz.

| Mevcut alan | Bu tasarımda genişletilecek nokta |
| --- | --- |
| `internal/engineering/project.go` | Sınırlı dosya/import haritası; genel çağrı grafiği değildir |
| `internal/agent/delivery_context.go` | Ana ajan için brief, profil ve yakın tarihli bilgi |
| `internal/engineering/runtime.go` | Kilitli, atomik, 1 MiB sınırlı yürütme günlüğü |
| `internal/agent/runtime_guard.go` | Araç işlemleri, bütçe ve görev sahipliği |
| `internal/agent/workflow_tool.go` | Hazır görev dalgası, dispatch ve kalite kontrolü |
| `internal/agent/role_quality.go` | Bağımsız test/review uzmanları ve gerçek doğrulama |
| `internal/session/quality_gate.go` | Ortak görev tamamlama kontrolü |
| `internal/session/rewind/` | Mesaj noktasından oturum fork etme ve dosya geri alma |
| `internal/sandbox/` | Windows Job Object; dosya/ağ güvenlik sınırı değildir |
| `internal/agent/tools/lsp_rename.go` | Sembol yeniden adlandırma zaten vardır |
| `internal/lsp/util/edit.go` | LSP workspace edit uygulama |
| `internal/agent/tools/ui_verify.go` | Web etkileşimi, iddialar ve görüntü kanıtı |
| `internal/commands/commands.go` | Markdown özel komutları ve skill komutları |
| `internal/ui/model/`, `internal/proto/`, `internal/workspace/` | Tek UI modeli, istemci/sunucu ve olay aktarımı |

## Mimari kararı

Mevcut coordinator yürütmenin tek sahibi olmaya devam eder. Yeni modüller küçük
arayüzlerle kaynak, kanıt veya eylem planı üretir. Araç ve model çağrıları mevcut
invoke/izin/bütçe yolundan geçer. İç içe döngüler bağımsız sınırsız ajanlar açmaz.

Alternatifler: ayrı agent motoru daha fazla durum senkronizasyonu gerektirir;
yalnızca prompt genişletmek ise devam, izolasyon ve tamamlama kurallarını runtime
düzeyinde uygulayamaz. Seçilen yaklaşım mevcut runtime'ı genişletmektir.

Kapsam dört bağımsız doğrulanabilir alt projeye ayrılır. Her biri kendi uygulama
planını alacak; sonraki alt proje gerekli arayüzler doğrulandıktan sonra başlayacak.

| Alt proje | Özellikler | Temel çıktılar |
| --- | --- | --- |
| A: Kaynak ve bağlam | 1, 2 | `CodeGraph`, `ContextPacket` |
| B: Yürütme ve toparlanma | 3, 4, 7, 11 | `RepairCase`, `Checkpoint`, `ExecutionPolicy`, `EnvironmentPlan` |
| C: Uzman koordinasyonu | 5, 6, 12 | `ContractRevision`, `Finding`, `RecipeRun` |
| D: Düzenleme ve deneyim | 8, 9, 10 | `EditPlan`, `ScenarioRun`, `WorkflowSnapshot` |

## Ortak sözleşmeler ve sınırlar

- Go modül tabanı `go.mod` içindeki Go 1.26.6'dır; üretim kodu bu tabanla uyumlu
  olmalı. Yerel doğrulama mevcut Go kurulumu ile yapılabilir.
- Derleme `CGO_ENABLED=0`, `GOEXPERIMENT=greenteagc` ile korunur.
- Config, `config.Service` üzerinden ve mevcut atlasrc/JSON birleştirmesiyle
  yönetilir. Şema ve istemci/sunucu tipleri birlikte güncellenir.
- Yeni kayıtların JSON alanları snake_case olur. Kayıtlar proje kökü, görev
  kimliği, spec fingerprint, kaynak fingerprint ve sürümle bağlanır.
- Yeniden çağrı ve eşzamanlı işlemlerde kimlikler idempotency key taşır. Aynı
  anahtar farklı içerikle tekrar kullanılırsa çakışma döner.
- Büyük kod grafikleri ve kanıtlar mevcut 1 MiB state içine gömülmez. Yeni
  artifact deposu kilitli, atomik, hash ile adreslenmiş dosyalar tutar; state
  yalnızca kimlik/hash/sürüm referansı taşır. Önce artifact yazılır, sonra referans
  commit edilir. Eksik veya bozuk artifact başarı olarak yorumlanmaz.
- Artifact yazma sınırı dosya başına 32 MiB; grafik en çok 20.000 düğüm ve
  50.000 kenar; referanslar koleksiyon başına en çok 128 kayıt. Sınır aşımında
  açık hata veya sorgu için belirtilen kısmi sonuç döner; eski kayıt bozulmaz.
- Aktif checkpoint, bulgu ve çalıştırma referanslarını otomatik silme yapılmaz.
  Temizlik yalnızca referanssız dosyaları, kilit altında ve açık CLI isteğiyle siler.
- İptal, izin reddi, eksik araç, timeout ve bozuk durum farklı sonuçlardır.
  Yalnızca gözlemlenmiş tamamlanma ve exit code başarılı sayılır.
- Okunan kod, web sayfası, handoff, workflow açıklaması ve log destekleyici veri
  olarak kalır; araç izinlerini veya sistem talimatı önceliğini değiştiremez.
- API anahtarları, ortam sırları ve ham credential içerikleri artifact, log,
  bağlam paketi veya kontrol paneline yazılmaz.
- Sahte invoker testleri gerçek OS izolasyonu veya gerçek terminal kanıtı değildir.

## A: Kaynak ve bağlam şartnamesi

### A1. Kod grafiği ve etki analizi

Yeni `internal/codegraph/` grafiği üretir; `internal/engineering/` saklama
referanslarını yönetir. Düğüm: kimlik, dil, dosya, sembol, tür, kaynak aralığı ve
dosya hash'i. Kenar: kaynak/hedef, `imports`, `calls`, `references` veya
`implements`, analiz kaynağı ve çözümleme düzeyi.

Go için AST ile bildirimler ve importlar çıkarılır; çağrı hedefleri yalnızca
gerçek sembol çözümlemesi veya LSP yanıtıyla eşleştirilir. Çözümlenemeyen çağrılar
isim benzerliğine göre kesin kenar haline getirilmez. Diğer dillerde mevcut LSP
sembol/referans/call hierarchy desteği sorgu kapsamında kullanılır. Dinamik çağrı
ve runtime veri akışı kapsam boşluğu olarak belirtilir; evrensel veri akışı
analizi varmış gibi sonuç verilmez.

`project_map` aracına `symbols`, `impact`, `refresh_graph` eylemleri eklenir.
Sorgu en çok 50 sonuç ve devam cursor'u verir; etki gezintisi derinlik 1-5,
en çok 200 düğümle sınırlıdır. Yenileme credential, symlink, binary ve üretilmiş
dizinler için mevcut dışlama politikasını korur. Kısmi tarama dosyanın silindiğini
kanıtlamaz. Değişen dosyanın eski kenarları yeni kaynak için kullanılamaz.

### A2. Göreve özel bağlam paketleri

Yeni `internal/agent/task_context.go` ana görev ve dispatch görevleri için
`ContextPacket` hazırlar. Paket görev/spec kimliği, ilgili kaynak parçaları,
kapsam talimatları, güncel kararlar, sözleşme revizyonları, eksik bağlam ve
kesilme bilgisini taşır. İlişkili testler grafik ile bulunabilir; ilişki
kanıtlanmamışsa aday olarak işaretlenir.

Seçim sırası: yürürlükteki talimatlar, kabul kriterleri, sahip olunan kaynaklar,
bağlı sözleşmeler, ilgili testler, ilişkili kod, güncel kararlar. Mevcut sistem
promptu ve görev kriterleri korunur; yeni retrieval kısmı en çok 24 KiB ve 16
kaynak parçasıdır. Bu sınır modelin toplam context budget kontrolünün yerine
geçmez. Paket hazırlama ücretli model çağrısı veya embedding servisi gerektirmez.

İlgili alt dizin talimatları mevcut talimat yükleyicisiyle okunur ve mevcut
öncelikleri korunur. Kaynak yeniden okunup hash doğrulanır. Eski paket yeniden
kullanılırsa yenilenir veya açıkça eksik olarak döner. Komşu proje ve farklı
worktree paketleri kök kimliği uyuşmadan birleştirilmez.

### A kabul testleri

- Aynı adlı iki fonksiyon yanlış kesin çağrı ilişkisi oluşturmaz.
- Sembol değişince eski sorgu sonucu güncel olarak sunulmaz.
- Eksik import/LSP kısmi sonucu ve sebebini gösterir.
- Alt dizin talimatları, multibyte metin ve 24 KiB sınırı birlikte korunur.
- Uzman yalnızca atandığı kök ve göreve ait paketi alır; paket izin vermez.

## B: Yürütme ve toparlanma şartnamesi

### B1. Teşhis ve onarım döngüsü

Yeni `internal/agent/repair_workflow.go`, `workflow repair` ile gözlemlenmiş
başarısız işlemden `RepairCase` açar. Kayıt başarısız operation ID, görev/spec,
hipotezler, ayırt edici kontroller, denemeler ve kanıt referansları taşır.
Durumlar `diagnosing`, `repairing`, `verifying`, `resolved`, `blocked`, `cancelled`.

Başlangıçta en çok 3 onarım denemesi; her denemede en çok 2 teşhis kontrolü ve
1 onarım uzmanı çağrısı. Kontroller mevcut verify/invoke yolundan çalışır.
Uzmanın açıklaması nedensellik kanıtı olarak otomatik kabul edilmez. Aynı
başarısız çağrıyı tekrar kesiciyi aşmak için farklı ID ile tekrar çalıştırmak
yasaktır. Farklı hipotez/kanıt veya gözlemlenmiş değişiklik olmadan yeni deneme
açılmaz. Bütçe deneme başlamadan ve her araç/model çağrısından önce denetlenir.

Onarım sonucu gerçek doğrulama ve gerekiyorsa bağımsız görev incelemesinden
sonra çözülmüş olur. İzin reddi ve altyapı eksikliği kod onarımına çevrilmez.
İptal geçmişi korur ve tamamlanma üretmez. Doğrulanmış ders mevcut lesson
yoluyla ayrıca kaydedilebilir; otomatik evrensel proje kuralı oluşturulmaz.

### B2. Devam noktaları ve uzlaştırma

Yeni `internal/engineering/checkpoint.go` stage geçişinde ve açık kullanıcı
isteğinde checkpoint kaydeder. İçerik: session/message kimliği, görev spec
hash'leri, plan fingerprint, mevcut stage, kaynak fingerprint, managed workspace
kimlikleri ve çözümlenmemiş operation ID'leri. Büyük dosya içerikleri kopyalanmaz.

`workflow checkpoint`, `workflow resume_plan`, `workflow resume` eylemleri ve
eşdeğer CLI komutları eklenir. `resume_plan` dosyaları değiştirmeden devam,
yeniden doğrulama ve inceleme gerektiren işlemleri ayırır. `resume` aynı plan
revizyonu ve kaynak için kabul edilen uzlaştırmayı uygular; çalıştırma yeniden
mevcut coordinator tarafından yönetilir.

Eski PID tek başına süreç kimliği sayılmaz; process başlangıç bilgisi ya da
container kimliği eşleşmiyorsa durum belirsizdir. Bilinmeyen yan etkili komut,
deploy, yayınlama veya migration otomatik replay edilmez. Checkpoint kaynak
değişikliğinde güvenilir geçmiştir, güncel passing kanıt değildir. Mevcut rewind
dosya geri alma özelliği ayrı ve açık bir işlem olarak kalır.

### B3. Gerçek komut izolasyonu

Yeni `internal/execution/` tüketiciye küçük bir runner arayüzü sunar. Request
argv, kök, görev, timeout ve `ExecutionPolicy` içerir; result gerçek exit code,
iptal nedeni, çıktı hash'leri ve backend kimliği taşır.

İlk gerçek güvenlik backend'i yapılandırılmış OCI runtime üzerinden container
çalıştırmadır. Host Windows/Linux/macOS olabilir; container image işletim sistemi
ve toolchain uyumu preflight'ta doğrulanır. Linux image içinde çalışan test,
Windows-native test olarak etiketlenmez. Native Windows güvenlik sandbox'ı bu
backend olarak iddia edilmez.

Politika seçenekleri: legacy host, container-required; proje mount'u ro/rw;
network none veya kullanıcı tarafından açıkça seçilmiş unrestricted; timeout,
CPU, memory ve process count. Domain allow-list, proxy ile uygulanmadan kabul
edilmez. Container-required için default ağ none, CPU 2, bellek 2 GiB,
process limit 128, timeout 10 dakika; kullanıcı ayarları mevcut bütçe sınırını
genişletemez. Image digest zorunlu; runtime veya image otomatik kurulmaz/pull edilmez.

Runtime soketi, host home, credentials ve farklı proje dizinleri container'a
mount edilmez. Environment açık allow-list ile oluşturulur. Read-only uzman
projeyi ro alır; geçici çıktı alanı ayrı rw mount'tur. İzin verilen outbound ağ
seçeneğinde bile host credential aktarımı otomatik olmaz. Gerekli backend veya
özellik eksikse komut başlamadan hata döner; host'a sessiz fallback yapılmaz.

Shell dış süreçleri, test/lint/verify ve senaryo alt süreçleri seçilen runner'a
bağlanır. Shell'in gömülü dosya builtin'leri container-required içinde host'a
yazmayı sürdürmemeli; shell bütünü container içinde çalıştırılır. Araç hook'ları
çalıştırma politikası kapsamında yürütülür. LSP/MCP süreçleri bu komut izolasyonu
ile otomatik izole edilmiş sayılmaz; kapsam ve backend panelde gösterilir.

İptalde yalnızca bu çalıştırmanın kaydedilmiş container kimliği sonlandırılır.
Başlama ile kimlik kaydı arasında kesinti halinde benzersiz run label üzerinden
uzlaştırma yapılır; ilgisiz container/process öldürülmez. Windows Job Object
mevcut host davranışı için korunur ve kaynak sınırlaması olarak etiketlenir.

### B4. Ortam denetimi ve hazırlama

Yeni `internal/environment/` manifest/lockfile kaynaklı `EnvironmentPlan`
üretir. `workflow environment` inspect/plan/apply/verify eylemlerini taşır.
Inspect yalnızca bounded dosya okur; tool version komutları normal runner ve
izin yolundan, ayrı probe olarak çalışır. Okuma aşamasında kurulum yapılmaz.

Go, Node paket yöneticileri, Python ve Rust için manifest araç/sürüm beklentileri
çıkarılır. Birden çok lockfile çakışma olarak gösterilir; rastgele paket yöneticisi
seçilmez. Plan açık argv listeleri, çalışma dizinleri, kaynak hash'leri ve ağ
gereksinimi taşır. Apply izin ve kaynak hash kontrolünden sonra mevcut budget
ile yürür; global kurulum yapmaz. İptal sonrası yeniden denetim gerekir; yarım
kurulum tamamlanmış sayılmaz. İmaj/tool sürümü, lock hash ve gözlemlenmiş sürümler
raporlanır; dış paket deposunun bit düzeyinde tekrar üretilebilirliği vaat edilmez.

### B kabul testleri

- Aynı başarısızlık kanıt değişmeden dördüncü onarım denemesine gidemez.
- Çökme/restart, kaynak değişimi ve yeniden kullanılan PID güvenli uzlaştırılır.
- Container-required backend eksikken host komutu çalıştırılmaz.
- Gerçek container testi dış mount erişimi, ro yazma reddi, ağ none ve çocuk
  süreç iptalini doğrular; mevcut runtime yoksa unavailable olarak raporlanır.
- Versiyon/lockfile uyuşmazlığı kod hatasından ayrılır; inspect kurulum yapmaz.
- Environment apply sırasında değiştirilmiş lockfile eski planı reddettirir.

## C: Uzman koordinasyonu şartnamesi

### C1. Sürümlü sözleşmeler

Yeni `internal/engineering/contracts.go` `ContractRevision` saklar: ID,
revizyon, owner task, consumer task IDs, API/veri/hata/invariant açıklaması,
kaynak referansları ve makine check tanımları. Sözleşme bir kaynak dosyasına
eşlik eden görevler arası kayıt olur; ikinci bir uygulama şeması oluşturmaz.

`workflow contract` register/query/revise/check eylemlerini taşır. Register ve
revise kaynak hash'lerini runtime'dan alır. Dispatch görev bağlamına kabul edilen
revizyonu ekler. Sözleşme değişince consumer görevler yeniden doğrulama gerektirir;
tarihsel completion silinmez ancak güncel stage ilerletmek için yeterli olmaz.
Concurrent revise compare-and-swap ile korunur. Sözleşme check'leri aynı runner,
izin ve gerçek journal eşleşmesini kullanır.

### C2. Bulgu ve yeniden inceleme

Yeni `internal/engineering/findings.go` `Finding` içerir: ID, task ID, reviewer,
önem, dosya/aralık, sorun, beklenen davranış, kaynak fingerprint, kanıt ve
verification tanımı. Durumlar open/assigned/fixed/verified/waived/stale.

Handoff'a opsiyonel `findings` eklenir; eski handoff'lar uyumludur. Runtime
kimlikleri atar, tekrar edilen aynı bulgu/spec için yeni onarım görevi üretmez.
`workflow remediate` bulgular için açık sahiplikli todos üretir. Aynı dosyanın
onarım görevleri bağımlılıkla sıralanır; aktif sahiplik çakışması dispatch olmaz.

`fixed` uygulayıcı beyanıdır. `verified` için farklı review execution kimliği,
güncel kaynak incelemesi ve yeni matching machine checks gerekir. Uzman
yargısı ile makine sonucu ayrı gösterilir. Waive yalnızca açık kullanıcı
isteğiyle gerekçe taşır; verified olarak etiketlenmez. Geçerli blocking bulgu
varken görev/stage passing olamaz; değiştirilmiş kaynakta eski bulgu yeniden
incelenir, kendiliğinden kapanmaz.

### C3. Çalıştırılabilir tarifler

Yeni `internal/workflows/` version 1 JSON tariflerini strict parse eder.
Proje kaynağı `.atlas/workflows/*.json`; kullanıcı kaynağı yapılandırılmış
workflow paths. Aynı isim çakışması açık hata olur. Her tarif ID/version,
typed params, requirement definitions, steps, dependencies, role, owned paths,
criteria ve check tanımları taşır. En çok 64 step; cycle, bilinmeyen role/param,
duplicate ID ve eksik requirement mapping reddedilir.

`atlas workflow recipes`, `validate`, `run` CLI yüzeyi ve mevcut komut paletinde
`/workflow:<id>` sunulur. Normal Markdown komutları korunur. Run mevcut todos
ve delivery plan oluşturur; mevcut stage/dispatch/review/advance üzerinden
ilerler. Aktif plan üzerine sessizce yazmaz. Yeni session ya da kullanıcı
tarafından seçilmiş uyumlu boş plan gerekir.

`RecipeRun` tarif hash'i ve param hash'iyle bağlanır; resume sırasında tarif
değişmişse açık yeni plan ister. Parametreler komut metnine shell interpolation
ile aktarılmaz; argv öğeleri olarak geçer. Prompt metnine aktarılan parametre
talimat önceliği kazanmaz. Tarif araç izni, model yetkisi veya kullanıcı adına
yayınlama yetkisi vermez. İlk built-in tarifler feature-delivery, migration-review
ve release-check; release-check sadece doğrular, publish çalıştırmaz.

### C kabul testleri

- Sözleşme revizyonu iki consumer'ın güncel sertifikasını geçersiz kılar.
- Aynı bulguyu iki kez işlemek tek onarım görevi üretir.
- Uygulayıcının fixed beyanı ve eski passing log bulguyu verified yapamaz.
- Döngülü tarif, shell metakarakterli argüman ve farklı içerikli aynı run key
  kontrollü sonuç verir; izin veya görev kapısı atlanamaz.
- Eski handoff ve Markdown komutları aynı davranışla kullanılabilir.

## D: Düzenleme ve kullanıcı deneyimi şartnamesi

### D1. Anlamsal düzenleme önizlemesi

Yeni `internal/agent/tools/lsp_edit_plan.go` ve `internal/lsp/util/edit_plan.go`
rename/replace için prepare/inspect/apply akışı sunar. `EditPlan` kesin hedef
dosyalar, önceki hash'ler, beklenen içerik hash'leri, LSP encoding, değişiklik
aralıkları ve izin kapsamı taşır. Mevcut araç isimleri korunur; yeni preview
seçeneği eklenir. Apply aynı plan kimliğine bağlı ve kaynak değişimine duyarlıdır.

Bütün hedefler önce doğrulanır: proje containment, symlink, ownership, readonly
rol, encoding ve çakışan edit aralıkları. Rename create/delete işlemleri de
planın açık parçası olur. Modelin sağladığı affected files listesi yetki belirlemez.
Permission dialog gerçek dosya listesi/diff taşır. Kullanıcı dosyası değiştiğinde
stale plan reddedilir; yeni diff olmadan yeniden izin kapsamı genişletilmez.

Birden çok dosyada gerçek OS-atomik transaction vaat edilmez. Geçici içerikler ve
önceki snapshot'lar hazırlanır, commit günlüğü kaydedilir. Yazma hatası/çökmede
partial durumu açıkça tutulur. Rollback yalnızca dosya halen bu planın yazdığı
hash'i taşıyorsa yapılır; sonraki kullanıcı değişikliği üzerine yazılmaz. LSP
tanılama ve ilgili verify sonuçları edit uygulamasından ayrı kanıt kaydıdır.

### D2. Web ve terminal senaryoları

Yeni `internal/scenarios/` version 1 senaryoları validate/run/report eder.
Senaryo ID, hedef, argv/URL, timeout, viewport, adımlar ve observable assertions
taşır. En çok 64 adım, 64 assertion, toplam 10 dakika. Run kimliği source
fingerprint, gerçek process/browser identity ve artifact hash'leriyle bağlanır.

Web, mevcut browser/ui_verify'yi kullanır: navigation, click/type/key, visible
text, focus ve console checks. Yerel test uygulamasında oluşturma/düzenleme ve
restart sonrası kalıcılık senaryosu gerekir. Scenario başlangıç/son kaynak
değişikliği sonucu sertifikalanamaz.

Terminal için gerçek PTY backend gerekir: Windows ConPTY ve Unix PTY. Adımlar
key/text, resize, bounded output assertion, exit ve cancel içerir. Capture
gerçek terminal bytes, boyut, zaman ve process identity taşır; ekran yorumlama
ile ham transcript ayrı artifact olur. Pipe üzerinde metin üretmek PTY kanıtı
sayılmaz. Gerekli platform backend yoksa unavailable olur. Renderer unit
testleri eklenir ancak gerçek terminal kontrolünün yerine geçmez.

Artifact'ta assertion pass ve görsel değerlendirme ayrı kalır. Klavye davranışı
tek screenshot'tan çıkarılmaz. Mevcut tasarım/critique stage kapıları gerçek
senaryo kanıtını kullanır ve eski kaynak artifact'ını güncel olarak kabul etmez.

### D3. Agent kontrol paneli

Yeni `internal/ui/model/workflow_panel.go` mevcut tek UI modeline bağlı bir
paneldir; nested Bubble Tea modeli değildir. Read model `WorkflowSnapshot`
görevler/dependencies, role execution, budget usage, bulgular, checks, stage,
checkpoint ve isolation capability/status içerir. Kaynak engineering state
ve session servisidir; panel ikinci state otoritesi olmaz.

İlk yükleme async `tea.Cmd`, sonraki güncellemeler revision'lı olayla yapılır.
Yavaş UI için ara durumlar birleştirilebilir, son durum kaybolmaz. Reconnect
tam snapshot alır. Server/proto/client-workspace aktarımı da eklenir; yalnızca
local UI'da çalışan özellik kabul edilmez. IO `Update` veya draw içinde yapılmaz.

Kullanıcı pending görevi yeniden atayabilir. Aktif görev için önce cancellation
isteği, gözlemlenmiş durma ve uzlaştırma gerekir; UI task status'u doğrudan
completed yapamaz. Stop request süreçlerin durduğunu kanıtlamaz. Görev kapsamı
değişince spec fingerprint ve ilgili kalite kapıları yenilenir.

Panel klavyeyle açılır/kapanır, tab/ok/enter/escape ile kullanılabilir. 80x24 ve
120x40 görünüm zorunlu; 40x12'de detaylar gizlenir ancak kapatma/durdurma
erişilebilir kalır. Token temaları, ANSI-aware genişlik yardımcıları ve mevcut
dialog ölçü kuralları kullanılır. Sır ve ham credential görüntülenmez.

### D kabul testleri

- UTF-16/UTF-8 LSP offset, CRLF, çakışan aralık ve proje dışı hedef testleri.
- Apply öncesi kullanıcı değişikliği stale hata verir; partial rollback sonraki
  kullanıcı değişikliğinin üzerine yazmaz.
- Gerçek PTY'de input/focus, resize, scroll, cancel ve exit kanıtı doğrulanır.
- Web kullanıcı senaryosu restart sonrası gerçek veri kalıcılığını doğrular.
- Panel küçük terminalde taşmaz, reconnect son revision'ı gösterir ve aktif
  görevin yeniden atanması cancellation/uzlaştırma kapısını atlayamaz.

## Entegrasyon, uyumluluk ve teslim koşulları

Her alt proje kendi spec bölümünden ayrı uygulama planı üretir. Planlar exact
dosya, arayüz ve failing regression testlerini belirler; bu belge kod gövdeleri
veya uygulama adımları yerine davranış sözleşmesini sabitler.

Yeni araç açıklamaları runtime desteğiyle birlikte eklenir; hazır olmayan araç
prompt'ta kullanılabilir diye gösterilmez. Promptlar gerçek capability, izin,
kapsam ve unavailable durumunu anlatır. Alt projeler önce kendi anlamlı
regression kontrollerini, sonra bütünleşik teslim senaryosunu geçer.

Uçtan uca kabul: iki bağımlı uzman görevi, ortak sözleşme, bir gözlemlenmiş hata,
bir inceleme bulgusu ve kesinti içeren küçük fixture proje. Atlas kaynak bağlamı
hazırlar, izole çalıştırmayı seçildiği şekilde uygular, onarımı sınırlar, bulguyu
bağımsız doğrular, restart sonrası uzlaştırır ve stage'i gerçek yeni checks ile
ilerletir. UI sonucu panelde, CLI/server sonucuyla aynı revizyonda görünür.

Doğrulama mock provider ile ücretli model çağrısı yapmadan yürütülür. Gerçek
container ve PTY kontrolleri ayrıca raporlanır; unavailable sonucu passing ile
karıştırılmaz. Bunlar ilgili platformda başarıyla gözlemlenmeden özellik o
platformda doğrulanmış ilan edilmez. Tüm ilgili Go dosyaları gofumpt ile
formatlanır. Son teslimde build, full test suite, CLI smoke ve platform kanıtı
ayrı ayrı belirtilir; test sayısı veya gerçek model başarısı önceden vaat edilmez.

Uygulama bitiminde on iki özelliğin her biri için çalışan giriş noktası,
doğrulama sonucu ve varsa platform sınırı dokümante edilir. Bir alt projenin
bitmesi bütün bu çalışmanın tamamlandığı anlamına gelmez.

## Şartname öz incelemesi

- On iki özellik A1-A2, B1-B4, C1-C3 ve D1-D3'e eksiksiz eşlendi.
- Mevcut LSP, özel komutlar, rewind ve Job Object yeniden yazılacak yeni
  özellikler olarak gösterilmedi; genişletme sınırları belirtildi.
- Persisted state ve artifact sınırları, çökme, concurrent mutation ve kimlik
  kontrolleri tanımlandı; eski kayıtlar ve kullanıcı değişiklikleri korunuyor.
- Windows host ile Linux container kanıtı ayrıldı; native Windows izolasyonu
  veya her dilde eksiksiz çağrı/veri akışı analizi iddiası bulunmuyor.
- Kullanıcı benchmark çalışmasını daha önce kapsam dışında bıraktı; bu tasarım
  benchmark, otomatik ücretli model testi veya model sıralaması eklemiyor.
- Bu dosya tasarımdır. Ürün değişikliği ve passing test iddiası içermez.
