// Complete public catalogs using provider listings and published metadata.
// This script never reads credentials or sends inference requests.
import fs from 'node:fs';
import crypto from 'node:crypto';

const root = 'internal/deps/atlas-models/internal/providers/configs/';
const evidence = { checked_at_utc: new Date().toISOString(), sources: [], changes: [] };
const pending = new Map();
async function getText(url) {
  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      const response = await fetch(url, { signal: AbortSignal.timeout(20000) });
      if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
      const body = await response.text();
      evidence.sources.push({ url, sha256: crypto.createHash('sha256').update(body).digest('hex') });
      return body;
    } catch (error) {
      if (attempt === 2) throw error;
    }
  }
}
const get = async url => JSON.parse(await getText(url));
const upstream = await get('https://models.dev/api.json');
const read = id => JSON.parse(fs.readFileSync(root + id + '.json', 'utf8'));
function convert(m) {
  const result = {
    id: m.id, name: m.name || m.id,
    cost_per_1m_in: m.cost?.input ?? 0,
    cost_per_1m_out: m.cost?.output ?? 0,
    cost_per_1m_in_cached: m.cost?.cache_write ?? 0,
    cost_per_1m_out_cached: m.cost?.cache_read ?? 0,
    context_window: m.limit.context,
    default_max_tokens: m.limit.output || 4096,
    can_reason: !!m.reasoning,
    supports_attachments: !!m.modalities?.input?.includes('image'),
  };
  const levels = m.reasoning_options?.find(x => x.type === 'effort')?.values;
  if (levels?.length) {
    result.reasoning_levels = levels;
    result.default_reasoning_effort = levels.includes('high') ? 'high' : levels[0];
  }
  return result;
}
function write(p, models, large, small) {
  const ids = new Set();
  for (const m of models) {
    if (ids.has(m.id)) throw new Error(`${p.id}: duplicate ${m.id}`);
    ids.add(m.id);
    if (!(m.context_window > 0 && m.default_max_tokens > 0)) throw new Error(`${p.id}/${m.id}: missing limits`);
    for (const field of ['cost_per_1m_in', 'cost_per_1m_out', 'cost_per_1m_in_cached', 'cost_per_1m_out_cached']) {
      if (!Number.isFinite(m[field]) || m[field] < 0) throw new Error(`${p.id}/${m.id}: invalid ${field}`);
    }
  }
  for (const id of [large, small]) if (!ids.has(id)) throw new Error(`${p.id}: missing default ${id}`);
  evidence.changes.push({ provider: p.id, before: p.models.length, after: models.length,
    added: models.filter(m => !p.models.some(old => old.id === m.id)).map(m => m.id),
    removed: p.models.filter(m => !ids.has(m.id)).map(m => m.id) });
  p.models = models;
  p.default_large_model_id = large;
  p.default_small_model_id = small;
  pending.set(root + p.id + '.json', JSON.stringify(p, null, 2) + '\n');
}

for (const [id, key] of [['opencode-zen', 'opencode'], ['opencode-go', 'opencode-go']]) {
  const p = read(id);
  const remote = await get(p.api_endpoint + '/models');
  const available = new Set(remote.data.map(m => m.id));
  const old = new Map(p.models.map(m => [m.id, m]));
  const meta = { ...upstream.opencode.models, ...upstream[key].models };
  const aliases = { 'deepseek-flash': 'deepseek-v4-flash', 'hy3-preview': 'hy3', 'omen-alpha': 'omen-alpha' };
  const models = [];
  for (const modelID of available) {
    // System One returns typed decisions, rather than language-model text.
    if (modelID.startsWith('jev-')) continue;
    let m = meta[modelID] || meta[aliases[modelID]];
    if (!m && old.has(modelID)) { models.push(old.get(modelID)); continue; }
    if (!m) {
      evidence.excluded ??= [];
      evidence.excluded.push({ provider: id, model: modelID, reason: 'Provider publishes availability but no limits, pricing, or protocol metadata.' });
      continue;
    }
    if (!m.tool_call || !m.modalities?.output?.includes('text')) continue;
    models.push({ ...convert(m), id: modelID });
  }
  write(p, models, p.default_large_model_id, p.default_small_model_id);
}

const rich = await get('https://aihubmix.com/api/v1/models');
const active = new Set((await get('https://aihubmix.com/v1/models')).data.map(m => m.id));
const ai = read('aihubmix');
const models = [];
const canonical = Object.values(upstream).flatMap(p => Object.values(p.models));
for (const m of rich.data) {
  if (!active.has(m.model_id) || m.types !== 'llm' || !m.output_modalities?.split(',').includes('text')) continue;
  if (m.retire_stage && m.retire_stage !== 'active') continue;
  const meta = upstream.aihubmix.models[m.model_id] || canonical.find(x => x.id === m.model_id);
  const tools = m.tool_call === true || /tools|function_calling/.test(m.features || '') || meta?.tool_call;
  if (!tools || !m.context_length || !m.pricing) continue;
  models.push({
    ...(meta ? convert(meta) : {}), id: m.model_id, name: m.model_name,
    cost_per_1m_in: Number(m.pricing.input), cost_per_1m_out: Number(m.pricing.output),
    cost_per_1m_in_cached: Number(m.pricing.cache_write || 0),
    cost_per_1m_out_cached: Number(m.pricing.cache_read || 0),
    context_window: m.context_length, default_max_tokens: m.max_output || meta?.limit?.output || 4096,
    can_reason: m.reasoning ?? meta?.reasoning ?? /thinking/.test(m.features || ''),
    supports_attachments: m.input_modalities?.split(',').includes('image') || false,
  });
}
write(ai, models, 'gpt-6.1-sol', 'gpt-6-luna');

