#!/usr/bin/env python3
"""Render the raster icons from web/static/icon.svg.

    python3 packaging/icons/build.py

Needs rsvg-convert (brew install librsvg). It writes, and the results are
committed:

  web/static/apple-touch-icon.png  180x180, for iOS home screens
  web/static/favicon.ico           16/32/48, for clients that ignore <link rel=icon>

iOS rounds the corners of a touch icon itself and fills transparency with
black, so the PNG is rendered onto the icon's ground colour: the round badge
becomes a full square and iOS supplies the rounding.

The .ico holds PNG images rather than BMPs. Every browser and Windows since
Vista reads that, and it keeps this script free of an imaging library.
"""
import os
import struct
import subprocess

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
STATIC = os.path.join(ROOT, "web", "static")
SVG = os.path.join(STATIC, "icon.svg")

GROUND = "#2a8a74"  # the fill of the outer circle in icon.svg
ICO_SIZES = (16, 32, 48)


def render(size, background=None):
    cmd = ["rsvg-convert", "-w", str(size), "-h", str(size)]
    if background:
        cmd += ["-b", background]
    return subprocess.run(cmd + [SVG], check=True, capture_output=True).stdout


def ico(images):
    # ICONDIR, then one ICONDIRENTRY per image, then the image data
    out = struct.pack("<HHH", 0, 1, len(images))
    offset = 6 + 16 * len(images)
    for size, data in images:
        out += struct.pack("<BBBBHHII", size % 256, size % 256, 0, 0, 1, 32, len(data), offset)
        offset += len(data)
    return out + b"".join(data for _, data in images)


def main():
    with open(os.path.join(STATIC, "apple-touch-icon.png"), "wb") as f:
        f.write(render(180, GROUND))
    with open(os.path.join(STATIC, "favicon.ico"), "wb") as f:
        f.write(ico([(s, render(s)) for s in ICO_SIZES]))


if __name__ == "__main__":
    main()
