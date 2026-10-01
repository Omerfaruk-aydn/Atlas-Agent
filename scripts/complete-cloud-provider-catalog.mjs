// Primary documentation gates all writes. No credentials or inference calls.
import fs from 'node:fs';
import crypto from 'node:crypto';
const root = 'internal/deps/atlas-models/internal/providers/configs/';
const evidence = { checked_at_utc: new Date().toISOString(), sources: [], changes: [], notes: [
  'Static prices use Standard/global short-context rates. Regional and long-context billing can differ.',
  'Vertex Gemini 3.6/3.7/3.8 Flash promotional rates expire on 2026-12-31.',
  'Availability in a published catalog does not grant account entitlement or regional deployment access.',
] };
async function source(url, required = []) {
  const response = await fetch(url, { signal: AbortSignal.timeout(30000) });
  if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
  const body = await response.text();
  for (const term of required) if (!body.includes(term)) throw new Error(`${url}: missing ${term}`);
  evidence.sources.push({ url, sha256: crypto.createHash('sha256').update(body).digest('hex') });
  return body;
}
const upstream = JSON.parse(await source('https://models.dev/api.json'));
const read = name => JSON.parse(fs.readFileSync(root + name + '.json', 'utf8'));
const pending = new Map();
function model(m, id = m.id) {
  if (!m?.limit?.context || !m.limit.output) throw new Error(`Missing metadata: ${id}`);
  const result = { id, name: m.name, cost_per_1m_in: m.cost?.input || 0, cost_per_1m_out: m.cost?.output || 0,
    cost_per_1m_in_cached: m.cost?.cache_write || 0, cost_per_1m_out_cached: m.cost?.cache_read || 0,
    context_window: m.limit.context, default_max_tokens: m.limit.output, can_reason: !!m.reasoning,
    supports_attachments: !!m.modalities?.input?.includes('image') };
  const levels = m.reasoning_options?.find(x => x.type === 'effort')?.values;
  if (levels?.length) { result.reasoning_levels = levels; result.default_reasoning_effort = levels.includes('medium') ? 'medium' : levels[0]; }
  return result;
}
function price(m, input, output, write = 0, read = 0) {
  Object.assign(m, { cost_per_1m_in: input, cost_per_1m_out: output, cost_per_1m_in_cached: write, cost_per_1m_out_cached: read });
  return m;
}
function stage(file, p, additions, large, small, remove = []) {
  const old = p.models;
  const fresh = new Map(additions.map(m => [m.id, m]));
  p.models = [...additions, ...old.filter(m => !fresh.has(m.id) && !remove.includes(m.id))];
  p.default_large_model_id = large; p.default_small_model_id = small;
  const ids = new Set();
  for (const m of p.models) {
    if (ids.has(m.id) || m.context_window <= 0 || m.default_max_tokens <= 0 || m.default_max_tokens > m.context_window) throw new Error(`Invalid ${p.id}/${m.id}`);
    ids.add(m.id);
  }
  for (const id of [large, small]) if (!ids.has(id)) throw new Error(`Missing default ${p.id}/${id}`);
  evidence.changes.push({ provider: p.id, before: old.length, after: p.models.length,
    added: p.models.filter(m => !old.some(x => x.id === m.id)).map(m => m.id), removed: old.filter(m => !ids.has(m.id)).map(m => m.id) });
  pending.set(root + file + '.json', JSON.stringify(p, null, 2) + '\n');
}

