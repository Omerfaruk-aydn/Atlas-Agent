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

For direct manipulation, preserve the grab offset, capture/release the pointer
and handle cancellation. Start retargeted motion from its actual presentation
state and carry velocity only when the implementation supports it. Bound momentum
to valid destinations; test reversing a moving sheet and cancelling a drag.
Press feedback can start on pointer-down, but commit activation on the appropriate
release/click/keyboard event so users can cancel. Never execute a destructive
action simply because a pointer touched its control.

Define type hierarchy with the actual font's metrics, content width and localized
text. Use optical sizing/tracking where supported; avoid a universal letter-spacing
rule. Depth must explain hierarchy: anchors, consistent enter/exit paths and clear
modal versus non-modal behavior. Avoid stacking translucent surfaces that destroy
contrast. Verify reduced-motion, opaque fallback, focus restoration and slow-device
behavior instead of choosing spring numbers from a table and claiming native fidelity.

Use translucency only when it communicates hierarchy and remains legible.
Consider runtime performance and avoid pervasive blur. Add sound or haptics
only when supported, appropriate and controllable. Verify controls, focus,
loading, completion, error, cancel and undo behavior.

Consult current primary platform documentation when exact APIs or official
guidelines matter. SOURCE.md preserves the supplied source text for provenance;
its historical assertions and fixed numeric recommendations are unverified
reference data, not authoritative requirements. See PROVENANCE.md.
