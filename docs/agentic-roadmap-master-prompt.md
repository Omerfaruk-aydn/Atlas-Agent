# ATLAS-AGENT — Agentic Sistem Yol Haritası: Master Uygulama Prompt'u

> Bu belge, ATLAS-AGENT reposunda (`D:\Atlas`, modül
> `github.com/Omerfaruk-aydn/Atlas-Agent`) çalışacak uygulayıcı ajana verilecek
> tek ve eksiksiz görev tanımıdır. Baştan sona oku, sonra Faz 0'dan başla.

---

## 0. Rolün ve görevin

Sen kıdemli bir Go ve agentic-sistem mühendisisin. ATLAS-AGENT, Go ile yazılmış,
birden fazla LLM sağlayıcısını destekleyen terminal tabanlı bir kodlama ajanıdır
(Bubble Tea v2 TUI, SQLite/sqlc, MCP, LSP, hooks, skills). Görevin, aşağıda
listelenen **40 iş kalemini** (§15) üretim kalitesinde, hatasız, güvenli, test edilmiş
ve belgelenmiş şekilde hayata geçirmek.

Hedef tek cümleyle: **Atlas'ı, modelin kapasitesinin tamamını kullanan, işini
kanıtla doğrulayan, uzun ve otonom işlerde tutarlı kalan, ölçülebilir biçimde
diğer kodlama CLI'larından daha iyi sonuç veren bir sisteme dönüştürmek.**

Başarı ölçütü "kod yazıldı" değil, şudur: her özellik çalışıyor, testlerle
kanıtlanmış, mevcut davranışı bozmamış, yapılandırılabilir, belgelenmiş ve
ölçülebilir.

---

## 1. Değişmez kurallar (ihlal edilemez)

1. **Önce oku.** Kök `AGENTS.md` ve TUI işinden önce `internal/ui/AGENTS.md`
   dosyalarını oku ve harfiyen uygula (gofumpt, import grupları, log mesajları
   büyük harfle başlar, yorumlar nokta ile biter ve 78 sütunda sarılır, testify
   `require`, `t.Parallel()`, `t.TempDir()`, octal izinler `0o644`, JSON
   etiketleri snake_case, `context.Context` ilk parametre).
2. **Commit, push, PR yok — sormadan asla.** Her faz sonunda değişiklikleri
   mantıklı dilimler halinde öner (semantic commit mesajlarıyla), ama commit'i
   yalnızca kullanıcı açıkça onaylarsa yap.
3. **Commit edilmemiş mevcut iş var.** Çalışma ağacında kullanıcının ~80
   dosyalık, commit edilmemiş değişikliği bulunuyor. Bunları **asla geri alma,
   silme, ezme, `git checkout`/`git reset`/`git stash` ile kaybetme**. Başlamadan
   `git status` ve `git diff --stat` çıktısını kaydet; kendi değişikliklerini
   bu mevcut işle karıştırmamaya özen göster ve faz raporlarında hangi dosyanın
   zaten değişmiş olduğunu belirt.
4. **Araç silme yok.** Mevcut hiçbir aracı (`internal/agent/tools/`) kaldırma.
   Birleştirme gerekirse eski adı alias olarak koru.
5. **Alan-özel değil, genel.** QEMU, 3D, kernel gibi belirli bir alana özgü
   araç, protokol veya prompt ekleme. Her özellik her proje türünde işe yaramalı.
6. **`<user_authorization>` bölümüne dokunma.**
   `internal/agent/templates/agent_contract.md.tpl` içindeki bu bölüm
   kullanıcının bilinçli tasarım kararıdır. Değiştirme, kısaltma, yumuşatma
   önerme.
7. **Gerçek model ile bench/eval çalıştırma yok — onaysız.** Bench altyapısını
   kur ve `atlas eval bench validate` ile doğrula; gerçek sağlayıcıya karşı
   ücretli/kotalı çalıştırmalar için önce kullanıcıdan onay al. Antigravity
   sağlayıcısını test için kullanma.
8. **Varsayılan davranışı bozma.** Riskli veya maliyetli her yeni davranış bir
   config seçeneğinin arkasında olmalı. Varsayılan değeri her kalem için ayrıca
   gerekçelendir; kalite artıran ve ucuz olanlar varsayılan açık olabilir,
   ek model çağrısı yapan veya paralel çalışan her şey varsayılan kapalıdır.
9. **Sessiz başarısızlık yok.** Bir kapı veya yardımcı model hata verirse bunu
   logla ve metriğe yaz. "Fail-open" bilinçli bir seçimse belgelenmeli ve
   sayılmalı.
10. **Kod, yorum, belge ve commit mesajları İngilizce** (repo dili). Kullanıcıya
    verilen raporlar Türkçe.
11. **Uydurma yok.** Bir dosya, fonksiyon veya davranıştan bahsetmeden önce
    kodda doğrula. Bu belgedeki dosya referansları başlangıç ipucudur, kesin
    gerçek değildir — kullanmadan önce kontrol et.

### Ortam notları (Windows)
- Platform Windows 10, kabuk PowerShell / Git Bash. Testleri **`go test -p 1`**
  ile çalıştır (paralel link aşamasında `link.exe` takılıyor).
