'use strict';

const assert = require('assert').strict;
const { setupWindowsSpeech } = require('./speech-setup');

let calls = [];
let warnings = [];
const fixture = {
  platform: 'win32', env: {},
  spawn: (...args) => { calls.push(args); return { status: 0 }; },
  log: { log: () => {}, warn: (message) => warnings.push(message) },
};
setupWindowsSpeech('C:\\Users\\Ömer&Ceylin\\atlas.exe', fixture);
assert.equal(calls.length, 1);
assert.equal(calls[0][0], 'C:\\Users\\Ömer&Ceylin\\atlas.exe');
assert.deepEqual(calls[0][1], ['voice', 'install', '--automatic']);
assert.equal(calls[0][2].shell, false);
assert.equal(calls[0][2].windowsHide, true);
setupWindowsSpeech('atlas.exe', { ...fixture, platform: 'linux' });
setupWindowsSpeech('atlas.exe', { ...fixture, env: { ATLAS_AGENT_SKIP_SPEECH_SETUP: '1' } });
assert.equal(calls.length, 1);
setupWindowsSpeech('atlas.exe', { ...fixture, env: { ATLAS_AGENT_VOICE_LANGUAGE: 'tr-TR' } });
assert.deepEqual(calls[1][1], ['voice', 'install', '--automatic', '--language', 'tr-TR']);
setupWindowsSpeech('atlas.exe', { ...fixture, env: { ATLAS_AGENT_VOICE_BACKEND: 'windows' } });
assert.deepEqual(calls[2][1], ['voice', 'setup', '--automatic']);
setupWindowsSpeech('atlas.exe', { ...fixture, spawn: () => ({ status: 1 }) });
setupWindowsSpeech('atlas.exe', { ...fixture, spawn: () => ({ error: new Error('timed out') }) });
assert.equal(warnings.length, 2);
console.log('Windows speech installer checks passed.');