const azureDocs = await source('https://learn.microsoft.com/en-us/azure/foundry/foundry-models/concepts/models-sold-directly-by-azure', ['gpt-6.1-sol', 'gpt-6-luna']);
const azurePrices = await source('https://azure.microsoft.com/en-us/pricing/details/cognitive-services/openai-service/', ['GPT-6 Sol', 'GPT-5.6 Sol']);
await source('https://techcommunity.microsoft.com/blog/azure-ai-foundry-blog/introducing-gpt-6-1-sol-in-microsoft-foundry-advanced-intelligence-optimized-for/4560811', ['GPT-6.1']);
const azureNew = Object.values(upstream.azure.models).filter(m => /^(gpt-|o[134])/.test(m.id) && m.tool_call && m.modalities?.output?.includes('text') && azureDocs.includes(m.id)).map(m => model(m));
// Published Global Standard short-context rates; Microsoft launch post supplies 6.1.
const azureRates = {
  'gpt-6.1-sol': [2, 10, 2.5, .1], 'gpt-6-astra': [10, 50, 12.5, 1],
  'gpt-6-sol': [2, 10, 2.5, .2], 'gpt-6-luna': [.1, .5, .125, .01],
  'gpt-5.6-sol': [4, 20, 5, .4], 'gpt-5.6-terra': [2, 12, 2.5, .2], 'gpt-5.6-luna': [.2, 1.2, .25, .02],
};
for (const m of azureNew) if (azureRates[m.id]) { price(m, ...azureRates[m.id]); m.context_window = 1050000; m.default_max_tokens = 128000; m.reasoning_levels = ['none', 'low', 'medium', 'high', 'xhigh', 'max']; }
stage('azure', read('azure'), azureNew, 'gpt-6.1-sol', 'gpt-6-luna', ['gpt-4.5-preview']);

const claudeDocs = await source('https://platform.claude.com/docs/en/build-with-claude/claude-on-vertex-ai', ['claude-fable-5-1', 'claude-opus-5-5', 'claude-sonnet-5-5']);
await source('https://platform.claude.com/docs/en/about-claude/pricing', ['Fable 5.1', 'Opus 5.5', 'Sonnet 5.5']);
let vertexDocs = await source('https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/google-models', ['3.8 Flash']);
vertexDocs += await source('https://docs.cloud.google.com/gemini-enterprise-agent-platform/models/guides/gemini-3-8-flash', ['gemini-3.8-flash']);
await source('https://cloud.google.com/vertex-ai/generative-ai/pricing', ['Gemini 3.8 Flash']);
const vm = Object.values(upstream['google-vertex'].models).filter(m => m.id.startsWith('gemini-') && (vertexDocs.includes(m.id) || vertexDocs.includes(m.name)) && m.tool_call && m.modalities?.output?.includes('text')).map(m => model(m));
for (const m of vm) if (/gemini-3\.[678]-flash$/.test(m.id)) { price(m, .75, 3.75, 0, .075); m.default_reasoning_effort = 'medium'; }
const directClaude = read('anthropic').models;
const vertexClaudeIDs = new Set(['claude-fable-5-1', 'claude-fable-5', 'claude-opus-5-5', 'claude-opus-5', 'claude-opus-4-8', 'claude-opus-4-7', 'claude-opus-4-6', 'claude-sonnet-5-5', 'claude-sonnet-5', 'claude-sonnet-4-6']);
for (const m of directClaude) {
  if (!vertexClaudeIDs.has(m.id) || !claudeDocs.includes(m.id)) continue;
  vm.push(structuredClone(m));
}
for (const id of ['claude-opus-4-5@20251101', 'claude-haiku-4-5@20251001']) if (claudeDocs.includes(id)) vm.push(model(upstream['google-vertex'].models[id]));
stage('vertexai', read('vertexai'), vm, 'gemini-3.1-pro-preview', 'gemini-3.8-flash', ['gemini-3-pro-preview', 'gemini-3.1-pro-preview-customtools', 'claude-opus-4-1-20250805', 'claude-opus-4-20250514', 'claude-sonnet-4-20250514']);

