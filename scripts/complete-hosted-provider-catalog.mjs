// Hosted model IDs are checked against primary catalogs before staged writes.
import fs from 'node:fs';
import crypto from 'node:crypto';
const root = 'internal/deps/atlas-models/internal/providers/configs/';
const evidence = { checked_at_utc: new Date().toISOString(), sources: [], changes: [] };
async function get(url) {
  const response = await fetch(url, { signal: AbortSignal.timeout(30000) });
  if (!response.ok) throw new Error(`${url}: ${response.status}`);
  const body = await response.text();
  evidence.sources.push({ url, sha256: crypto.createHash('sha256').update(body).digest('hex') });
  return body;
}
const metadata = JSON.parse(await get('https://models.dev/api.json'));
const pending = new Map();
function convert(m) {
  if (!m?.limit?.context || !m.limit.output) throw new Error(`Missing metadata ${m?.id}`);
  const result = { id: m.id, name: m.name, cost_per_1m_in: m.cost?.input || 0, cost_per_1m_out: m.cost?.output || 0,
    cost_per_1m_in_cached: m.cost?.cache_write || 0, cost_per_1m_out_cached: m.cost?.cache_read || 0,
    context_window: m.limit.context, default_max_tokens: m.limit.output, can_reason: !!m.reasoning,
    supports_attachments: !!m.modalities?.input?.includes('image') };
  const levels = m.reasoning_options?.find(x => x.type === 'effort')?.values;
  if (levels?.length) { result.reasoning_levels = levels; result.default_reasoning_effort = levels.includes('high') ? 'high' : levels[0]; }
  return result;
}
function stage(id, models, large, small) {
  const p = JSON.parse(fs.readFileSync(root + id + '.json'));
  const ids = new Set(models.map(m => m.id));
  if (ids.size !== models.length || !ids.has(large) || !ids.has(small)) throw new Error(`Invalid defaults or duplicate models: ${id}`);
  for (const m of models) if (m.default_max_tokens > m.context_window || m.default_max_tokens <= 0) throw new Error(`Invalid output ${m.id}`);
  evidence.changes.push({ provider: id, before: p.models.length, after: models.length,
    added: models.filter(m => !p.models.some(old => old.id === m.id)).map(m => m.id), removed: p.models.filter(m => !ids.has(m.id)).map(m => m.id) });
  Object.assign(p, { models, default_large_model_id: large, default_small_model_id: small });
  pending.set(root + id + '.json', JSON.stringify(p, null, 2) + '\n');
}
await get('https://inference-docs.cerebras.ai/models/overview.md');
const cerebrasCard = await get('https://inference-docs.cerebras.ai/models/qwen-3.8-27b.md');
if (!cerebrasCard.includes('modelId="qwen-3.8-27b"') || !cerebrasCard.includes('$0.99')) throw new Error('Cerebras card changed');
const cm = Object.values(metadata.cerebras.models).map(convert);
const qwen = cm.find(m => m.id === 'qwen-3.8-27b');
// Free-tier limits work on both Free Trial and Pay as You Go.
Object.assign(qwen, { context_window: 64000, default_max_tokens: 32000, cost_per_1m_in: .99, cost_per_1m_out: 1.49 });
stage('cerebras', cm, 'gpt-oss-120b', 'qwen-3.8-27b');

const basetenDoc = await get('https://docs.baseten.co/inference/model-apis/overview.md');
const basetenPrices = await get('https://www.baseten.co/pricing');
const basetenPriceText = basetenPrices.replace(/<[^>]*>/g, ' ').replace(/\s+/g, ' ').split('Price per 1M tokens')[1];
if (!basetenPriceText) throw new Error('Baseten price table missing');
const rows = [...basetenDoc.matchAll(/model: "([^"]+)",\s+slug: "([^"]+)",\s+context: (\d+),\s+maxOutput: (\d+)/g)];
if (rows.length < 10) throw new Error('Baseten serving table missing');
const bm = rows.map(([, name, id, context, output]) => {
  const aliases = { 'DeepSeek V4 Flash 0731': 'DeepSeek-V4-Flash-0731', 'GLM 5.3 Flash': 'GLM-5.3-Flash', 'Nemotron Ultra': 'NVIDIA Nemotron 3 Ultra', 'OpenAI GPT 120B': 'GPT OSS 120B' };
  const label = aliases[name] || name.replace(/^GLM /, 'GLM-');
  const offset = basetenPriceText.indexOf(`${label} ${label} $`);
  if (offset < 0) throw new Error(`Baseten missing price evidence ${id}`);
  const rates = [...basetenPriceText.slice(offset, offset + 300).matchAll(/\$(\d+(?:\.\d+)?)/g)].map(x => Number(x[1]));
  if (rates.length < 5) throw new Error(`Baseten missing token rates ${id}`);
  const m = convert(metadata.baseten.models[id]);
  Object.assign(m, { cost_per_1m_in: rates[0], cost_per_1m_in_cached: 0, cost_per_1m_out_cached: rates[2], cost_per_1m_out: rates[4] });
  m.context_window = Math.min(m.context_window, Number(context) * 1000);
  m.default_max_tokens = Math.min(m.default_max_tokens, Number(output) * 1000, m.context_window);
  return m;
});
stage('baseten', bm, 'zai-org/GLM-5.3', 'zai-org/GLM-5.3-Flash');

