// Primary-source corrections for MiniMax regional pricing and invite-only Claude.
import fs from 'node:fs';
import crypto from 'node:crypto';
const root = 'internal/deps/atlas-models/internal/providers/configs/';
const evidence = { checked_at_utc: new Date().toISOString(), sources: [], changes: [] };
async function get(url, terms = []) {
  const response = await fetch(url, { signal: AbortSignal.timeout(30000) });
  if (!response.ok) throw new Error(`${url}: ${response.status}`);
  const text = await response.text();
  for (const term of terms) if (!text.includes(term)) throw new Error(`${url}: missing ${term}`);
  evidence.sources.push({ url, sha256: crypto.createHash('sha256').update(text).digest('hex') });
  return text;
}
const prices = await get('https://platform.minimax.io/docs/guides/pricing-paygo.md', ['Permanent 50% off', 'MiniMax-M2.7']);
const cnPrices = await get('https://platform.minimaxi.com/docs/guides/pricing-paygo.md', ['MiniMax-M3', '2.625']);
await get('https://platform.minimax.io/docs/api-reference/text-anthropic-api.md', ['Thinking is off by default for `MiniMax-M3`', 'adaptive']);
await get('https://platform.minimax.io/docs/api-reference/text-chat-anthropic.md', ['131072', '524288']);
await get('https://platform.minimax.io/docs/guides/pricing-token-plan-team.md', ['September 5, 2026', 'Subscription Key']);
const fx = await get('https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml', ["currency='USD'", "currency='CNY'"]);
const usd = Number(fx.match(/currency='USD' rate='([^']+)'/)[1]);
const cny = Number(fx.match(/currency='CNY' rate='([^']+)'/)[1]);
evidence.cny_to_usd = usd / cny;
evidence.fx_date = fx.match(/Cube time='([^']+)'/)[1];
const mythos = await get('https://platform.claude.com/docs/en/models/mythos-5-1/overview', ['claude-mythos-5-1', '128K', 'invitation only']);
await get('https://platform.claude.com/docs/en/build-with-claude/claude-on-vertex-ai', ['claude-mythos-5-1']);
const pending = new Map();
const read = name => JSON.parse(fs.readFileSync(root + name + '.json', 'utf8'));
const stage = (name, provider) => {
  evidence.changes.push({ id: provider.id, models: provider.models.length, default_large_model_id: provider.default_large_model_id });
  pending.set(root + name + '.json', JSON.stringify(provider, null, 2) + '\n');
};
for (const name of ['minimax', 'minimax-china']) {
  const p = read(name);
  const china = name === 'minimax-china';
  const rate = china ? usd / cny : 1;
  const rows = china ? cnPrices : prices;
  for (const m of p.models) {
    if (!rows.includes(`**${m.id}**`)) throw new Error(`No official price for ${m.id}`);
    let values;
    if (m.id === 'MiniMax-M3') {
      // Standard short-context tier; long context and priority have separate rates.
      values = china ? [2.1, 8.4, 0, .42] : [.3, 1.2, 0, .06];
      m.context_window = 1000000;
      m.default_max_tokens = 131072;
      m.supports_attachments = true;
    } else {
      const row = rows.split('\n').find(l => l.includes(`**${m.id}**`));
      const amounts = row.split('|').slice(2, 6).map(cell => Number(cell.match(/[0-9]+(?:\.[0-9]+)?/)[0]));
      if (amounts.some(x => !Number.isFinite(x))) throw new Error(`Invalid prices for ${m.id}`);
      values = [amounts[0], amounts[1], amounts[3], amounts[2]];
    }
    for (const [i, field] of ['cost_per_1m_in', 'cost_per_1m_out', 'cost_per_1m_in_cached', 'cost_per_1m_out_cached'].entries()) m[field] = Number((values[i] * rate).toFixed(8));
    m.can_reason = true;
  }
  p.default_large_model_id = 'MiniMax-M3';
  p.default_small_model_id = 'MiniMax-M3';
  stage(name, p);
}
const plan = read('minimax-coding');
for (const m of plan.models) {
  m.can_reason = true;
  if (m.id === 'MiniMax-M3') m.default_max_tokens = 131072;
}
stage('minimax-coding', plan);
const anthropic = read('anthropic');
const fable = anthropic.models.find(m => m.id === 'claude-fable-5-1');
if (!fable || !mythos.includes('shares Claude Fable')) throw new Error('Mythos specifications unverified');
const newest = { ...structuredClone(fable), id: 'claude-mythos-5-1', name: 'Claude Mythos 5.1 (Invite Only)' };
anthropic.models = [newest, ...anthropic.models.filter(m => m.id !== newest.id)];
for (const m of anthropic.models) {
  if (m.id === 'claude-mythos-5') m.name = 'Claude Mythos 5 (Invite Only)';
  for (const field of ['cost_per_1m_in', 'cost_per_1m_out', 'cost_per_1m_in_cached', 'cost_per_1m_out_cached']) m[field] = Number(m[field].toFixed(8));
}
stage('anthropic', anthropic);
const vertex = read('vertexai');
vertex.models = [structuredClone(newest), ...vertex.models.filter(m => m.id !== newest.id)];
stage('vertexai', vertex);
const router = read('openrouter');
if (!router.models.some(m => m.id === 'anthropic/claude-sonnet-5.5')) throw new Error('Verified router default missing');
router.default_large_model_id = 'anthropic/claude-sonnet-5.5';
stage('openrouter', router);
for (const [file, content] of pending) fs.writeFileSync(file, content);
fs.writeFileSync('docs/provider-native-catalog-evidence-2026-10-01.json', JSON.stringify(evidence, null, 2) + '\n');
console.log(JSON.stringify(evidence.changes));
