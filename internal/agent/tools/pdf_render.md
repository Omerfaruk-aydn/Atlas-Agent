Render one page of a PDF to a PNG so you can look at it. `view` extracts a PDF's text; this shows what the page looks like (layout, figures, scanned pages that have no text).

Give `path` and `page` (1-based, default 1). `dpi` is optional (default 110, up to 200): raise it only when small print is unreadable. The image is written to `.atlas/pdf/` and its path is returned; open it with `view` to see it.

It needs `pdftoppm` (Poppler) on PATH, the same package `view` uses for PDF text. Nothing is installed automatically; if it is missing the tool says so.
