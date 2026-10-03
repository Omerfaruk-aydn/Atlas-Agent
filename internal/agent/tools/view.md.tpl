Read a file by path with line numbers; supports offset and line limit (default {{ .DefaultReadLimit }}, max {{ .MaxViewSizeKB }}KB returned file content section); renders images (PNG, JPEG, GIF, WebP); use ls for directories.

DOCX, XLSX, PPTX and format-4 Jupyter notebooks are extracted as text with paragraph, sheet/cell, slide or cell source positions. PDF extraction uses pdftotext (Poppler) on PATH; no converter is installed automatically. Documents are bounded to 50 MiB and extracted output to 2 MiB before normal pagination. Spreadsheet formulas are shown with cached values and are not recalculated. Notebook code is not executed. Empty PDF pages warn that OCR may be needed; text extraction is not proof of visual completeness. Use metadata total_lines/has_more and offset to continue reading.
{{ if .HashAnchors }}
Each line is shown as `line|hash|content`. For a single-line change, pass that hash as edit's anchor_hash with anchor_line instead of old_string -- no need to reproduce the line's exact text or whitespace. A stale hash is rejected, telling you to re-read the file rather than silently landing on the wrong line.
{{ end }}
