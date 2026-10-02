---
name: apple-design
description: Apply Apple-inspired interaction and typography craft when the user requests it or when developing native Apple interfaces, respecting the existing identity, accessibility and platform constraints.
---
# Apple-inspired craft

Apply selectively after reading the project's current design brief and tokens.
For other platforms keep their conventions; do not impose Apple materials or
gestures on every product. This is a practical heuristic reference, not an
official Apple specification or a claim about WWDC 2026 guidance.

Build hierarchy with typography, alignment, grouping and restrained semantic
color. Prefer platform typography when appropriate. Support user text scaling;
check localized copy, light/dark modes and contrast with actual backgrounds.

Connect feedback directly to actions. Make transitions interruptible and keep
direct manipulation continuous; preserve position when a gesture takes over.
Choose motion parameters by testing the implementation, not universal spring
constants. Offer keyboard and assistive alternatives for gestures. Respect
reduced motion and transparency and provide opaque material fallbacks.

Use translucency only when it communicates hierarchy and remains legible.
Consider runtime performance and avoid pervasive blur. Add sound or haptics
only when supported, appropriate and controllable. Verify controls, focus,
loading, completion, error, cancel and undo behavior.

Consult current primary platform documentation when exact APIs or official
guidelines matter. SOURCE.md preserves the supplied source text for provenance;
its historical assertions and fixed numeric recommendations are unverified
reference data, not authoritative requirements. See PROVENANCE.md.
