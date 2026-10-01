# ATLAS model, plan ve hesap bağlantısı denetimi

Denetim tarihi: **1 Ekim 2026 (Europe/Istanbul)**.

Bu belge düzeltme öncesi denetimi kaydeder. Sonraki kod değişiklikleri ve
doğrulama sonuçları [düzeltme raporunda](provider-catalog-fixes-2026-10-01.md),
güncel ID karşılaştırması ise
[düzeltme sonrası denetimde](provider-catalog-audit-after-fixes-2026-10-01.json)
yer alır.

## Sonuç ve kapsam

Katalogun tamamı güncel değil. 66 sağlayıcı kaydındaki 1.836 model kaydı
incelendi; bunlar benzersiz modeller değildir, aynı model farklı
sağlayıcılarda tekrar bulunur. 15 sağlayıcının herkese açık model listesi
alınarak ID karşılaştırması yapıldı. Diğer sağlayıcıların açık uç noktaları
kimlik doğrulama istedi, erişim hatası verdi veya hesap/dağıtım belgeleri
gerektirdi. Başlıca model üreticileri, planlar ve kapanışlar ayrıca resmî
belgelerden araştırıldı. Bütün 1.836 kaydın fiyatı, protokolü ve hesapta
çalışması doğrulanmış değildir.

Bu denetimde uygulamanın model/provider kodu değiştirilmedi. Yalnızca
araştırma raporu, ham karşılaştırma ve tekrar çalıştırılabilir denetim
aracı eklendi. Gerçek anahtar, OAuth oturumu veya ücretli model çağrısı
kullanılmadı.

Ham kanıt: [sağlayıcı başına JSON karşılaştırması](provider-catalog-audit-2026-10-01.json).
Tekrar çalıştırma: `node scripts/audit-provider-models.mjs`.

**Kanıtın anlamı:** Açık listede bulunmamak modelin kesin kapatıldığı
anlamına gelmez; özel erişim, alias, bölge ve plan farkı olabilir. Buna
karşılık resmî kapanış duyurusu güçlü kanıttır. 401/403 yanıtı modelin
geçersiz olduğunu göstermez. Bir modelin üreticide bulunması da aracı
sağlayıcıda, abonelikte veya kullanıcının hesabında bulunduğunu kanıtlamaz.

## Öncelikli bulgular

### 1. Groq varsayılanlarının ikisi de kapanmış