const families = ['opus-5-5', 'sonnet-5-5', 'fable-5-1'];
const cards = {};
for (const family of families) cards[family] = await source(`https://docs.aws.amazon.com/bedrock/latest/userguide/model-card-anthropic-claude-${family}.html`, [`global.anthropic.claude-${family}`]);
for (const [file, prefix] of [['bedrock-united-states', 'us'], ['bedrock-europe', 'eu']]) {
  const additions = [];
  for (const family of families) {
    const baseID = `claude-${family}`;
    const original = directClaude.find(m => m.id === baseID);
    if (!original) throw new Error(`Missing primary catalog model ${baseID}`);
    for (const region of [prefix, 'global']) {
      // Sonnet 5.5 has global routing only, even from US/EU source Regions.
      if (family === 'sonnet-5-5' && region !== 'global') continue;
      const id = `${region}.anthropic.${baseID}`;
      if (!cards[family].includes(id)) continue;
      const m = { ...structuredClone(original), id, name: original.name + (region === 'global' ? ' (Global)' : ` (${prefix.toUpperCase()})`) };
      if (region !== 'global') for (const field of ['cost_per_1m_in', 'cost_per_1m_out', 'cost_per_1m_in_cached', 'cost_per_1m_out_cached']) m[field] = Number((m[field] * 1.1).toFixed(6));
      additions.push(m);
    }
  }
  const p = read(file);
  stage(file, p, additions, `${prefix}.anthropic.claude-opus-5-5`, p.default_small_model_id, [`${prefix}.anthropic.claude-sonnet-5-5`]);
}

const awsModels = [];
for (const family of ['gpt-6.1-sol', 'gpt-6-astra', 'gpt-6-sol', 'gpt-6-luna']) {
  const card = await source(`https://docs.aws.amazon.com/bedrock/latest/userguide/model-card-openai-${family.replaceAll('.', '-')}.html`, [`us.openai.${family}`]);
  for (const prefix of ['us', 'global']) {
    // The first-party card explicitly states no global profile at launch.
    if (family === 'gpt-6.1-sol' && prefix === 'global') continue;
    const id = `${prefix}.openai.${family}`;
    if (!card.includes(id)) continue;
    const m = model(upstream.azure.models[family], id);
    m.name += prefix === 'global' ? ' (Global)' : ' (US)';
    m.default_max_tokens = 131072;
    m.context_window = 1000000;
    price(m, ...azureRates[family]);
    if (prefix === 'us') for (const field of ['cost_per_1m_in', 'cost_per_1m_out', 'cost_per_1m_in_cached', 'cost_per_1m_out_cached']) m[field] = Number((m[field] * 1.1).toFixed(6));
    awsModels.push(m);
  }
}
stage('bedrock-openai', { id: 'bedrock-openai', name: 'Amazon Bedrock OpenAI (API Key)', type: 'openai', api_key: '$AWS_BEDROCK_OPENAI_API_KEY', api_endpoint: 'https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1', models: [] }, awsModels, 'us.openai.gpt-6.1-sol', 'global.openai.gpt-6-luna');

const aliPrices = await source('https://www.alibabacloud.com/help/en/model-studio/model-pricing', ['qwen3.8-max', 'qwen3.8-flash']);
await source('https://www.alibabacloud.com/help/en/model-studio/models', ['qwen3.8-max', 'qwen3.8-flash']);
for (const file of ['alibaba-singapore', 'alibaba-united-states']) {
  const models = ['qwen3.8-max', 'qwen3.8-flash'].map(id => model(upstream.alibaba.models[id]));
  if (file === 'alibaba-united-states') {
    price(models[0], 1.65, 4.951); price(models[1], .113, .382);
  }
  stage(file, read(file), models, 'qwen3.8-max', 'qwen3.8-flash');
}

await source('https://www.antigravity.google/docs/models/', ['Gemini 3.8 Flash', 'Gemini 3.6 Flash', 'Gemini 3.1 Pro']);
const ag = read('antigravity');
const agModels = ['gemini-3.8-flash', 'gemini-3.7-flash', 'gemini-3.6-flash'].map(id => price(model(upstream['google-vertex'].models[id]), 0, 0));
const pro = structuredClone(ag.models.find(m => m.id === 'gemini-3.1-pro'));
pro.context_window = 1048576; pro.default_reasoning_effort = 'high'; agModels.push(pro);
stage('antigravity', ag, agModels, 'gemini-3.1-pro', 'gemini-3.8-flash', ['gemini-3.5-flash', 'gemini-3.5-flash-lite']);

for (const [file, data] of pending) fs.writeFileSync(file, data);
fs.writeFileSync('docs/provider-cloud-catalog-evidence-2026-10-01.json', JSON.stringify(evidence, null, 2) + '\n');
console.log(JSON.stringify(evidence.changes, null, 2));