- `AI_AGENT` ortam değişkeni ayarlıyken `internal/cmd` altındaki bir test
  bozuluyor; bu değişkeni test komutlarında temizle ve bu hatayı senin
  değişikliğinin sonucu sanma.
- Derleme `CGO_ENABLED=0`, `GOEXPERIMENT=greenteagc`.
- Format: `gofumpt -w .` (yoksa `goimports`, o da yoksa `gofmt`). Lint:
  `task lint:fix`. Config şeması değişirse `schema.json`'u `internal/cmd/schema.go`
  üzerinden yeniden üret.

---

## 2. Çalışma yöntemi (her kalem için zorunlu döngü)

Her iş kalemini şu sırayla yürüt. Adım atlama.

1. **Denetim.** Kalemle ilgili mevcut kodu bul ve oku. Atlas'ta pek çok şeyin
   kısmi bir hali zaten var (goal, advisor, escalator, worktree alternatifleri,
   kuyruğa katlanan mesajlar, review, schedule, session export, error memory,
   failure corpus). **Sıfırdan yazmadan önce var olanı genişlet.** Denetim
   sonucunu iki-üç cümleyle not et: ne var, ne eksik, nereye bağlanacak.
2. **Tasarım notu.** `docs/` altında kısa bir tasarım bölümü yaz (veya ilgili
   mevcut belgeyi güncelle): amaç, veri akışı, config, varsayılanlar, hata
   durumları, maliyet etkisi, geri dönüş yolu.
3. **Kırmızı test.** Davranışı tanımlayan, şu an başarısız olan testleri yaz.
   Mümkün olduğunda model çağrısı içermeyen deterministik birim testleri tercih
   et; model gereken yerde mock sağlayıcıları kullan (`config.UseMockProviders`).
4. **Küçük dilimlerle uygula.** Her dilimden sonra etkilenen paketlerde
   `go build`, `go vet`, ilgili `go test -p 1` çalıştır.
5. **Yeşil ve regresyon.** Kırmızı testler geçmeli; `internal/agent`,
   `internal/config`, `internal/cmd` ve dokunduğun her paketin tüm testleri
   geçmeli. Golden dosya değiştiyse farkı incele, gerekçesiz `-update` yapma.
6. **Kalite kapıları.** `gofumpt`, `task lint:fix` (log-capital kuralı dahil),
   `go vet`. Diff'te kalan TODO, debug çıktısı, yorum satırına alınmış kod,
   zayıflatılmış test olmamalı.
7. **Belgele.** Kullanıcıya dönük davranış değiştiyse `docs/` ve gerekirse
   `README.md`/`HOOKS.md` güncelle. Config seçenekleri `jsonschema` açıklamasıyla
   gelsin; `atlasrc` builtins tarafında da karşılığı olsun.
8. **Ölç.** Her yeni kapı/özellik `internal/harnessmetrics` üzerinden en az şu
   sayaçları üretmeli: tetiklenme, başarı, başarısızlık/fail-open, ek token,
   ek süre.

### Kalite çıtası
- Fonksiyonlar tek bir iş yapar. `sessionAgent.Run()` (≈1270 satır) ve
  `coordinator.go` (≈3000 satır) zaten aşırı büyük: **bunlara yeni mantık
  gömme.** Yeni davranışı ayrı dosyalarda, küçük ve test edilebilir birimler
  halinde yaz; `Run()`'a yalnızca ince çağrı noktaları ekle.
- Arayüzler tüketen pakette tanımlanır, küçük tutulur.
- Eşzamanlılık: her goroutine iptal edilebilir (`ctx`), her paylaşılan durum
  korunur, `go test -race` uygun paketlerde temiz geçer (CGO kapalıysa race
  çalışmaz; o durumda kilit disiplinini kod incelemesiyle kanıtla).
- Hiçbir sır (API anahtarı, token) loga, trace'e, paylaşılan oturuma veya
  prompt'a düşmez; mevcut `secret_redaction` mekanizmasını kullan.
- Windows, Linux ve macOS'ta çalışır; yol işlemleri `filepath` ile.

---

## 3. Faz planı ve bağımlılıklar

Fazları sırayla yürüt. Bir faz bitmeden sonrakine geçme; her faz sonunda
**Faz Raporu** (bkz. §11) üret ve kullanıcıdan devam onayı al.

| Faz | İçerik | Neden bu sırada |
|---|---|---|
| 0 | Denetim ve temel çizgi | Neyin var olduğunu bilmeden tasarım yapılmaz |
| 1 | Ölçüm ve gözlemlenebilirlik | Sonraki her özelliğin etkisi ölçülebilsin |
| 2 | Tamamlama hattı ve doğrulama | Kalitenin omurgası; diğer kapılar buraya takılır |
| 3 | Bağlam yönetimi | Uzun işlerin ve otonominin ön koşulu |
| 4 | Model yönlendirme | Doğru işe doğru model ve düşünme seviyesi |
| 5 | İş akışı ve izinler (UX) | Kullanıcının sistemi yönetme biçimi |
| 6 | Paralel doğruluk | Faz 1–4 altyapısını kullanır |
| 7 | Otonomi ve dağıtım | Önceki tüm katmanların üzerine kurulur |
| 8 | Öz-iyileştirme | Ölçüm + corpus + bench olgunlaşınca |

