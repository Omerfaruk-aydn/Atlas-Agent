Get LSP errors, warnings, and hints for a file or the whole project.

Response metadata includes `observed`: false means no running language server
was available or the observation was cancelled. An empty response alone does
not prove a clean diagnostic pass.