const cortecs = read('cortecs');
const eur = await fetch('https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml', { signal: AbortSignal.timeout(20000) });
if (!eur.ok) throw new Error('ECB reference rate unavailable');
const xml = await eur.text();
const rate = Number(xml.match(/currency='USD' rate='([^']+)'/)?.[1]);
const date = xml.match(/time='([^']+)'/)?.[1];
if (!(rate > 0) || !date) throw new Error('Invalid EUR/USD reference rate');
evidence.exchange_rate = { source: eur.url, date, eur_usd: rate };
const cm = (await get(cortecs.api_endpoint + '/models')).data
  .filter(m => m.supported_features?.includes('tools') && m.output_modalities?.includes('text'))
  .map(m => {
    const model = convert(upstream.cortecs.models[m.id]);
    const factor = m.pricing.currency === 'EUR' ? rate : m.pricing.currency === 'USD' ? 1 : NaN;
    return { ...model, name: m.id, context_window: m.context_size, default_max_tokens: m.max_output_tokens || 4096,
      cost_per_1m_in: m.pricing.input_token * factor,
      cost_per_1m_out: m.pricing.output_token * factor,
      cost_per_1m_in_cached: (m.pricing.cache_write_cost || 0) * factor,
      cost_per_1m_out_cached: (m.pricing.cache_read_cost || 0) * factor,
      supports_attachments: m.input_modalities.includes('image') };
  });
cortecs.type = 'openai-compat';
write(cortecs, cm, 'gpt-6.1-sol', 'gpt-6-luna');

// Bind Hugging Face entries to a specific serving provider so their limits
// and prices describe the actual route rather than an arbitrary router choice.
const hf = read('huggingface');
const hm = [];
for (const m of (await get(hf.api_endpoint + '/models')).data) {
  if (!m.architecture?.input_modalities?.includes('text') || !m.architecture?.output_modalities?.includes('text')) continue;
  for (const route of m.providers || []) {
    if (route.status !== 'live' || !route.supports_tools || !route.context_length || !route.pricing) continue;
    hm.push({ id: `${m.id}:${route.provider}`, name: `${m.id} (${route.provider})`,
      context_window: route.context_length, default_max_tokens: Math.min(4096, route.context_length),
      cost_per_1m_in: route.pricing.input, cost_per_1m_out: route.pricing.output,
      cost_per_1m_in_cached: 0, cost_per_1m_out_cached: 0,
      can_reason: false, supports_attachments: m.architecture.input_modalities.includes('image') });
  }
}
write(hf, hm, 'zai-org/GLM-5.3:baseten', 'deepseek-ai/DeepSeek-V4.1-Flash:baseten');

// Qiniu publishes prices, protocols, limits, and features in its server-rendered
// model gallery. Its USD unit prices avoid treating CNY prices as dollars.
const qiniu = read('qiniucloud');
const qserved = new Set((await get(qiniu.api_endpoint + '/models')).data.map(m => m.id));
const qpage = await getText('https://www.qiniu.com/ai/models');
const qdata = JSON.parse(qpage.match(/<script id="__NEXT_DATA__"[^>]*>([\s\S]*?)<\/script>/)?.[1] || '{}');
if (!Array.isArray(qdata.props?.pageProps?.models)) throw new Error('Qiniu gallery metadata unavailable');
const qm = [];
for (const m of qdata.props.pageProps.models) {
  if (!qserved.has(m.id) || m.private || !m.support_api_protocols?.includes('openai-chat')) continue;
  if (!m.architecture?.output_modalities?.includes('text')) continue;
  if (!(m.architecture.function_calling?.supported || m.features?.includes('工具调用'))) continue;
  if (m.retirement_at && new Date(m.retirement_at + 'T00:00:00+08:00') <= new Date()) continue;
  const details = (m.pricing_rules_v2 || []).flatMap(r => Object.entries(r.details_v2 || {}));
  const price = pattern => {
    const prices = details.filter(([k, v]) => pattern.test(k) && v.unit_name === 'token')
      .map(([, v]) => Number(v.unit_price_usd) * 1000000 / v.unit_size);
    return prices.length ? Math.max(...prices) : null;
  };
  const input = price(/^(input|ncache)(_|$)/), output = price(/^output(_|$)/);
  if (input === null || output === null || !m.model_constraints?.context_length) {
    evidence.excluded ??= [];
    evidence.excluded.push({ provider: 'qiniucloud', model: m.id, reason: 'No published USD token prices or context limit.' });
    continue;
  }
  qm.push({ id: m.id, name: m.name, context_window: m.model_constraints.context_length,
    default_max_tokens: m.model_constraints.max_default_completion_tokens ||
      Math.min(4096, m.model_constraints.max_tokens || m.model_constraints.context_length),
    cost_per_1m_in: input, cost_per_1m_out: output,
    cost_per_1m_in_cached: price(/^(cache_write|cache_creation)(_|$)/) || 0,
    cost_per_1m_out_cached: price(/^(cache|cache_read)(_|$)/) || 0,
    can_reason: !!(m.architecture.reasoning?.supported || m.features?.includes('深度思考')),
    supports_attachments: !!m.architecture.input_modalities?.includes('image') });
}
evidence.qiniu_cost_policy = 'Published USD token prices; highest published length/time tier used for conservative estimates.';
write(qiniu, qm, 'z-ai/glm-5.3', 'deepseek/deepseek-v4.1-flash');