---

## 4. Faz 0 — Denetim ve temel çizgi

- `git status`, `git diff --stat` kaydet. `go build ./...` ve
  `go test -p 1 ./internal/agent/... ./internal/config/... ./internal/cmd/...`
  çalıştır; **önceden var olan** kırık testleri listele (senin sorumluluğun
  değil ama raporla).
- Aşağıdaki her kalem için mevcut kod haritası çıkar:
  `docs/agentic-roadmap-audit.md` — kalem başına: ilgili dosyalar, mevcut
  davranış, boşluk, entegrasyon noktası, risk.
- Kapıların mevcut envanterini çıkar ve tur sonu karar akışını diyagramla:
  `evaluator.go`, `contract_gate.go`, `completion_decision.go`,
  `outcome_guard.go`, `goal.go` (goal judge), `advisor.go`, escalator
  (`coordinator.go: buildEscalator`), `plan_review.go`, `runtime_guard.go`,
  `strategy_monitor.go`, `diff_guard.go`, `loop_detection.go`.
- Mevcut system prompt'un mod başına token boyutunu ölç (coder, task, browser
  ve desktop protokolleri yüklüyken/yüksüzken).

**Çıktı:** denetim belgesi + temel çizgi test durumu + kapı akış diyagramı.
Bu fazda üretim kodu değişmez.

---

## 5. Faz 1 — Ölçüm ve gözlemlenebilirlik

### 1.1 Tur izi (trace) ve `atlas eval trace`
- Her tur için yapılandırılmış bir iz: adımlar, araç çağrıları (süre, sonuç
  boyutu, hata), model çağrıları (model, token, önbellek isabeti, maliyet), her
  kapının kararı ve gerekçesi, devam/sıkıştırma/handoff olayları.
- Kalıcı depolama SQLite'ta (yeni migration + sqlc sorguları
  `internal/db/sql/`) veya `.atlas/` altında JSONL — denetimdeki mevcut
  `harnessmetrics` yapısına uyan seçeneği gerekçelendir.
- `atlas eval trace <session-id> [--turn N] [--json]`: insan-okunur zaman
  çizelgesi ve JSON çıktısı. "Model neden 40 adım attı?" sorusu tek komutla
  cevaplanabilmeli.
- **Kabul:** sahte sağlayıcıyla çalışan bir oturumun izi eksiksiz kaydediliyor;
  sırlar maskeleniyor; iz yazımı başarısız olsa tur bozulmuyor.

### 1.2 Bench genişletme ve kapı ablasyonu
- `bench/tasks` şu an 9 Go görevi. 40–60 göreve çıkacak bir yapı kur ve en az
  şunları ekle: çok dosyalı değişiklikler, birbirini etkileyen değişiklikler,
  belirsiz spesifikasyon, kapsam dışına çıkma tuzağı, TypeScript ve Python
  görevleri, gerçek-dünya tarzı hata düzeltmeleri. Her görev: başlangıç
  durumu kırmızı, referans çözüm yeşil (`atlas eval bench validate` ile kanıtla),
  gizli testler benzersiz adlı.
- Ablasyon: manifest'te kapıları tek tek aç/kapat (evaluator, contract, advisor,
  goal judge, escalator, yeni eklenecek lint-fix/mutasyon kapıları) ve şunları
  raporla: pass@1, pass@k, token, maliyet, süre, fail-open sayısı.
- `atlas eval bench report` karşılaştırmalı tablo ve önceki çalıştırmaya göre
  regresyon uyarısı üretsin.
- **Kabul:** tüm görevler `validate`'ten geçiyor; ablasyon manifest'i hazır;
  gerçek model çalıştırması kullanıcı onayına bırakılmış.

### 1.3 Kapı metrikleri
- Her kapı için: tetiklendi / geri gönderdi / geçti / fail-open / ek token /
  ek süre. `atlas stats` (veya mevcut `stats_harness.go`) bunları göstersin.

### 1.4 Sıkıştırma kalite değerlendirmesi
- Özetleme sonrası kritik bilgilerin (dosyalar, kararlar, açık işler,
  kullanıcı kısıtları, doğrulama kanıtları) korunup korunmadığını ölçen bir
  eval: sentetik uzun oturum → özet → "özet bu soruları cevaplayabiliyor mu?"
  kontrol listesi. `atlas eval` altına eklenir.

### 1.5 System prompt token golden testi
- Mod başına system prompt token sayısını golden dosyada sabitle; %5'ten büyük
  artış testi kırsın (bilinçli artışta golden güncellenir ve gerekçe yazılır).

---

## 6. Faz 2 — Tamamlama hattı ve doğrulama

### 2.1 Tek tamamlama hattı (completion pipeline)
- Ayrı ayrı karar veren kapıları tek, sırası belli bir hatta topla:
  1. Deterministik kontroller: derleme, etkilenen testler, lint/tip,
     diff guard (zayıflatılmış test, kalıntı), sır taraması (yalnızca değişen
     dosyalar: mevcut `scan_secrets`/`security_scan`).
  2. Kontrat: `.atlas/work.json` gereksinimleri ve doğrulama komutları.
  3. Test kalitesi (bkz. 2.5, 2.6).
  4. Bağımsız inceleme (evaluator).
