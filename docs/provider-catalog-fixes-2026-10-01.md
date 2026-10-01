# Katalog ve bağlantı güncellemesi — 1 Ekim 2026

Sonraki çalışma: bu raporda kaydedilen altı paket hatasının kök nedenleri
[test düzeltme raporunda](test-fixes-2026-10-01.md) ve güncel doğrulama
sonuçları [yeni test kaydında](test-fixes-verification-2026-10-01.json) bulunur.

Atlas’ın seçilebilir katalogu **67 sağlayıcı ve 2.199 sağlayıcı/model kaydı** içeriyor. Aynı modelin farklı sağlayıcılardaki sunumları ayrı kayıtlardır. Sayım, varsayılanlar ve HEAD ile karşılaştırma [kapsama dosyasında](provider-catalog-coverage-2026-10-01.json) bulunur.

Kapsam, Atlas’ın konuşma ve araç çağırma protokolleriyle kullanabildiği metin modelleri ve görüntü anlama/reasoning yetenekleridir. Embedding, reranking, ses/video/görsel üretimi ve farklı protokol gerektiren servisler konuşma modeli gibi eklenmedi. Yayımlanan bir model kişisel hesabın o modele erişimi olduğu anlamına gelmez.

## Xiaomi API ve Token Plan

| Sağlayıcı | Ortam değişkeni | API adresi | Kayıt |
| --- | --- | --- | ---: |
| `xiaomi` | `XIAOMI_API_KEY` | `https://api.xiaomimimo.com/v1` | 5 |
| `xiaomi-token-plan-cn` | `XIAOMI_TOKEN_PLAN_CN_API_KEY` | `https://token-plan-cn.xiaomimimo.com/v1` | 4 |
| `xiaomi-token-plan-sgp` | `XIAOMI_TOKEN_PLAN_SGP_API_KEY` | `https://token-plan-sgp.xiaomimimo.com/v1` | 4 |
| `xiaomi-token-plan-ams` | `XIAOMI_TOKEN_PLAN_AMS_API_KEY` | `https://token-plan-ams.xiaomimimo.com/v1` | 4 |

MiMo 2.6 Pro, 2.6 Flash, 2.5 Pro ve 2.5 plan kataloglarında; API ayrıca doğrulanan UltraSpeed sunumunu içerir. API ve plan anahtarları ayrı saklanır; yanlış bölgenin anahtarı otomatik kullanılmaz. Native `thinking.type` gönderilir ve araç çağrıları arasındaki `reasoning_content` korunur. Yerel HTTP testi bu davranışı doğruladı.

```sh
atlas login xiaomi
atlas login xiaomi-token-plan-sgp
atlas login --list
```

