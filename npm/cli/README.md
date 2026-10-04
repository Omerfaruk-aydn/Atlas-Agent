# @atlas-coder/atlas-agent

Atlas Agent — terminal-first AI coding assistant.

## Install

```bash
npm install -g @atlas-coder/atlas-agent
atlas-agent
```

On install, the matching native binary for your OS/arch is downloaded from the
[GitHub release](https://github.com/Omerfaruk-aydn/Atlas-Agent/releases) that
matches this package version.

On Windows x64, installation and updates also run `atlas-agent voice install`.
This downloads the offline **Vosk** runtime and Turkish, English, Italian and
French models from their official sources, verifies pinned SHA256 hashes and
installs them beside the executable. Downloads total approximately 185 MB;
installed assets occupy approximately 344 MB. Existing complete assets are
reused. Administrator permission, a transcription API and Whisper are not
required. Failed speech setup leaves the coding CLI installed and usable.

Use `atlas-agent voice install` to retry downloads and `atlas-agent voice status`
to check recognition. `ATLAS_AGENT_SKIP_SPEECH_SETUP=1` skips automatic downloads.
Menu language selects the dictation model unless `option voice language` is set.
`option voice model-dir` and `option voice runtime-dir` support custom paths.

The legacy Windows engine remains optional: set `option voice backend windows`.
`atlas-agent voice setup` installs its Windows components and may request UAC.
Set `ATLAS_AGENT_VOICE_BACKEND=windows` before npm installation to choose this
optional setup instead of Vosk downloads. That legacy engine does not support
Turkish or Italian. Arabic models are not installed. Audio remains local.

## Usage

```bash
atlas-agent                  # interactive mode
atlas-agent run "your task"  # non-interactive
atlas-agent --help           # CLI help
atlas-agent models           # list models
atlas-agent dirs             # show config paths
```

## Supported platforms

| OS      | Architectures         |
| ------- | --------------------- |
| Windows | x64                   |
| macOS   | x64 (Intel), arm64 (Apple Silicon) |
| Linux   | x64                   |

## License

MIT — see [LICENSE.md](https://github.com/Omerfaruk-aydn/Atlas-Agent/blob/main/LICENSE.md).