- Hattın sonunda **tek karar** (`pass | continue | unresolved | unverified`) ve
  **tek, birleşik devam mesajı** çıkar. Model aynı turda birbiriyle çelişen
  birden fazla talimat almamalı.
- Ucuz aşama başarısızsa pahalı aşama (model incelemesi) çalışmaz.
- Mevcut kapıların davranışı korunur; hat onları sıralayan ve birleştiren
  katmandır. Mevcut testler geçmeye devam etmeli.
- **Dosya değişikliği tespiti** araç adına değil, tur başı/sonu git diff +
  `runtime_guard.go`'daki kaynak parmak izi karşılaştırmasına dayanmalı; bash
  ile (`sed -i`, script, codegen) yapılan değişiklikler de incelemeye girmeli.

### 2.2 Yapılandırılmış evaluator kararı
- `parseEvaluatorVerdict` serbest metin ayrıştırmasını, tool call veya JSON
  şema ile alınan yapılandırılmış karara çevir:
  `{verdict: pass|fail, findings: [{severity, file, line, summary, evidence}]}`.
- Format hatası → bir kez düzeltme isteği; yine başarısızsa fail-open ama
  **sayılır ve loglanır**.

### 2.3 Kırmızı → yeşil iş akışı
- Hata düzeltme ve davranış ekleme işlerinde ajan önce başarısız bir kontrol
  (test veya reprodüksiyon komutu) kaydeder; kontratta "red evidence" olarak
  tutulur. Tamamlama hattı, aynı kontrolün düzenlemeden sonra yeşil olduğunu
  ve ilgili test paketinin bozulmadığını doğrular.
- Akış: kapsam + kabul kriterleri → kırmızı → küçük dilimler (her dilimde
  `impact_analysis` ile seçilen etkilenen testler) → yeşil + regresyon →
  bağımsız inceleme → kanıtlı rapor. Prompt'a (`coder.md.tpl` engineering
  workflow) kısa ve net biçimde yansıt; tören haline getirme — küçük
  düzeltmelerde hafif kalmalı.

### 2.4 Lint-fix döngüsü
- Her dosya düzenleyen araç çağrısından sonra değişen dosyalarda LSP
  teşhisleri ve (yapılandırılmışsa) hızlı linter çalışır. Yeni hata varsa ajan
  bunu aynı adımda bilgi olarak değil, **düzeltilmesi gereken bulgu** olarak
  alır. Önceden var olan hatalar ayrı işaretlenir (yalnızca yeni hatalar
  zorunlu).
- Sonsuz döngüyü engelle: aynı hata N kez tekrarlarsa duraklama tespitine
  (2.7) devret.

### 2.5 Test kalitesi kontrolü (mutasyon)
- Ajanın eklediği/değiştirdiği testler için, değişen kaynak satırlarında
  sınırlı sayıda deterministik mutasyon (operatör çevirme, sınır değeri,
  koşul tersleme, dönüş değeri) uygula, ilgili testleri çalıştır, mutasyonu
  geri al. Yakalanmayan mutasyonlar "zayıf test" bulgusu olur.
- Dil desteği eklenti noktasıyla; ilk sürüm Go, ardından TS/Python için
  genişletilebilir arayüz. Zaman bütçesi ve mutant sayısı yapılandırılabilir.
- Kaynak dosyası her koşulda (panik, iptal, zaman aşımı dahil) orijinal
  haline döner — bunu test et. Çalışma ağacında asla mutant kalmamalı;
  mümkünse geçici kopya/worktree üzerinde çalış.
- Varsayılan kapalı, opt-in.

### 2.6 Özellik tabanlı test ve fuzz önerisi
- Değişen kod parser, kodlayıcı/çözücü, serileştirme, sayısal hesap gibi
  sinyaller taşıyorsa (genel sezgiler, alan-özel değil) ajana property-based
  test veya Go `testing.F` fuzz testi yazmasını öneren bir yönlendirme ekle.
  Zorunlu değil, öneridir; gerekçesiyle tek satır.

### 2.7 Akıllı döngü ve duraklama tespiti
- `loop_detection.go` yalnızca birebir aynı çağrı+sonucu yakalıyor. Ekle:
  - Aynı **hata imzası** (normalize edilmiş hata mesajı/test adı) farklı
    çağrılarda tekrar ediyor.
  - N adım boyunca **ilerleme yok**: yeni geçen test yok, yeni dosya/bulgu
    yok, kontrat maddesi kapanmadı.
- Tepki kademeli: önce strateji değiştir mesajı, sonra oracle danışması
  (4.3), sonra kullanıcıya net bir engel raporu. Mevcut `strategy_monitor.go`
  ve `contract_alternatives.go` ile birleştir, kopyalama.

### 2.8 Kanıta bağlı öğrenilmiş kurallar ("phantom guardrails" önlemi)
- `internal/errmemory`, `harnessmetrics/strategy.go` ve
  `engineering/learned_methods.go` tarafından üretilen her kural, onu doğuran
  gerçek olaya (oturum, tur, hata imzası) bağlanır.
