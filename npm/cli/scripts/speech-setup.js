'use strict';

const { spawnSync } = require('child_process');

// Use the installed binary's embedded setup, never a second downloaded script.
// Failure only affects dictation; the coding CLI remains usable.
function setupWindowsSpeech(binary, options = {}) {
  const platform = options.platform || process.platform;
  const env = options.env || process.env;
  const spawn = options.spawn || spawnSync;
  const log = options.log || console;
  if (platform !== 'win32' || env.ATLAS_AGENT_SKIP_SPEECH_SETUP === '1') return;
  const backend = env.ATLAS_AGENT_VOICE_BACKEND === 'windows' ? 'setup' : 'install';
  const args = ['voice', backend, '--automatic'];
  if (env.ATLAS_AGENT_VOICE_LANGUAGE) {
    args.push('--language', env.ATLAS_AGENT_VOICE_LANGUAGE);
  }
  log.log(backend === 'setup'
    ? 'Atlas Agent: checking optional Windows speech components. Windows may request administrator approval.'
    : 'Atlas Agent: installing verified offline Turkish, English, Italian and French Vosk models.');
  const result = spawn(binary, args, {
    stdio: 'inherit', windowsHide: true, shell: false, env,
    timeout: 17 * 60 * 1000,
  });
  if (result.error || result.status !== 0) {
    log.warn(`Atlas Agent is installed. Offline dictation setup did not finish; retry atlas-agent voice ${backend} later.`);
  }
}

module.exports = { setupWindowsSpeech };