const fireworks = await get('https://docs.fireworks.ai/serverless/pricing.md');
const fireworksModes = await get('https://docs.fireworks.ai/serverless/serverless-modes.md');
const fm = Object.values(metadata['fireworks-ai'].models).filter(m => m.tool_call && m.modalities?.output?.includes('text') &&
  (m.id.includes('/routers/') ? fireworksModes.includes(m.id) : fireworks.includes(m.id.split('/').at(-1)))).map(convert);
for (const m of fm) {
  const slug = m.id.split('/').at(-1);
  const line = fireworks.split('\n').find(line => line.includes(`/fireworks/${slug})`) && !line.includes('(US)') && !line.includes('Fast]'));
  const rates = line ? [...line.matchAll(/\\?\$(\d+(?:\.\d+)?)/g)].map(x => Number(x[1])) : [];
  if (rates.length >= 3) Object.assign(m, { cost_per_1m_in: rates[0], cost_per_1m_out_cached: rates[1], cost_per_1m_out: rates[2] });
}
stage('fireworks', fm, 'accounts/fireworks/models/kimi-k3', 'accounts/fireworks/models/glm-5p3-flash');

const nebius = JSON.parse(await get('https://tokenfactory.nebius.com/api/public/models_info'));
const nm = [];
for (const row of nebius) {
  if (row.status !== 'active' || !['text2text', 'image2text'].includes(row.type)) continue;
  for (const flavor of row.flavors || []) {
    const meta = metadata.nebius.models[flavor.model_id];
    if (!meta?.tool_call) continue;
    const m = convert(meta);
    Object.assign(m, { context_window: flavor.max_model_len || flavor.context_window_k * 1000,
      cost_per_1m_in: flavor.input_price_per_million_tokens, cost_per_1m_out: flavor.output_price_per_million_tokens,
      supports_attachments: row.type === 'image2text' });
    m.default_max_tokens = Math.min(m.default_max_tokens, m.context_window);
    nm.push(m);
  }
}
stage('nebius', nm, 'moonshotai/Kimi-K3', 'Qwen/Qwen3.8-27B');

await get('https://www.scaleway.com/en/docs/generative-apis/reference-content/supported-models/');
const scalePage = await get('https://www.scaleway.com/en/generative-apis/');
const scaleData = JSON.parse(scalePage.match(/<script id="__NEXT_DATA__" type="application\/json">([\s\S]*?)<\/script>/)[1]);
const fx = await get('https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml');
const usd = Number(fx.match(/currency=['"]USD['"]\s+rate=['"]([\d.]+)['"]/)[1]);
evidence.eur_to_usd = { rate: usd, date: fx.match(/time=['"]([^'"]+)['"]/)[1] };
const scaleModels = new Map();
function walk(value) {
  if (!value || typeof value !== 'object') return;
  if (value.apiId && value.toolCallingSupported && value.supportedApis?.includes('/v1/chat/completions') && ['general_availability', 'preview'].includes(value.status)) scaleModels.set(value.apiId, value);
  for (const child of Object.values(value)) walk(child);
}
walk(scaleData);
function usdPrice(price) {
  const value = price?.perMillionTokens?.value;
  if (!value || value.currencyCode !== 'EUR') throw new Error('Scaleway missing EUR token price');
  return Number(((Number(value.units) + Number(value.nanos) / 1e9) * usd).toFixed(6));
}
const sm = [...scaleModels.values()].map(m => {
  const region = m.regions.find(r => r.region === 'fr-par');
  if (!region) throw new Error(`Scaleway missing fr-par ${m.apiId}`);
  const result = { id: m.apiId, name: m.name, cost_per_1m_in: usdPrice(region.inputTokenPrice), cost_per_1m_out: usdPrice(region.outputTokenPrice),
    cost_per_1m_in_cached: 0, cost_per_1m_out_cached: region.inputCachedTokenPrice?.perMillionTokens ? usdPrice(region.inputCachedTokenPrice) : 0,
    context_window: m.contextWindow, default_max_tokens: m.maxOutputTokens, can_reason: m.reasoning,
    supports_attachments: m.tasks.includes('vision') };
  if (m.supportedReasoningValues?.length) { result.reasoning_levels = m.supportedReasoningValues; result.default_reasoning_effort = m.defaultReasoningValue; }
  return result;
});
stage('scaleway', sm, 'qwen3.5-397b-a17b', 'qwen3.8-27b');

for (const [file, body] of pending) fs.writeFileSync(file, body);
fs.writeFileSync('docs/provider-hosted-catalog-evidence-2026-10-01.json', JSON.stringify(evidence, null, 2) + '\n');
console.log(JSON.stringify(evidence.changes, null, 2));