- Kanıtı olmayan, belirli bir süre/oturum sayısı boyunca tetiklenmeyen veya
  tetiklendiğinde fayda göstermeyen kural otomatik olarak zayıflar ve silinir.
  Kural kullanım sayaçları metriklere yazılır.

---

## 7. Faz 3 — Bağlam yönetimi

### 3.1 Prompt cache'i koru
- Goal ve browser kontratı gibi değişken içerik şu an her istekte system
  prompt'a ekleniyor (`agent.go` içinde goal/contract enjeksiyonu). Sabit
  öneki (system prompt + araç tanımları) değişmez tut; değişken bloğu son
  mesaja veya önbellek kırılma noktasından sonraya taşı.
- **Kabul:** goal değişince system prompt baytları değişmiyor (test); önbellek
  isabet oranı trace'te görünüyor.

### 3.2 "Context anxiety" önlemi
- Bağlam doldukça modelin işi aceleyle kapatmasını engelle: `budget_line.go`
  yüksek doluluk seviyelerinde "acele etme; gerekirse sistem handoff/sıkıştırma
  yapacak, işi yarım kapatma" mesajı versin.
- Metrik: bağlam doluluğuna göre kapı başarısızlık oranı ve erken bitirme
  sinyalleri (doğrulama yapılmadan "tamam" denmesi). Trace'e yaz.

### 3.3 Uyarlamalı bağlam budama
- Eski ve artık ilgisiz büyük araç çıktılarını (view, grep, bash, fetch)
  sıkıştırmadan önce kısalt veya tek satırlık referansa indir
  (`[view internal/x.go:1-400 — 18 KB, yeniden okumak için view]`).
- Kurallar: son K adım asla budanmaz; düzenlenen dosyaların son hali
  korunur; hata çıktıları kısaltılır ama hata satırları kalır; budama
  geçmişi DB'yi değil, modele giden mesajı etkiler (kayıt bozulmaz).
- Tool-call/tool-result eşleşmesi korunmalı (`filterOrphanedToolResults`
  mantığını bozma).

### 3.4 Boşta sıkıştırma
- Oturum, sağlayıcının prompt cache süresine yakın bir eşikten uzun süre
  boşta kaldıysa ve bağlam belirli bir boyutu aşıyorsa, kullanıcı dönmeden önce
  arka planda sıkıştırılır. İptal edilebilir; kullanıcı mesaj yazarsa yarım
  sıkıştırma güvenle bırakılır. Varsayılan kapalı veya muhafazakâr eşik.

### 3.5 `@dosya` için yol referansı
- Eklenen dosyalar için seçenek: içerik yerine yol + boyut + kısa özet
  (ilk satırlar/semboller) gönder; model gerekirse `view` ile okur. Eşik
  üzerindeki dosyalarda otomatik, küçük dosyalarda içerik. Config ile
  ayarlanabilir. Görseller ve PDF'ler mevcut davranışı korur.

### 3.6 Handoff / temiz başlangıç
- Bağlam eşiğe ulaştığında (veya kullanıcı/ajan isteğiyle) özetle devam etmek
  yerine: ajan devam için gereken bağlamı yapılandırılmış biçimde çıkarır
  (hedef, kontrat durumu, değişen dosyalar, kararlar, açık işler, doğrulama
  kanıtları, kullanıcı kısıtları), yeni bir oturum/thread açılır ve bu paketle
  başlar. Eski oturuma bağlantı korunur (session tree).
- Mevcut `feature_handoff.go`, `request_checklist`, summary şablonu ve
  `.atlas/progress.md` ile birleştir.
- TUI'de görünür olay + `/handoff` komutu. Özetlemeye göre bilgi kaybını 1.4
  eval'i ile karşılaştırarak kanıtla.

---

## 8. Faz 4 — Model yönlendirme

### 4.1 Aşamaya göre düşünme seviyesi ve model
- Aşamalar: keşif, planlama, uygulama (mekanik düzenleme), doğrulama, inceleme.
  Her aşama `model_roles` üzerinden ayrı model ve reasoning effort alabilsin.
- Aşama, ajanın yaptığı işten (araç kullanımı, kontrat durumu) türetilir;
  açık bir aşama değişikliği trace'e yazılır. Model değişimi önbelleği
  bozacağından, değişimler aşama sınırlarında ve seyrek olmalı — maliyet
  etkisini ölç.

### 4.2 Architect / editor ayrımı
- Opsiyonel mod: güçlü model çözümü ve düzenleme planını üretir (dosya,
  değişiklik niyeti, değişmezler); hızlı/ucuz model düzenlemeleri uygular;
  derleme/test başarısızsa architect'e geri döner.
- Plan yapılandırılmış veri olmalı; editor plandan sapamaz, sapma gerekiyorsa
  architect'e sorar. `delegate_tool.go`/subagent altyapısını yeniden kullan.

### 4.3 Oracle aracı
- Advisor ve escalator mantığını, ajanın kendisinin çağırabileceği tek bir
  `oracle` aracında birleştir: "şu sorunda takıldım, bağlam şu, seçenekler şu".
  En güçlü akıl yürütme modeli, salt-okunur araçlarla, sınırlı adımda cevap
  verir. Otomatik advisor/escalator davranışı config ile korunur.
