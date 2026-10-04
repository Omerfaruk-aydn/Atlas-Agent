# Interface language implementation

The approved feature is a persistent `/language` picker for English, Turkish,
German, French, Italian, and Arabic. Atlas-authored interface labels, menus,
help, notices, and descriptions are localized. Commands, identifiers, code,
paths, subprocess output, user content, and conversation history retain their
original content. This setting does not change model instructions or permissions.

Implementation stays in the current checkout and branch.

1. Add embedded deterministic catalogs and locale validation, without global
   mutable locale state or network translation. Verify placeholder compatibility.
2. Connect locale to config, global persistence, shell config, and schema.
3. Add a searchable keyboard-accessible language picker and slash command.
   Save asynchronously and publish UI state only after successful persistence.
4. Route Atlas-authored text through per-UI/per-style translators. Preserve
   original command aliases and stable IDs for search and dispatch.
5. Refresh component labels and cached rendering after selection while
   preserving conversation content. Handle narrow terminals and Arabic text.
6. Verify catalogs, picker dispatch, persistence, language isolation, preserved
   commands/content, rendering dimensions, relevant tests, lint, and build.

Translations are source-controlled resources. Unsupported language values
are rejected when writing config. An untranslated key has an English fallback;
coverage checks report catalog gaps rather than claiming complete translation.
Arabic glyph shaping and bidirectional behavior depend on terminal support;
code/path strings must never be reordered or reversed to simulate RTL.