// Subscription entitlements come from Qiniu's plan documentation, not from
// the pay-as-you-go model list. Keep its plan key separate from the API key.
const qplanIDs = ['z-ai/glm-4.6', 'z-ai/glm-4.7', 'z-ai/glm-5', 'z-ai/glm-5.1',
  'minimax/minimax-m2.5', 'minimax/minimax-m2.5-highspeed', 'minimax/minimax-m2.7', 'minimax/minimax-m3',
  'moonshotai/kimi-k2.5', 'moonshotai/kimi-k2.6', 'deepseek/deepseek-v3.2-251201',
  'deepseek/deepseek-v4-pro', 'deepseek/deepseek-v4-flash'];
const qplanDoc = await getText('https://developer.qiniu.com/aitokenapi/13330/subscription-introduction');
for (const id of qplanIDs) if (!qplanDoc.includes(id)) throw new Error(`Qiniu plan entitlement no longer published: ${id}`);
const qplan = { ...qiniu, id: 'qiniu-token-plan', name: 'Qiniu Token Plan', api_key: '$QINIU_TOKEN_PLAN_API_KEY', models: [] };
const qpm = qm.filter(m => qplanIDs.includes(m.id)).map(m => ({ ...m,
  cost_per_1m_in: 0, cost_per_1m_out: 0, cost_per_1m_in_cached: 0, cost_per_1m_out_cached: 0 }));
write(qplan, qpm, 'minimax/minimax-m3', 'moonshotai/kimi-k2.6');

const avian = read('avian');
const am = (await get(avian.api_endpoint + '/models')).data
  .filter(m => m.context_length && m.max_output && m.pricing)
  .map(m => ({ id: m.id, name: m.display_name || m.id, context_window: m.context_length,
    default_max_tokens: m.max_output, cost_per_1m_in: m.pricing.input_per_million,
    cost_per_1m_out: m.pricing.output_per_million, cost_per_1m_in_cached: 0,
    cost_per_1m_out_cached: m.pricing.cache_read_per_million || 0,
    can_reason: !!m.reasoning, supports_attachments: !!m.supports_vision }));
write(avian, am, 'xiaomi/mimo-v2.6-pro', 'xiaomi/mimo-v2.6-flash');

const nv = read('nvidia-nim');
const nvFlashCard = await getText('https://build.nvidia.com/z-ai/glm-5-3-flash/modelcard');
if (!nvFlashCard.includes('1,048,576') || !nvFlashCard.includes('defaults to')) throw new Error('NVIDIA GLM Flash metadata not verified');
const served = new Set((await get(nv.api_endpoint + '/models')).data.map(m => m.id));
const nm = Object.values(upstream.nvidia.models)
  .filter(m => served.has(m.id) && m.tool_call && m.modalities?.output?.includes('text'))
  .map(convert);
const nvFlash = nm.find(m => m.id === 'z-ai/glm-5.3-flash');
if (nvFlash) { nvFlash.context_window = 1048576; nvFlash.default_reasoning_effort = 'max'; }
if (served.has('deepseek-ai/deepseek-v4.1-flash')) {
  nm.push({ id: 'deepseek-ai/deepseek-v4.1-flash', name: 'DeepSeek V4.1 Flash',
    context_window: 1048576, default_max_tokens: 262144, can_reason: true, supports_attachments: true,
    cost_per_1m_in: 0, cost_per_1m_out: 0, cost_per_1m_in_cached: 0, cost_per_1m_out_cached: 0 });
}
write(nv, nm, 'deepseek-ai/deepseek-v4.1-flash', 'z-ai/glm-5.3-flash');

// Finish source reads and validation before changing any catalog files.
for (const [file, body] of pending) fs.writeFileSync(file, body);
fs.writeFileSync('docs/provider-catalog-completion-evidence-2026-10-01.json', JSON.stringify(evidence, null, 2) + '\n');
console.log(JSON.stringify(evidence.changes.map(({ provider, before, after }) => ({ provider, before, after }))));
