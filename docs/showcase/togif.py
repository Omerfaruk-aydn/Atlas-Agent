"""Converts an animated WebP clip to an optimized GIF next to it.

    python docs/showcase/togif.py docs/assets/clip-hero.webp [--width 900] [--colors 96]
"""
import argparse
import os

from PIL import Image, ImageSequence

ap = argparse.ArgumentParser()
ap.add_argument("src")
ap.add_argument("--width", type=int, default=900)
ap.add_argument("--colors", type=int, default=96)
a = ap.parse_args()

im = Image.open(a.src)
frames, ms = [], []
for f in ImageSequence.Iterator(im):
    rgb = f.convert("RGB")
    if rgb.width > a.width:
        rgb = rgb.resize((a.width, round(rgb.height * a.width / rgb.width)), Image.LANCZOS)
    frames.append(rgb.quantize(colors=a.colors, method=Image.Quantize.MEDIANCUT, dither=Image.Dither.NONE))
    ms.append(max(20, f.info.get("duration", 100)))
out = os.path.splitext(a.src)[0] + ".gif"
frames[0].save(out, save_all=True, append_images=frames[1:], duration=ms, loop=0, optimize=True, disposal=1)
print(f"{os.path.basename(out)}: {len(frames)} frames, {os.path.getsize(out) // 1024} KB")
