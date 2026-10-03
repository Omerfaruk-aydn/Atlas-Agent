Audit the actual currently loaded browser page. a11y_audit uses window.axe.run
when axe-core is already available. Otherwise it reports a clearly marked partial
DOM audit for missing image alternatives, control names, input labels and page
language. A partial audit is not WCAG certification. interaction_audit observes
horizontal overflow, focus and at most 200 controls; use ui_verify/scenario for
actual keyboard and interaction steps. The browser's eval permission applies.

visual_diff compares two recorded ui_verify captures in the current session by
artifact_id. Both hashes and image dimensions are checked before comparing pixels.
Tolerance is 0-255 per channel. max_changed_ratio is 0-1, default 0 (exact match).
Animations, fonts and dynamic content can cause differences; this is a measured
visual regression result, not a judgment of design quality.
