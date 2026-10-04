#!/usr/bin/env node
// Postinstall: download the platform-specific binary from GitHub Releases.
//
// Robustness upgrades vs the original:
//   * Retries transient network errors and 5xx responses (3 attempts, jittered
//     exponential backoff) so a flaky network doesn't break a fresh install.
//   * Treats 404 distinctly: surfaces a clear "release not found" error
//     instead of a generic 404, telling the user to upgrade the wrapper or
//     re-trigger the GitHub release workflow.
//   * Streams the response to disk (same as before) so a 100MB+ download
//     doesn't buffer in memory.

const fs = require('fs');
const path = require('path');
const https = require('https');
const { URL } = require('url');
const { setupWindowsSpeech } = require('./speech-setup');

const PLATFORM_MAP = { win32: 'windows', darwin: 'darwin', linux: 'linux' };
const platform = PLATFORM_MAP[process.platform] || process.platform;
const arch = process.arch;
const ext = process.platform === 'win32' ? '.exe' : '';
const assetName = `atlas-agent-${platform}-${arch}${ext}`;

const REPO = 'Omerfaruk-aydn/Atlas-Agent';
const VERSION = require('../package.json').version;
const TAG = `v${VERSION}`;

const MAX_ATTEMPTS = 3;
const BASE_DELAY_MS = 500;

const binDir = path.join(__dirname, '..', 'bin');
fs.mkdirSync(binDir, { recursive: true });
const dest = path.join(binDir, assetName);

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

function download(url, redirectsLeft, attempt) {
  if (redirectsLeft == null) redirectsLeft = 5;
  if (attempt == null) attempt = 1;
  return new Promise((resolve, reject) => {
    const req = https.get(url, { headers: { 'User-Agent': 'atlas-agent-installer' } }, (res) => {
      if ([301, 302, 303, 307, 308].includes(res.statusCode)) {
        if (redirectsLeft <= 0) return reject(new Error('Too many redirects for ' + url));
        const next = res.headers.location;
        if (!next) return reject(new Error('Redirect with no Location header'));
        res.resume();
        return resolve(download(new URL(next, url).toString(), redirectsLeft - 1, attempt));
      }
      if (res.statusCode === 404) {
        res.resume();
        return reject(new Error(
          `HTTP 404: release ${TAG} has no ${assetName} asset. ` +
            `The npm wrapper is at v${VERSION} but no matching GitHub release exists. ` +
            `Either upgrade the wrapper (npm i -g @atlas-coder/atlas-agent@latest) ` +
            `or re-run the Atlas Agent release workflow on GitHub.`
        ));
      }
      if (res.statusCode >= 500 && attempt < MAX_ATTEMPTS) {
        res.resume();
        const delay = BASE_DELAY_MS * Math.pow(2, attempt - 1) + Math.floor(Math.random() * 250);
        console.warn(
          `atlas-agent installer: HTTP ${res.statusCode} from ${url} ` +
            `(attempt ${attempt}/${MAX_ATTEMPTS}), retrying in ${delay}ms`
        );
        return sleep(delay).then(() => resolve(download(url, redirectsLeft, attempt + 1)));
      }
      if (res.statusCode !== 200) {
        res.resume();
        return reject(new Error('HTTP ' + res.statusCode + ' for ' + url));
      }
      const tmp = dest + '.part';
      const out = fs.createWriteStream(tmp);
      res.pipe(out);
      out.on('finish', () =>
        out.close(() => {
          try {
            fs.renameSync(tmp, dest);
            resolve();
          } catch (e) {
            reject(e);
          }
        })
      );
      out.on('error', reject);
      res.on('error', (err) => {
        // Connection-level errors (ECONNRESET, ETIMEDOUT, ENOTFOUND...) — retry.
        if (attempt < MAX_ATTEMPTS) {
          const delay = BASE_DELAY_MS * Math.pow(2, attempt - 1) + Math.floor(Math.random() * 250);
          console.warn(
            `atlas-agent installer: ${err.message} (attempt ${attempt}/${MAX_ATTEMPTS}), ` +
              `retrying in ${delay}ms`
          );
          sleep(delay).then(() => resolve(download(url, redirectsLeft, attempt + 1)));
        } else {
          reject(err);
        }
      });
    });
    req.on('error', (err) => {
      if (attempt < MAX_ATTEMPTS) {
        const delay = BASE_DELAY_MS * Math.pow(2, attempt - 1) + Math.floor(Math.random() * 250);
        console.warn(
          `atlas-agent installer: ${err.message} (attempt ${attempt}/${MAX_ATTEMPTS}), ` +
            `retrying in ${delay}ms`
        );
        sleep(delay).then(() => resolve(download(url, redirectsLeft, attempt + 1)));
      } else {
        reject(err);
      }
    });
    req.setTimeout(60000, () => {
      req.destroy(new Error('Download timeout (60s)'));
    });
  });
}

(async () => {
  const url = `https://github.com/${REPO}/releases/download/${TAG}/${assetName}`;
  console.log(`Atlas Agent postinstall: downloading ${assetName} from ${url}`);
  try {
    await download(url);
  } catch (err) {
    console.error('Atlas Agent postinstall FAILED: ' + err.message);
    console.error('URL: ' + url);
    console.error('You can manually download it from:');
    console.error('  https://github.com/' + REPO + '/releases/tag/' + TAG);
    console.error('and place it at:');
    console.error('  ' + dest);
    process.exit(1);
  }
  if (process.platform !== 'win32') {
    fs.chmodSync(dest, 0o755);
  }
  const size = (fs.statSync(dest).size / 1024 / 1024).toFixed(1);
  console.log(`Atlas Agent postinstall: installed ${assetName} (${size} MB)`);
  setupWindowsSpeech(dest);
})();