Yerel büyük model `moonshotai/kimi-k2-instruct-0905`, küçük model
`qwen/qwen3-32b`. Resmî kapanış tarihleri sırasıyla **15 Nisan 2026** ve
**17 Temmuz 2026**. Güncel listede `openai/gpt-oss-120b`,
`openai/gpt-oss-20b` ve `qwen/qwen3.8-27b` bulunuyor; yerel Groq kataloğunda
bu modeller yok. Varsayılanlar ve mevcut iki kayıt düzeltilmeli.
[Kapanış duyuruları](https://console.groq.com/docs/deprecations),
[güncel modeller](https://console.groq.com/docs/models).

### 2. Cohere'de deprecated alias ve doğrulanmayan model ID'si var

Küçük varsayılan `command-r` ve katalogdaki `command-r-plus` resmî tabloda
15 Eylül 2025'ten beri deprecated. Büyük varsayılan
`command-a-reasoning-32k` için güncel resmî model tablosunda eşleşme yok;
belgelenmiş ID `command-a-reasoning-08-2025`.
`command-a-plus-05-2026` ve `north-mini-code-1-0` ayrıca araştırılmalı.
Model ID'si, görüntü desteği ve protokol doğrulanmadan bunları çalışan
entegrasyon olarak sunmak doğru olmaz.
[Cohere model tablosu](https://docs.cohere.com/docs/models),
[North Mini Code](https://docs.cohere.com/docs/north-mini-code-1.0).

### 3. Hesap girişlerinin yedisinde model çağrısı tamamlanmamış

`grok-web`, `windsurf`, `jetbrains`, `augment`, `factory`, `coderabbit`
ve `zed` provider dosyalarında `Generate` ve `Stream` doğrudan
`not yet implemented` hatası döndürüyor. Bu, dış kaynak tahmini değil,
yerel kod bulgusudur. Login/model seçici görünmesi çalışan sohbet desteği
olarak değerlendirilmemeli. Önce çağrı katmanı tamamlanmalı veya ürün
arayüzünde destek durumu belirtilmeli.

Kanıt dosyaları: `internal/deps/atlas-llm/providers/<provider>/<provider>.go`;
Grok için `providers/grokweb/grokweb.go`. Claude ve Muse bu sınıfa dahil
edilmedi; kodları gerçek Anthropic uyumlu adaptöre delegasyon yapıyor.
Ancak bunların hesap yetkileri canlı oturumla doğrulanmadı.

### 4. Mistral'de ürün adı API ID'si gibi kullanılmış

Yerel `mistral-medium-3.5` yerine resmî ID `mistral-medium-3-5`;
yerel `devstral-2-2512` yerine belgelenmiş ID `devstral-2512`.
Büyük varsayılan `mistral-large-3`; resmî model sayfası
`mistral-large-2512` ID'sini gösteriyor. Olası aliaslar hesap model
listesinden ayrıca doğrulanmalı. Devstral 2, 22 Mayıs 2026'dan beri
deprecated; önerilen model Medium 3.5. Le Chat aboneliği ile API anahtarı
erişimi aynı varsayılmamalı.
[Medium 3.5](https://docs.mistral.ai/models/mistral-medium-3-5-26-04),
[Large 3](https://docs.mistral.ai/models/mistral-large-3-25-12),
[Devstral 2](https://docs.mistral.ai/models/devstral-2-25-12).

### 5. Açık listede olmayan varsayılanlar

Hugging Face: `meta-llama/Llama-4-Maverick` büyük varsayılanı açık listede
yok; listede tam sürüm ID'leri mevcut. NeuralWatt: `glm-5.2` ve
`glm-5.2-fast` varsayılanları yok; `glm-5.3` / `glm-5.3-flash` mevcut.
Venice: `stealth-ox-alpha` küçük varsayılanı yok. Bunlar hesapla veya
sağlayıcı alias belgeleriyle doğrulanmalı; otomatik olarak kesin kapatılmış
sayılmadı. Kaynaklar ham JSON'daki ilgili `url` alanlarıdır.

## Yeni modellerin doğrulaması

| Sağlayıcı | Resmî belgede doğrulanan güncel modeller | Yerel durum |
| --- | --- | --- |
| OpenAI API | `gpt-6-astra`, `gpt-6.1-sol`, `gpt-6-sol`, `gpt-6-luna` | Dört ID var; varsayılanlar 6.1 Sol / 6 Luna. |
| Anthropic API | `claude-fable-5-1`, `claude-opus-5-5`, `claude-sonnet-5-5`, Haiku 4.5 | Yeni üç ID var; diğer eski kayıtların yaşam döngüsü ayrıca değerlendirilmelidir. |
| DeepSeek API | `deepseek-flash`: V4.1 Flash; `deepseek-v4-pro`: 0813 Pro | Güncel doğrudan ID var. Üreticinin aliasları gateway ID'leriyle aynı değildir. |
| Google Gemini | `gemini-3.8-flash` | Kayıt var; küçük varsayılan hâlâ `gemini-3-flash-preview`. |
| xAI | `grok-4.7` | Kayıt ve varsayılanlar var. |
| Xiaomi | `mimo-v2.6-pro`, `mimo-v2.6-flash` | API ve üç bölgesel Token Plan kaydında mevcut. |
| Z.AI | `glm-5.3-flash`, API için ayrıca `glm-5.3-flashx` | Flash var; FlashX yok. FlashX Coding Plan'a dahil değil. |
| MiniMax | `MiniMax-M3.1-Flash-Preview` | Yerel kayıtlarda yok; yalnızca M Plan/MiniMax Code erişimi belgeleniyor. |
| Cerebras | `gpt-oss-120b`, `qwen-3.8-27b` | GPT-OSS var; Qwen 3.8 yok. Yerel Gemma 4 için genel Shared Inference listesinde eşleşme yok. |

Kaynaklar: [OpenAI Docs](https://developers.openai.com/api/docs/models),
[Claude](https://platform.claude.com/docs/en/models/overview),
[DeepSeek](https://api-docs.deepseek.com/quick_start/pricing/),
[Gemini](https://ai.google.dev/gemini-api/docs/models/gemini-3.8-flash/),
[Grok](https://docs.x.ai/developers/models/grok-4.7),
[Xiaomi](https://mimo.mi.com/docs/en-US/tokenplan/Token%20Plan/subscription),
[GLM](https://docs.z.ai/guides/vlm/glm-5.3-flash),
[MiniMax](https://platform.minimax.io/docs/guides/text-generation),
[Cerebras](https://inference-docs.cerebras.ai/models/overview).

## Token Plan / Coding Plan ayrımları

- **Xiaomi:** API anahtarı ile `tp-` plan anahtarı ayrıdır. Token Plan için
  CN/SGP/AMS uç noktaları kullanılır. Metin modelleri 2.6 Pro/Flash ile
  2.5 Pro/2.5; UltraSpeed plan listesinde yok. Eski iki metin modeli
  **21 Ekim 2026, 10:00 Pekin** tarihinde kaldırılacak.
  [Resmî plan belgesi](https://mimo.mi.com/docs/en-US/tokenplan/Token%20Plan/subscription).
- **Alibaba:** Yerelde Singapore/US genel API sağlayıcıları var;
  `https://coding-intl.dashscope.aliyuncs.com/v1` ve ayrı `sk-sp-` anahtarı
  kullanan Coding Plan kaydı yok. Bu plan yalnızca açıkça listelenmiş
  on model sürümünü destekliyor; genel API'nin en yeni Qwen modeli otomatik
  olarak plana eklenemez. [Resmî plan](https://www.alibabacloud.com/help/en/model-studio/coding-plan).
- **MiniMax:** Subscription Key ve pay-as-you-go API Key birbirinin yerine
  kullanılmaz. Token Plan ve yeni M Plan kapsamı ayrı incelenmeli.
  M3.1 Flash Preview erişiminin genel API/Token Plan'a açık olduğu
  varsayılmamalı. Doküman ve abonelik sayfasında aylık fiyatlar da
  farklı görünüyor; sabit fiyat eklenmemeli.
  [Token Plan](https://platform.minimax.io/docs/token-plan/intro),
  [model erişim koşulu](https://platform.minimax.io/docs/guides/text-generation).
- **Z.AI:** Yerel `zai` kaydı zaten `/api/coding/paas/v4` adresine gider;
  adı genel API gibi görünür. Coding Plan ile standart API ayrı
  sunulmalı. FlashX plana dahil değil.
  [Flash/FlashX kapsamı](https://docs.z.ai/guides/vlm/glm-5.3-flash).
- **OpenCode:** Go ve Zen ayrı plan/sağlayıcılar. Aynı modelin iki plandaki
  protokolü veya bulunabilirliği aynı varsayılmamalı.
  [Go](https://opencode.ai/docs/go/), [Zen](https://opencode.ai/docs/zen/).

## Kapanışlar ve yeni protokol gereksinimleri

- Gemini `gemini-3-pro-preview` **9 Mart 2026**'da kapanmış; Gemini ve
  Vertex yerel listelerinde hâlâ bulunuyor. Gemini API kapanışını Vertex
  için otomatik olarak aynı kabul etmiyorum; Vertex kendi yaşam döngüsü
  ve bölge belgeleriyle doğrulanmalı.
  `gemini-3-flash-preview` için kapanış tarihi açıklanmamış; eski olması
  kapatıldığı anlamına gelmez.
  [Gemini yaşam döngüsü](https://ai.google.dev/gemini-api/docs/deprecations).
- Claude Sonnet 4.5 için **30 Eylül 2026**'da yeni duyuru yapılmış;
  kapanış **30 Kasım 2026**. Henüz kapatılmış olarak işaretlenmemeli.
  [Anthropic duyurusu](https://platform.claude.com/docs/en/about-claude/model-deprecations).
- Claude 5.5'te yalnızca ID eklemek yeterli değil: örnek olarak Sonnet
  5.5'in `between_tools` düşünme modu ve zorunlu araç seçimi kısıtları var.
  Yerel Anthropic adaptöründe effort dalı sıcaklık/top-p/top-k alanlarını
  otomatik temizlemiyor; özelleştirilmiş istekler için uyumluluk kontrolü
  gerekli. Bu bulgu canlı 400 testi değil, kaynak/kod karşılaştırmasıdır.
  [Sonnet 5.5 değişiklikleri](https://platform.claude.com/docs/en/models/sonnet-5-5/whats-new-sonnet-5-5).
- GLM 5.3 Flash düşünmeyi kapatmayı desteklemiyor. FlashX'in API'de
  bulunması Coding Plan erişimi sağlamıyor.
  [GLM özellikleri](https://docs.z.ai/guides/vlm/glm-5.3-flash).
- OpenAI model fiyatları, Gemini'nin veya OpenRouter'ın fiyatı olarak
  kopyalanamaz. Uzun bağlam, cache, Fast/priority/flex ve bölge farkları
  mevcut düz maliyet alanlarına tam sığmaz. Hesap/model erişimiyle fiyat
  güncelliği farklı kontrollerdir.

## Gateway ve hesap erişimi

OpenRouter'da açık listede olup yerel katalogda olmayan **araç destekli
metin modeli sayısı bu filtreyle sıfır**. Listede eksik olan 66 kayıt;
görsel/ses, özel yönlendirme, araçsız veya farklı amaçlı modeller içerir.
Bu yüzden hepsini coding-agent model seçicisine eklemek doğru değil.
Ancak 20 yerel kayıt açık listede yok; geçmiş kayıtların temizliği gerekiyor.

GitHub Copilot resmî tablosu yeni GPT-6, Claude 5.5, Gemini 3.8 ve Grok 4.7
ailelerini doğruluyor. Gerçek backend ID'leri ve bağlam/effort izinleri
hesap model listesinde ayrıca denetlenmeli. API üreticisinin 1M bağlamı
Copilot hesabına otomatik kopyalanamaz.
[Copilot model/istemci/plan tablosu](https://docs.github.com/en/copilot/reference/ai-models/supported-models).

Factory'nin resmî listesi yeni modelleri içerirken Atlas Factory kataloğu
yalnızca üç eski kayıt içeriyor; ayrıca adaptör henüz tamamlanmamış.
[Factory modelleri](https://docs.factory.com/models).

Azure deployment adları, AWS inference profile ID'leri ve Vertex model
ID'leri doğrudan üretici API ID'leriyle eşit değildir. Bölge ve proje
erişimi gerektiren listeler canlı hesap olmadan kesinleştirilemez.
[AWS](https://docs.aws.amazon.com/bedrock/latest/userguide/model-cards.html),
[Vertex](https://docs.cloud.google.com/vertex-ai/generative-ai/docs/models),
[Azure yaşam döngüsü](https://learn.microsoft.com/en-us/azure/foundry/openai/concepts/model-retirement-schedule).

## Önerilen düzeltme sırası

1. Resmî kapanışı doğrulanmış varsayılanları ve yanlış/doğrulanmayan native
   ID'leri düzelt: Groq, Cohere, Mistral ve Gemini.
2. Açık listede bulunmayan varsayılanları sağlayıcı belgeleri/hesap
   listesiyle doğrula: Hugging Face, NeuralWatt, Venice.
3. OpenCode ve diğer gateway farklarını modalite ve araç desteğiyle filtrele;
   doğrulanmış model ID'si kadar gerçek protokolü de doğrula.
4. Alibaba Coding Plan, MiniMax M Plan ve Z.AI genel API/plan ayrımlarını
   açık sağlayıcı kayıtları olarak tasarla; erişim kapsamını karıştırma.
5. Tamamlanmamış yedi hesap adaptörünü çalışan girişlerden ayır.
6. Hesaba özgü `/models` kontrolü ile erişim ve çıktı/bağlam sınırlarını
   doğrula; sonrasında yeni varsayılanları seç.

## 66 sağlayıcının açık liste kontrolü

Aşağıdaki sayılar tüm API kategorilerini kapsar; eksik kayıtların hepsi
coding-agent için uygun değildir. “Listede yok” yalnızca karşılaştırma
sonucudur. Detaylı model ID farkları ham JSON dosyasındadır.

| Sağlayıcı | Yerel kayıt | Açık liste | Yerelde eksik | Açık listede yok | Açık listede olmayan varsayılanlar | Sonuç |
| --- | ---: | ---: | ---: | ---: | --- | --- |
| aihubmix | 285 | 417 | 158 | 26 | — | Açık liste alındı (HTTP 200) |
| alibaba-singapore | 20 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| alibaba-us | 4 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| amp | 4 | — | — | — | — | Açık liste doğrulanamadı |
| anthropic | 17 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| antigravity | 5 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| atlascloud | 32 | 124 | 94 | 2 | — | Açık liste alındı (HTTP 200) |
| augment | 2 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| avian | 13 | 14 | 2 | 1 | — | Açık liste alındı (HTTP 200) |
| azure | 14 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| baseten | 13 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| bedrock | 11 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| bedrock-europe | 10 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| bolt | 3 | — | — | — | — | Açık liste doğrulanamadı |
| cerebras | 2 | — | — | — | — | Açık liste doğrulanamadı (HTTP 403) |
| chatgpt | 7 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| chutes | 12 | 14 | 2 | 0 | — | Açık liste alındı (HTTP 200) |
| claude | 8 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| coderabbit | 3 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| codex-ide | 3 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| cohere | 4 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| copilot | 41 | — | — | — | — | Açık liste doğrulanamadı (HTTP 400) |
| cortecs | 100 | 109 | 18 | 9 | — | Açık liste alındı (HTTP 200) |
| deepseek | 3 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| factory | 3 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| fireworks | 17 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| gemini | 11 | — | — | — | — | Açık liste doğrulanamadı (HTTP 403) |
| grok-web | 7 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| groq | 2 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| huggingface | 4 | 132 | 130 | 2 | meta-llama/Llama-4-Maverick | Açık liste alındı (HTTP 200) |
| ionet | 31 | 38 | 7 | 0 | — | Açık liste alındı (HTTP 200) |
| jetbrains | 13 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| kimi-coding | 4 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| minimax | 8 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| minimax-china | 8 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| minimax-coding | 3 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| mistral | 6 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| moonshot | 11 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| muse | 5 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| nebius | 24 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| neuralwatt | 18 | 22 | 11 | 7 | glm-5.2, glm-5.2-fast | Açık liste alındı (HTTP 200) |
| nvidia-nim | 14 | 81 | 75 | 8 | — | Açık liste alındı (HTTP 200) |
| openai | 32 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| opencode-go | 48 | 43 | 6 | 11 | — | Açık liste alındı (HTTP 200) |
| opencode-zen | 79 | 84 | 8 | 3 | — | Açık liste alındı (HTTP 200) |
| openrouter | 416 | 462 | 66 | 20 | — | Açık liste alındı (HTTP 200) |
| perplexity | 3 | — | — | — | — | Açık liste doğrulanamadı (HTTP 404) |
| phind | 2 | — | — | — | — | Açık liste doğrulanamadı |
| qiniucloud | 14 | — | — | — | — | Açık liste doğrulanamadı |
| scaleway | 12 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| synthetic | 11 | 11 | 2 | 2 | — | Açık liste alındı (HTTP 200) |
| venice | 103 | 127 | 28 | 4 | stealth-ox-alpha | Açık liste alındı (HTTP 200) |
| vercel | 222 | 397 | 189 | 14 | — | Açık liste alındı (HTTP 200) |
| vercel-v0 | 3 | — | — | — | — | Açık liste doğrulanamadı (HTTP 404) |
| vertexai | 12 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| windsurf | 5 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| xai | 7 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| xiaomi | 5 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| xiaomi-token-plan-ams | 4 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| xiaomi-token-plan-cn | 4 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| xiaomi-token-plan-sgp | 4 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| yi | 2 | — | — | — | — | Açık liste doğrulanamadı |
| zai | 13 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| zed | 5 | — | — | — | — | Hesap/dağıtım veya belge gerekiyor |
| zhipu | 12 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |
| zhipu-coding | 13 | — | — | — | — | Açık liste doğrulanamadı (HTTP 401) |

Yerel varsayılanların kendi kataloglarında bulunması ve duplicate ID kontrolü: 66 sağlayıcıda sorun bulunmadı. Bu yapı kontrolü, modelin dış API’de aktif olduğunu göstermez.