Anahtar girişte gizlenir. Otomasyon için `--api-key-stdin` kullanılabilir. [Resmî Xiaomi platformu](https://platform.xiaomimimo.com/).

## API, abonelik ve hesap bağlantıları

| Bağlantı | Uygulanan kapsam |
| --- | --- |
| Alibaba Coding | Uluslararası ve CN için ayrı sağlayıcı, adres ve anahtar; her birinde resmî 10 model. Yeni PAYG modelleri otomatik plan yetkisi sayılmaz. |
| Alibaba Personal Token Plan | CN ve SGP; her birinde 12 model ve ayrı anahtar. |
| Alibaba Team Token Plan | CN ve SGP; her birinde 21 model ve ayrı anahtar. |
| Kimi Coding | Çin ve global adresler ayrı; her birinde 4 doğrulanan kimlik. |
| MiniMax M Plan | `MiniMax-M3.1-Flash-Preview`, 1M bağlam, zorunlu adaptive thinking, `low/medium/high/xhigh/max`, varsayılan `max`. |
| MiniMax Token Plan | Mevcut `minimax-coding` kimliği korunur; M3 ve M2.7/Highspeed. Team üyeleri kendi Subscription Key’i ile aynı bağlantıyı kullanabilir; yeni Team alımları 5 Eylül’de durduruldu. |
| Qiniu Token Plan | 9 yetkili model; kapanmış GLM4.6/4.7, Kimi2.5 ve DeepSeek3.2 kayıtları kaldırıldı. |
| Z.AI | Mevcut `zai` Coding Plan olarak korunur. Genel API ayrı `zai-api`, `ZAI_PAYGO_API_KEY`, `https://api.z.ai/api/paas/v4`. Plan anahtarı PAYG’yi otomatik etkinleştirmez. |
| Amazon Bedrock OpenAI | Ayrı `bedrock-openai`, `AWS_BEDROCK_OPENAI_API_KEY`, Responses protokolü; 7 resmî US/global kimlik. GPT6.1 Sol için yayımlanmamış global profil eklenmedi. |
| Meta Model API | Ayrı `meta-api`, `MODEL_API_KEY`, `https://api.meta.ai/v1`; 5 Muse Spark sunumu, Responses protokolü. Standart 1.3 varsayılan. Contributor sürümleri açık isimlidir ve varsayılan yapılmaz. |

Planlarda token maliyeti alanlarının sıfır olması, aboneliğin ücretsiz olduğu anlamına gelmez. Kota, abonelik ücreti ve Credits sağlayıcı tarafından belirlenir. Yayımlanmamış çıktı sınırları için 4.096 gibi işletim varsayılanları kullanıldı; bunlar azami kapasite iddiası değildir.

Kaynaklar: [Alibaba Coding](https://www.alibabacloud.com/help/en/model-studio/coding-plan), [MiniMax M Plan](https://platform.minimax.io/docs/m-plan/intro), [MiniMax Team](https://platform.minimax.io/docs/guides/pricing-token-plan-team), [Z.AI ayrımı](https://docs.z.ai/api-reference/introduction), [Meta modeller](https://dev.meta.ai/docs/models). Diğer kaynakların tam adresleri ve SHA256 değerleri aşağıdaki kanıt dosyalarındadır.

## Güncel modeller ve gerçek sunumlar

| Alan | Sonuç |
| --- | --- |
| OpenAI / Azure | GPT6.1 Sol, GPT6 Astra/Sol/Luna ve GPT5.6 ailesi. Azure’ın kendi bağlam/çıktı ve cache fiyatları kullanılır; doğrudan API metadata’sı körlemesine kopyalanmaz. |
| Anthropic | Fable5.1, Opus5.5, Sonnet5.5; Mythos5.1 resmî `claude-mythos-5-1` kimliğiyle **Invite Only** olarak eklendi. Erişim gerektiren hesap yetkisi otomatik sağlanmaz. |
| Gemini / Vertex | Flash3.8/3.7/3.6 ve sunulan Pro modelleri; emekli Gemini3 Pro Preview kaldırıldı. Vertex’te yeni Claude modellerinin gerçek düz kimlikleri kullanılır. Mythos5.1 yine davet gerektirir. |
| DeepSeek | Güncel `deepseek-flash` V4.1 Flash; V4 Pro ayrı tutuldu. `deepseek-v4-flash` mevcut yapılandırmalar için resmî alias olarak korunur; adı **DeepSeek V4 Flash (Alias to V4.1 Flash)**. Cache okuma `.006`, yazma `0`, bağlam 1.048.576. |
| xAI / Z.AI | Grok4.7 ve GLM5.3 ailesi; sunulan kimlik, reasoning ve fiyatlar ayrı. |
| MiniMax | PAYG M3 varsayılan; kalıcı indirimli Standard kısa bağlam fiyatı `.30/1.20`, cache okuma `.06`. Uzun bağlam ve Priority farklı fiyatlanır. M2.x cache okuma/yazma alanları düzeltildi. Çin fiyatları CNY’den ECB kuruyla USD’ye çevrildi. M3.1 Flash Preview PAYG’ye taşınmadı. |
| OpenCode Zen / Go | 82/42 kayıt; model bazında Anthropic, Responses, Google veya uyumlu Chat Completions yönlendirmesi. Gemini adresindeki `/v1` ve oturum affinity başlığı düzeltildi. |
| Hugging Face | 171 kayıt `model:provider` kimliğiyle yönlendirilen sağlayıcıya sabitlendi; farklı sunumların fiyat/bağlamı karıştırılmadı. |
| AIHubMix / Cortecs / Avian | 303/103/14 kayıt, yayımlanan katalogla eşleşen metadata. Cortecs EUR fiyatları ECB USD kuruyla çevrildi. |
| NVIDIA / Qiniu API | 21/38 doğrulanan kayıt. NVIDIA GLM5.3 Flash bağlamı 1.048.576 ve varsayılan effort `max`; yalnız ID dönen listeden bilinmeyen özelliklerle model üretilmez. |
| Baseten / Fireworks / Nebius | 11/12/20 kayıt; resmî aktif liste, router kimlikleri, fiyat ve hosted bağlam sınırlarıyla eşitlendi. |
| Scaleway | 11 tool destekli model, ürün sayfasının gerçek API metadata’sı; EUR fiyatlar ECB kuruyla USD’ye çevrildi. |
| Cerebras / Groq / Cohere / Mistral | Sunulmayan/emekli modeller, yanlış kimlik ve adresler düzeltildi. Cerebras Qwen3.8 27B için ücretsiz katmanın güvenli sınırları kullanılır; ücretli katman daha büyük sınır sunar. |
| OpenRouter / Vercel / Venice | 392/252/122 tool destekli metin kaydı; kendi fiyat, bağlam ve görüntü metadata’sı. OpenRouter büyük varsayılanı Sonnet5.5. Negatif/değişken fiyatlı router’lar sabit ücretli model gibi eklenmez. |
| Diğer hosted API’ler | Chutes, io.net, Neuralwatt, Synthetic ve AtlasCloud kendi yayımlanan sunumlarıyla karşılaştırıldı. Başka sağlayıcıdaki kapasite varsayılmadı. |

Statik maliyet alanları Standard, kısa bağlam ve ilgili bölge fiyatını temsil eder. AWS bölgesel Claude/OpenAI fiyat farkları ayrı işlenir. Vertex Flash3.6/3.7/3.8 promosyonu **31 Aralık 2026** sonunda biter. EUR/CNY dönüşüm tarihi kanıt dosyasında kayıtlıdır. Sağlayıcı faturası uzun bağlam, Priority, bölge ve özel sözleşmelerle farklı olabilir. MiniMax M3 otomatik cache için ayrı bir yazma fiyatı yayımlamaz; M2.x açık cache yazma fiyatı M3’e kopyalanmadı.

Kaynaklar: [DeepSeek fiyat/alias](https://api-docs.deepseek.com/quick_start/pricing), [Mythos5.1](https://platform.claude.com/docs/en/models/mythos-5-1/overview), [Claude Vertex](https://platform.claude.com/docs/en/build-with-claude/claude-on-vertex-ai), [MiniMax fiyatları](https://platform.minimax.io/docs/guides/pricing-paygo), [MiniMax protokolü](https://platform.minimax.io/docs/api-reference/text-anthropic-api), [Scaleway](https://www.scaleway.com/en/generative-apis/), [ECB](https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml).

## Hesap girişi ve istek davranışı

ChatGPT/Codex hesabı, Claude hesabı, Copilot, Antigravity ve Muse için mevcut çalışan adaptörler korunur. Hesap/plan erişimini giriş sonrasında sağlayıcı belirler. Antigravity adaptörü Gemini protokolü içindir; hesabın sunduğu başka aileler aynı adaptöre eklenmedi.

Grok Web, Windsurf, JetBrains, Augment, Factory, CodeRabbit ve Zed için model çağrısı tamamlanmamış adaptörler giriş/model seçiminde sunulmaz. Amp/Bolt/Phind doğrulanmış inference bağlantısı değildir; `codex-ide` yerine API için OpenAI, hesap için ChatGPT kullanılır. Toplam 11 bağlantı gizlidir; manuel yanlış yapılandırmada açıklayıcı hata kimlik bilgileri çözülmeden üretilir.

İstek katmanındaki düzeltmeler:

- MiniMax ve hesap bağlantılarında süreçteki başka Anthropic anahtarı istemeden HTTP başlığına eklenmez. Ortam değişkeni global olarak silinmez.
- Claude/Mythos adaptive thinking yolunda geçersiz sampling parametreleri çıkarılır. MiniMax M3 düşünme isteği adaptive olur; kapalı varsayılanı korunur. M3.1’in zorunlu düşünmesi kapatılmaz.
- Alibaba GLM5.3 için zorunlu `enable_thinking=true` korunur.
- Vertex Claude seçenekleri Anthropic, Gemini seçenekleri Google olarak üretilir. Gemini3.8 geçersiz sampling parametreleri almaz ve araç ID’leri korunur.
- Antigravity resim içeriğini, araç kimliklerini ve thought signature bilgisini tekrar isteğe taşıyabilir; Generate/Stream yolları test edildi.
- Copilot Responses ile Claude/Muse hesaplarının reasoning seçenekleri doğru protokolde taşınır; OpenCode model ve oturum yönlendirmesi düzeltildi.

## Doğrulama sonucu

Son kod değişikliklerinden sonra:

- `go test -p 1 ./internal/config ./internal/cmd ./internal/discover ./internal/ui/dialog ./internal/deps/atlas-models/... ./internal/deps/atlas-llm/providers/...` **geçti**.
- Agent paketindeki Xiaomi, MiniMax, Alibaba, OpenCode, Meta/Bedrock, Vertex, Copilot, Claude ve Muse yönlendirme testleri **geçti**.
- `CGO_ENABLED=0 GOEXPERIMENT=greenteagc go build -p 1 .` **geçti**; doğrulama binary’si geçici dizine yazıldı.
- `git diff --check` ve sağlayıcı içi tekil ID/varsayılan kontrolleri **geçti**.
- `go test -p 1 ./...` önceki doğrulama turunda çalıştırıldı; **tam süit geçmedi**. Altı pakette hata var: `internal/agent`, `internal/agent/tools`, `internal/agent/tools/mcp`, `internal/oauth/muse`, `internal/shell`, `internal/shellconfig`. İki agent hatası önceki HEAD’de de yeniden üretildi. Diğer hatalar Windows shell/process ve HOME/USERPROFILE davranışlarıyla ilgili; bu çalışmada hepsinin baseline olduğu kanıtlanmadı. Son MiniMax/Mythos değişikliği sonrasında ilgili paketler yeniden geçti; tam süit tekrar çalıştırılmadı.
- Bağımsız incelemenin bulduğu anahtar sızıntısı, yanlış protokol, sampling, zorunlu düşünme ve signature/araç geçmişi hataları düzeltildi.
- Gerçek API anahtarı veya OAuth hesabıyla ücretli canlı inference yapılmadı. HTTP testleri yerel sunucuyla; güncellik kontrolleri kamuya açık resmî GET kaynaklarıyla yapıldı.

## Tekrarlanabilir kanıt ve sınırlar

Kaynak adresleri, yakalama zamanları ve SHA256 değerleri:

- [Ana katalog senkronizasyonu](provider-catalog-completion-evidence-2026-10-01.json)
- [Bulut sağlayıcıları](provider-cloud-catalog-evidence-2026-10-01.json)
- [Hosted sağlayıcılar](provider-hosted-catalog-evidence-2026-10-01.json)
- [Meta API](provider-meta-catalog-evidence-2026-10-01.json)
- [MiniMax ve Mythos](provider-native-catalog-evidence-2026-10-01.json)
- [Son açık liste karşılaştırması](provider-catalog-audit-after-fixes-2026-10-01.json)
- [Test sonuç kaydı](provider-catalog-verification-2026-10-01.json)

`scripts/complete-*-provider-catalog.mjs` kaynak kontrolleri tamamlanmadan katalog dosyalarını yazmaz. Metadata gerektiğinde models.dev indeksinden alınır ve sağlayıcının gerçek sunumu/resmî belgeleriyle sınırlandırılır; indeks tek başına hesap yetkisi veya endpoint kanıtı değildir. `scripts/audit-provider-models.mjs` açık listeleri tekrar karşılaştırır.

Kimlik doğrulaması isteyen `/models` yanıtları, modellerin emekli olduğunu kanıtlamaz. Listelerdeki ses/görüntü/embedding kayıtları nedeniyle remote toplamıyla konuşma katalogu toplamı farklı olabilir.

Bu rapor **1 Ekim 2026 tarihinde yakalanan kaynakların** durumunu kaydeder. Sonradan değişen katalog, hesap yetkisi, bölge veya plan kotası için canlı hesap doğrulaması gerekir. Kullanıcının önceden sabitlediği model kimlikleri otomatik yeniden yazılmaz.