- Kullanım sınırı ve maliyet bütçesi; 2.7 duraklama tespiti tarafından da
  tetiklenebilir.

### 4.4 Farklı model ailesinden inceleyici
- Evaluator için varsayılan olarak, yazar modelden **farklı sağlayıcı/aile**
  seçen bir çözümleyici (kullanılabilir ve yetkili sağlayıcılar arasından).
  Yoksa mevcut davranışa düş ve bunu logla.

---

## 9. Faz 5 — İş akışı ve izinler

### 5.1 Spec modu
- Büyük işlerde ajan önce `.atlas/spec/requirements.md` (kullanıcı hikâyeleri
  + kabul kriterleri), sonra `design.md` (mimari, veri akışı, riskler), sonra
  `tasks.md` (sıralı, işaretlenebilir görevler, her biri doğrulama komutuyla)
  üretir. Her aşamada kullanıcı onayı (TUI'de onay/düzenle/reddet).
- Bu dosyalar `.atlas/work.json` kontratının **insan-okunur yüzü**dür: iki yönlü
  senkron (kullanıcı `tasks.md`'yi düzenlerse kontrat güncellenir; ajan görevi
  kapatınca dosyada işaretlenir). Tek doğruluk kaynağını açıkça belirle ve
  çakışma çözümünü test et.
- Küçük işlerde devreye girmez (otomatik eşik + `/spec` ile manuel).

### 5.2 Steer modu
- Atlas'ta kuyruktaki mesajlar zaten aktif tura katlanabiliyor
  (`drainQueueForStep`). Bunu açık bir moda dönüştür: çalışma sırasında
  yazılan mesaj turu kesmez, bir sonraki adıma "kullanıcı yönlendirmesi"
  olarak eklenir; kullanıcı TUI'de "yönlendir" ile "kuyruğa al/yeni tur"
  arasında seçim yapabilir. Yönlendirme kontrata ve request checklist'e
  eklenir, kaybolmaz (sıkıştırma sonrası dahil).

### 5.3 Çalışma sırasında izin yükseltme
- Ajanın tur ortasında ek yetki isteyebileceği bir araç: hangi kapsam (dizin
  yazma, ağ alanı, komut kategorisi), neden, ne süreyle (bu çağrı / bu tur /
  bu oturum). Kullanıcı onaylarsa tur kesilmeden devam eder; reddederse ajan
  alternatif yol arar. `internal/permission` modeliyle tutarlı; headless
  modda politika dosyasına göre otomatik karar.

### 5.4 Auto-approve sınıflandırıcısı
- "Her şeyi sor" ile "yolo" arasında bir mod: küçük/hızlı bir model, bash
  komutlarını ve izin isteklerini bağlamla birlikte `safe | risky | unknown`
  olarak sınıflar (gerekçe ile). `safe` otomatik geçer, diğerleri sorulur.
- Önce deterministik kurallar (mevcut `command_policy.go`, `permission`
  kategorileri, salt-okunur komut listesi), model yalnızca gri bölge için.
  Model hatası/zaman aşımı → **sor** (fail-closed). Her otomatik onay trace'e
  ve denetim günlüğüne yazılır. Yıkıcı kategoriler (silme, push, ağdan
  çalıştırma, sistem ayarı) asla otomatik geçmez.
- Sınıflandırıcı için etiketli bir test seti oluştur ve yanlış-güvenli oranını
  raporla.

### 5.5 Output styles
- Ajanın üslubunu ve rapor biçimini seçmek: ör. `default`, `concise`,
  `explanatory`, `learning`. Kullanıcı kendi stilini dosyayla tanımlayabilsin
  (`.atlas/output-styles/*.md`, kullanıcı ve proje kapsamı). System prompt'un
  sabit önekini bozmayacak şekilde yerleştir (önbellek!). `/style` komutu.

### 5.6 Safe mode
- `atlas --safe-mode`: tüm kullanıcı/proje özelleştirmeleri (hooks, skills,
  MCP, plugins, output styles, context dosyaları, atlasrc builtins'in
  yan etkileri) devre dışı, yalnızca temel sağlayıcı yapılandırması ile
  başlat. Bozuk config teşhisi için. Banner ile görünür olsun; `atlas doctor`
  önerisinde geçsin.

---

## 10. Faz 6 — Paralel doğruluk

### 6.1 Paralel deneme + turnuva seçimi
- Zor görevlerde (kontrat başarısızlık serisi, kullanıcı isteği veya config)
  3–5 deneme ayrı git worktree'lerinde paralel çalışır
  (`MaxConcurrentSubAgents` sınırı ve maliyet bütçesi ile).
- Her deneme yapılandırılmış bir özet üretir: yaklaşım, değişen dosyalar,
  çalıştırılan kontroller ve sonuçları, bilinen riskler.
- Seçim: önce deterministik eleme (derlenmeyen, kontrat komutları kırmızı
  olan elenir; regresyon sayısına göre sıralama), sonra kalanlar arasında
  özetler + diff'ler üzerinden küçük gruplar halinde **turnuva** (ikili veya
  üçlü karşılaştırma, mümkünse farklı model ailesiyle).
