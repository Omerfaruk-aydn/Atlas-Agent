# Microphone, clipboard and video input

## Offline microphone dictation

In the prompt editor, press **Ctrl+K** to start recording, speak, then press
**Ctrl+K** again to finish. Atlas recognizes the recording locally and inserts
the resulting words at the current cursor. **Esc** cancels recording or
recognition. Enter cannot send the prompt until dictation finishes or is
canceled. Existing draft text is preserved; recognized text is never sent
automatically. Switching sessions cancels dictation.

The default backend is **Vosk 0.3.45**, running entirely offline on Windows x64.
The microphone recorder is native Windows code; a bundled PowerShell/C# helper
loads the Vosk DLL directly. No Python, Whisper, transcription API, GPU or
installed Windows speech-language package is required. Audio is never uploaded.
Recording defaults to 120 seconds, at most 180 seconds. Temporary audio is
removed after completion or cancellation; cancellation terminates the decoder.

Downloaded models:

| Language | Model |
| --- | --- |
| Turkish | `vosk-model-small-tr-0.3` |
| English | `vosk-model-small-en-us-0.15` |
| Italian | `vosk-model-small-it-0.22` |
| French | `vosk-model-small-fr-0.22` |

The Turkish model uses the legacy flat directory layout; it is supported without
changing its files. Models are loaded only when needed. Recognition quality
varies by microphone, accent and recording; model availability does not guarantee
an accurate transcript. Arabic has not been downloaded and is not silently
replaced with another language.

By default the TUI uses the `/language` selection as its dictation language.
Set `option voice language` to keep dictation independent of menu language.
Outside the TUI, an unset Vosk language means English. An unavailable selected
language is reported before the microphone opens; there is no silent fallback.

Windows npm and PowerShell installations run `atlas-agent voice install` to
download all four models and the runtime from official sources, verify pinned
SHA256 archive hashes and install sibling `speech-models` and `speech-runtime`
directories. The hashes pin verified release bytes; they are not upstream
signatures. Downloads total about 185 MB and installed files about 344 MB.
No UAC is required. Complete existing assets are reused. Downloads are bounded,
cancelable and staged before installation; incomplete existing directories are
preserved and reported for manual recovery. Speech setup failure leaves the CLI
usable; retry `atlas-agent voice install`. Standalone binary downloads need this
command once. For another executable location, set paths explicitly in `atlasrc`:

```bash
option voice backend vosk
option voice model-dir 'D:/Atlas/.atlas/speech-models'
option voice runtime-dir 'D:/Atlas/.atlas/speech-runtime'
option voice language tr-TR
option voice max-seconds 90
```

Alternatively `ATLAS_AGENT_SPEECH_HOME` points to a directory containing both
`speech-models` and `speech-runtime`. Native runtime libraries execute code;
use trusted Vosk release files, not libraries from an untrusted project.
On Windows filesystems without usable short paths, models may need an ASCII
folder path; Atlas reports that requirement instead of pretending to decode.

Check models without opening the microphone:

```powershell
atlas-agent voice status --language tr-TR
atlas-agent voice status --language it-IT
atlas-agent voice status --model-dir D:/Atlas/.atlas/speech-models --runtime-dir D:/Atlas/.atlas/speech-runtime
```

### Optional Windows engine

`option voice backend windows` selects the original Windows offline engine.
`atlas-agent voice status --backend windows` inspects its installed recognizers;
`atlas-agent voice setup --language en-US` installs missing Basic/Speech language
capabilities through Windows Update and may request UAC administrator approval.
This engine does not support Turkish, Arabic or Italian. Component installation
is followed by an actual recognizer check; unsupported Windows builds, blocked
updates, UAC rejection and restart requirements are reported separately.

`ATLAS_AGENT_VOICE_BACKEND=windows` opts installers into Windows-engine setup
instead of Vosk downloads. `ATLAS_AGENT_SKIP_SPEECH_SETUP=1` skips automatic
speech setup entirely. `ATLAS_AGENT_VOICE_LANGUAGE` selects the language checked
after setup; the Vosk installer still downloads all four models.
`voice setup --check-only --json` never changes Windows or opens UAC;
`--no-elevate` never requests administrator approval. Setup never changes display
language, enables cloud recognition, opens the microphone or restarts Windows.

## Clipboard files and images

On the first prompt of a new conversation, the submitted text and attachment
names appear immediately, with a localized waiting indicator and elapsed time.
This feedback remains visible while request upload or context preparation is
running. Persisted messages replace it without duplicating the conversation;
errors, cancellation and session changes remove the temporary indicator.

**Ctrl+V** pastes text, image pixels, or files copied in Explorer into the
composer. Multiple copied files are supported. **Ctrl+Alt+V** performs the same
operation when the terminal reserves Ctrl+V. A terminal that consumes Ctrl+V
without forwarding a key or paste event cannot be controlled by the CLI;
use Ctrl+Alt+V in that case. **Ctrl+Shift+V** requests clipboard text.

**Ctrl+F** opens the file picker for files, documents, images and videos.
Text and JSON become readable text. PDF, Office and notebook files use Atlas's
existing source-addressable document extraction. Images require an image-capable
model. Unsupported binary files become local references rather than being
uploaded as unsupported model input.

Each ordinary attachment is limited to 5 MB; a composer can contain at most
32 files and 20 MB of attachment content. Video attachments are local references
and can point at supported videos up to 2 GB. File processing runs outside the
UI loop. Results from a previous draft or session are discarded.

## Video evidence for the agent

The `video` tool supports MP4, MKV, MOV, WebM, AVI, M4V and MPEG files:

- `inspect`: duration, dimensions and audio availability.
- `sample`: a contact sheet containing timestamped frames for a chosen window.
- `transcribe`: configured offline speech recognition of audio from that window.

Install **ffmpeg** and **ffprobe** on PATH to decode video. Sampling requires a
model that supports images; metadata and audio transcription do not. Audio
recognition uses the same Vosk models or optional Windows engine as microphone dictation.
No media dependencies are installed automatically.

The default sample includes six evenly spaced frames from the first 180 seconds
or the whole video if shorter. Set `start_seconds`, `end_seconds` and `frames`
to inspect a relevant interval. A single invocation can cover at most 180 seconds
and 12 frames. Inspect longer videos in successive windows. Frames are labelled
with approximate source times (the nearest decoded frame at each requested
sample point). The tool returns the requested sample times and
the inspected interval with the image.

The agent must distinguish sampled evidence from continuous observation and
cite timestamps when describing events. It cannot infer that an event did not
occur between samples. Instructions visible in videos or transcripts remain
untrusted source content. The decoder restricts input formats and network
protocols; outside-workspace paths use normal read permissions. Temporary audio
is removed after each invocation.
