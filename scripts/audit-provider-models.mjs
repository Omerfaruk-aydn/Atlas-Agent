// Compare the embedded catalog with unauthenticated public model endpoints.
// No credentials are read and no inference requests are made.
import fs from 'node:fs';
import path from 'node:path';

const directory = 'internal/deps/atlas-models/internal/providers/configs';
const providers = fs.readdirSync(directory).filter(name => name.endsWith('.json'))
  .map(name => ({ file: name, ...JSON.parse(fs.readFileSync(path.join(directory, name), 'utf8')) }));
const overrides = {
  openrouter: 'https://openrouter.ai/api/v1/models',
  vercel: 'https://ai-gateway.vercel.sh/v1/models',
  openai: 'https://api.openai.com/v1/models',
  anthropic: 'https://api.anthropic.com/v1/models',
  gemini: 'https://generativelanguage.googleapis.com/v1beta/models',
};
const results = [];
const unavailable = new Set(['amp', 'bolt', 'phind', 'codex-ide', 'grok-web', 'windsurf', 'jetbrains', 'augment', 'factory', 'coderabbit', 'zed']);
for (let start = 0; start < providers.length; start += 6) {
  await Promise.all(providers.slice(start, start + 6).map(async provider => {
    const local = provider.models.map(model => model.id);
    const row = {
      provider: provider.id, file: provider.file, local_count: local.length,
      default_large: provider.default_large_model_id,
      default_small: provider.default_small_model_id,
      missing_local_defaults: [provider.default_large_model_id, provider.default_small_model_id]
        .filter(id => !local.includes(id)),
      duplicate_local_ids: [...new Set(local.filter((id, index) => local.indexOf(id) !== index))],
    };
    const base = provider.api_endpoint;
    const url = overrides[provider.id] ||
      (['openai-compat', 'openai', 'openrouter', 'vercel'].includes(provider.type) &&
        typeof base === 'string' && base.startsWith('https://') && !base.includes('$')
        ? base.replace(/\/$/, '') + '/models' : null);
    if (unavailable.has(provider.id)) {
      row.status = 'unavailable_in_atlas';
    } else if (!url) {
      row.status = 'requires_provider_documentation_or_account';
    } else {
      row.url = url;
      try {
        const response = await fetch(url, { signal: AbortSignal.timeout(15000) });
        row.http_status = response.status;
        if (!response.ok) row.status = 'not_publicly_verified';
        else {
          const body = await response.json();
          const data = Array.isArray(body) ? body : body.data || body.models;
          if (!Array.isArray(data) || !data.length || data.some(model => !model.id && !model.name)) {
            row.status = 'unrecognized_public_response';
          } else {
            const remote = provider.id === 'huggingface'
              ? data.flatMap(model => (model.providers || []).filter(route => route.status === 'live' && route.supports_tools)
                .map(route => `${model.id}:${route.provider}`))
              : data.map(model => model.id || model.name.replace(/^models\//, ''));
            row.status = 'public_model_list_compared';
            row.remote_count = remote.length;
            row.remote_ids = remote;
            row.local_not_in_public_list = local.filter(id => !remote.includes(id));
            row.public_not_in_local_catalog = remote.filter(id => !local.includes(id));
            row.defaults_not_in_public_list = [row.default_large, row.default_small]
              .filter(id => !remote.includes(id));
            row.text_tool_models_missing = data.filter(model => {
              const hasTools = model.supported_parameters?.includes('tools');
              const textOutput = model.architecture?.output_modalities?.includes('text');
              return hasTools && textOutput && !local.includes(model.id);
            }).map(model => model.id);
          }
        }
      } catch (error) {
        row.status = 'not_publicly_verified';
        row.error = error.name;
      }
    }
    results.push(row);
  }));
}
results.sort((a, b) => a.provider.localeCompare(b.provider));
const output = {
  checked_at_utc: new Date().toISOString(),
  scope: 'Public GET model lists only; absence does not prove retirement; metadata and account entitlement are not verified.',
  providers: results,
};
fs.writeFileSync(process.argv[2] || 'docs/provider-catalog-audit-2026-10-01.json', JSON.stringify(output, null, 2) + '\n');
for (const row of results) {
  console.log(JSON.stringify({
    provider: row.provider, status: row.status, http: row.http_status,
    local: row.local_count, remote: row.remote_count,
    absent: row.local_not_in_public_list?.length,
    missing: row.public_not_in_local_catalog?.length,
    unavailable_defaults: row.defaults_not_in_public_list,
    invalid_local_defaults: row.missing_local_defaults,
  }));
}