- Kazanan ana çalışma ağacına uygulanır ve tamamlama hattından **yeniden**
  geçer. Kaybeden worktree'ler temizlenir (kullanıcı tutmak isterse
  tutulabilir). Mevcut `contract_alternatives.go` ve worktree altyapısını
  genişlet.

### 6.2 Önce keşif, sonra zenginleştirme
- Paralel mod iki aşamalı olabilsin: önce denemeler (veya ucuz keşif
  subagent'ları) kod tabanının ilgili haritasını çıkarır (giriş noktaları,
  arayüzler, tüketiciler, testler, tuzaklar) — mevcut `codemap`,
  `project_map`, `impact_analysis` ile. Bu harita birleştirilir ve son
  deneme(ler) bu zenginleştirilmiş bağlamla başlar.

### 6.3 Ara noktadan yeniden oynatma
- Başarısız deneme baştan başlatılmaz: trace ve checkpoint'lerden "doğru
  giden son adım" belirlenir (son yeşil kontrol, son geçerli kontrat durumu),
  dosya durumu o checkpoint'e döndürülür ve oradan farklı bir stratejiyle
  dallanılır. Mevcut checkpoint/rewind altyapısını kullan; kullanıcının
  commit edilmemiş işine dokunmayacak şekilde worktree üzerinde çalış.

---

## 11. Faz 7 — Otonomi ve dağıtım

### 7.1 Goal modu (uzun gözetimsiz çalışma)
- Mevcut `goal.go` üzerine: "durmadan devam et" modu. Belirli aralıklarla
  checkpoint (kontrat durumu + git worktree durumu + ilerleme özeti), bütçe
  (süre, maliyet, adım) sınırları, sonunda kanıtlı rapor. Kullanıcı istediği
  an durum görebilmeli (`atlas goal status`) ve durdurabilmeli.
- Handoff (3.6) ile uzun çalışma bağlam taşmadan sürer; duraklama tespiti
  (2.7) ile boşa dönmez.

### 7.2 Headless CI modu ve GitHub Action
- `atlas run --json` zaten tek JSON nesnesi basıyor. Genişlet: akışlı JSONL
  olay çıktısı (`--output-format stream-json`), sabit çıkış kodları
  (başarı / doğrulanamadı / engel / hata), izin politikası dosyası
  (`internal/permission/headless.go` ile).
- `deploy/` veya ayrı bir dizinde yeniden kullanılabilir bir GitHub Action:
  issue veya yorum tetikleyicisi → Atlas çalışır → dal + PR. Sırlar yalnızca
  Actions secrets üzerinden; loglarda maskeli.

### 7.3 PR bot / otomatik review
- Mevcut `atlas review` üzerine: PR diff'ini inceleyip satır içi yorum
  bırakan mod (GitHub API veya `gh`), 7.2 Action ile tetiklenir. Yapılandırılmış
  bulgular (2.2 formatı), önem eşiği, aynı bulguyu tekrar yorumlamama
  (idempotent), yanlış pozitifleri azaltmak için doğrulama adımı
  (bulguyu kodda tekrar kontrol eden ikinci geçiş).

### 7.4 Uzak/bulut çalıştırma ve devir
- Bir CLI oturumunu uzak bir makineye veya konteynere devretmek: oturum
  paketi (kontrat, handoff paketi, dal) + uzak çalışma + sonucun PR/dal olarak
  geri gelmesi. Mevcut `remote_node`, `isolation`, `mobile_relay`,
  `server` altyapısını denetle ve üzerine kur; kimlik doğrulama ve şifreli
  kanal zorunlu. Önce yerel konteyner (Docker) hedefi, sonra SSH uzak makine.

### 7.5 Proaktif ajan
- `schedule` ve `watch_comments` üzerine: kullanıcının açıkça seçtiği olay
  kaynaklarını (CI başarısız oldu, yeni issue, bağımlılık güvenlik uyarısı,
  belirli dosyalardaki `ATLAS:` yorumları) izleyen ve **yalnızca öneri veya
  taslak PR** hazırlayan bir arka plan işçisi. Kendiliğinden push/merge yok;
  her eylem kullanıcının ayarladığı izin politikasına tabi. Bildirim mevcut
  `notify` altyapısıyla.

### 7.6 Plugin paketleri
- Skill, hook, MCP sunucusu, slash komutu, output style ve agent tanımlarını
  tek bir paket olarak kurma/paylaşma: `atlas plugin install|list|remove|
  init|validate`. Manifest dosyası (ad, sürüm, içerik, gereken izinler),
  kaynak olarak yerel dizin veya git URL. Kurulum öncesi içeriği ve istenen
  izinleri gösterip onay al; hooks gibi kod çalıştıran içerik açıkça
  işaretlensin. Safe mode (5.6) tüm plugin'leri kapatır.

### 7.7 Thread paylaşımı
- `session_export` üzerine: oturumu tek dosyalık, kendi içinde bağımsız,
  okunabilir bir HTML'e (ve Markdown'a) dışa aktar — mesajlar, araç çağrıları
  (katlanabilir), diff'ler, maliyet özeti. **Sır maskeleme zorunlu** ve
  export öncesi önizleme. Ağa yükleme yok; yalnızca yerel dosya üretir.

---

## 12. Faz 8 — Harness'ın kendini iyileştirmesi

