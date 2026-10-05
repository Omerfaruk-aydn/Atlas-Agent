# Explorer path readback, v16 candidate

Latest user benchmark: 3:41, preceding run 2:27. Session records:
1992fb10-7c72-4c06-80ec-ab9c2338d97d and
0f070de5-3e83-4b2e-9888-9c892ed9beca. Calls increased from 16 to 21,
image results from 2 to 4; neither run used mode flow. Explorer folder/address
preparation took approximately 98 seconds instead of 44. These are conversation
intervals including model/provider waiting, not native execution timings.

The new inspect helper reads Shell.Application's current folder for the exact
Explorer HWND and native process. It does not focus, navigate, press keys or
read file contents. The shell folder must be a filesystem directory; virtual,
duplicate, changing or unavailable identities omit the field. Enumeration is
capped at 128 shell windows with a 350 ms loop bound; the outer tool timeout
still bounds COM calls that do not return promptly.

Observation returns explorer_location only after matching the current foreground
window ID, process, executable and CabinetWClass. It contains the exact Unicode
path, provenance and verification flag. It is excluded from the snapshot cache.
The result places it before large control lists. Existing visual fallback and
target verification remain unchanged; folder identity does not prove file type
or saved content.

Explorer URLs may use legacy system-code-page percent escapes. The helper uses
Folder.Self.Path directly and reads folder path/location URL twice for stability;
it does not decode localized path labels or assume UTF-8 URL escaping. This
preserves Ö, ü and ampersands in the actual folder path.

Main prompt, desktop role, tool descriptions and Explorer capability hints now
tell the model to reuse this verified path, avoiding repeated Ctrl+L/Alt+D,
address clicks and screenshots solely to rediscover it. If unavailable, use one
resolved address-field read before obtaining targeted evidence or replanning.

Validation includes mocked shell exact-window/virtual/duplicate/invalid-path
cases, native identity rejection, no extra input/image calls, cache exclusion,
and a live read of an existing Explorer folder without changing foreground.
Candidate: D:/Atlas/.atlas/atlas-desktop-flow-v16-dev.exe. Benchmark speed remains
unmeasured until the actual task is rerun with this candidate.

Full tools (78.505s), computer (14.628s), prompt (7.554s) and subagents
(0.878s) suites passed. Scoped lint reported zero issues. Go formatting,
diff checks, build and v0.15.6-desktop-flow-v16-dev version check passed.
