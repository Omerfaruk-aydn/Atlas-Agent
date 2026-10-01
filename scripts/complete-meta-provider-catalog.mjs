// Meta's direct API has separate billing from the Muse Code account adapter.
import fs from 'node:fs';
import crypto from 'node:crypto';
const evidence = { checked_at_utc: new Date().toISOString(), sources: [] };
async function get(url, terms = []) {
  const response = await fetch(url, { signal: AbortSignal.timeout(30000) });
  if (!response.ok) throw new Error(`${url}: ${response.status}`);
  const body = await response.text();
  for (const term of terms) if (!body.includes(term)) throw new Error(`${url}: missing ${term}`);
  evidence.sources.push({ url, sha256: crypto.createHash('sha256').update(body).digest('hex') });
  return body;
}
const docs = await get('https://dev.meta.ai/docs/models', ['muse-spark-1.3', 'https://api.meta.ai/v1']);
await get('https://dev.meta.ai/docs/pricing-rate-limits', ['$1.25', '$4.25', '$0.002']);
await get('https://dev.meta.ai/docs/reasoning', ['minimal', 'xhigh', 'Contributor']);
await get('https://dev.meta.ai/docs/protocols/responses', ['/v1/responses']);
const upstream = JSON.parse(await get('https://models.dev/api.json'));
const models = Object.values(upstream.meta.models).filter(m => docs.includes(m.id) && m.tool_call).map(m => {
  const contributor = m.id.endsWith('-contributor');
  return { id: m.id, name: m.name, cost_per_1m_in: contributor ? .1 : 1.25, cost_per_1m_out: contributor ? .2 : 4.25,
    cost_per_1m_in_cached: 0, cost_per_1m_out_cached: contributor ? .002 : .15,
    context_window: 1048576, default_max_tokens: Math.min(m.limit.output, 131072), can_reason: true, supports_attachments: true,
    reasoning_levels: ['minimal', 'low', 'medium', 'high', 'xhigh', ...(m.id === 'muse-spark-1.3' ? ['max'] : [])],
    default_reasoning_effort: 'medium' };
});
if (models.length !== 5) throw new Error('Expected five verified Muse Spark models');
const provider = { id: 'meta-api', name: 'Meta Model API', type: 'openai', api_key: '$MODEL_API_KEY', api_endpoint: 'https://api.meta.ai/v1',
  default_large_model_id: 'muse-spark-1.3', default_small_model_id: 'muse-spark-1.3', models };
fs.writeFileSync('internal/deps/atlas-models/internal/providers/configs/meta-api.json', JSON.stringify(provider, null, 2) + '\n');
fs.writeFileSync('docs/provider-meta-catalog-evidence-2026-10-01.json', JSON.stringify(evidence, null, 2) + '\n');
console.log(`Meta Model API: ${models.length} models; Standard defaults preserve the user's data policy.`);