- Failure corpus (`harnessmetrics/corpus.go`, `atlas eval corpus`) ve trace'ten
  tekrar eden başarısızlık örüntülerini kümele; her küme için somut bir
  iyileştirme **önerisi** üret (prompt değişikliği, kural, araç ipucu,
  varsayılan ayar) — kanıt oturumlarıyla birlikte.
- Öneri asla otomatik uygulanmaz: önce bench'te A/B (kullanıcı onayıyla
  gerçek model çalıştırması), sonuç anlamlıysa kullanıcıya diff olarak sunulur.
- 2.8'deki kanıt bağlama kuralı burada da geçerli: kanıtı olmayan öneri
  üretilmez.

---

## 13. Faz raporu formatı (her faz sonunda, Türkçe)

1. **Özet:** bu fazda ne tamamlandı (kalem kalem, ✅ / ⚠️ kısmi / ❌).
2. **Değişen dosyalar:** yeni/değişen, ve hangilerinde kullanıcının önceden
   commit edilmemiş değişikliği vardı.
3. **Doğrulama kanıtı:** çalıştırılan komutlar ve gerçek sonuçları
   (build, vet, lint, test paketleri, `bench validate`). Çalıştırılamayanları
   ve nedenini açıkça yaz.
4. **Config ve varsayılanlar:** eklenen seçenekler, varsayılan değerler ve
   gerekçeleri.
5. **Ölçüm:** eklenen metrikler; varsa ölçülen maliyet/süre etkisi.
6. **Riskler ve bilinen sınırlamalar.**
7. **Önerilen commit dilimleri:** her biri için semantic commit mesajı
   (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`). Commit'i onay
   gelmeden yapma.
8. **Sonraki faz için onay sorusu.**

---

## 14. Genel tamamlanma tanımı (Definition of Done)

Bir kalem ancak şunların tümü doğruysa tamamdır:

- [ ] Denetim notu ve tasarım bölümü yazıldı.
- [ ] Davranışı tanımlayan testler önce kırmızıydı, şimdi yeşil.
- [ ] Dokunulan tüm paketlerin testleri `go test -p 1` ile geçiyor; önceden
      kırık olanlar ayrıca raporlandı.
- [ ] `gofumpt`, `go vet`, `task lint:fix` temiz.
- [ ] Config seçenekleri `jsonschema` açıklamalı, `schema.json` güncel,
      `atlasrc` karşılığı var.
- [ ] Varsayılan davranış korunuyor veya değişiklik gerekçelendirildi.
- [ ] Metrikler/trace olayları üretiliyor.
- [ ] Sırlar hiçbir çıktıya sızmıyor.
- [ ] Kullanıcı belgeleri güncel.
- [ ] Kullanıcının commit edilmemiş işi korunmuş.

---

## 15. Kalem listesi (kontrol listesi)

**Faz 1 — Ölçüm:** 1.1 Trace + `atlas eval trace` · 1.2 Bench genişletme +
ablasyon · 1.3 Kapı metrikleri · 1.4 Sıkıştırma kalite eval'i · 1.5 Prompt
token golden testi

**Faz 2 — Doğrulama:** 2.1 Tek tamamlama hattı · 2.2 Yapılandırılmış evaluator
kararı · 2.3 Kırmızı→yeşil akışı · 2.4 Lint-fix döngüsü · 2.5 Mutasyon ile
test kalitesi · 2.6 Property/fuzz önerisi · 2.7 Akıllı döngü/duraklama
tespiti · 2.8 Kanıta bağlı öğrenilmiş kurallar

**Faz 3 — Bağlam:** 3.1 Prompt cache koruma · 3.2 Context anxiety önlemi ·
3.3 Uyarlamalı budama · 3.4 Boşta sıkıştırma · 3.5 `@dosya` yol referansı ·
3.6 Handoff / temiz başlangıç

**Faz 4 — Model yönlendirme:** 4.1 Aşamaya göre model/effort · 4.2
Architect/editor · 4.3 Oracle aracı · 4.4 Farklı aileden inceleyici

**Faz 5 — İş akışı:** 5.1 Spec modu · 5.2 Steer modu · 5.3 Runtime izin
yükseltme · 5.4 Auto-approve sınıflandırıcısı · 5.5 Output styles · 5.6 Safe
mode

**Faz 6 — Paralel doğruluk:** 6.1 Paralel deneme + turnuva · 6.2 Keşif sonra
zenginleştirme · 6.3 Ara noktadan yeniden oynatma

**Faz 7 — Otonomi/dağıtım:** 7.1 Goal modu · 7.2 Headless CI + GitHub Action ·
7.3 PR bot · 7.4 Uzak/bulut devir · 7.5 Proaktif ajan · 7.6 Plugin paketleri ·
7.7 Thread paylaşımı

**Faz 8 — Öz-iyileştirme:** 8.1 Corpus'tan kanıtlı iyileştirme önerileri

Kapsam dışı (bilerek): ACP desteği, özelleştirilebilir statusline,
hava boşluklu mod, `<user_authorization>` değişikliği, araç silme.

---

**Başla:** Faz 0'ı yürüt, denetim belgesini ve temel çizgi raporunu üret, sonra
Faz 1 için onay iste.
