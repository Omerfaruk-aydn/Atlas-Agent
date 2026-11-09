Compare two images and report how much they differ. Use it to check a page you built against a reference mockup, or a screenshot against an earlier one.

Give `reference_path` (the target look) and `candidate_path` (what you have). The candidate is scaled to the size of the reference first, so screenshots at different widths can be compared, though a different aspect ratio will count as a difference.

The result gives the share of pixels that differ by more than `tolerance` (0-255 per colour channel, default 16), the average difference, and a 3x3 grid showing where the differences are. A diff image is written to `.atlas/compare/` (differences in red over a faded reference): open it with `view` to see what to fix.

This measures pixels, not taste: a small share can still be the wrong font, and a large one can be a harmless shift. Use the numbers to find where to look, then look.
