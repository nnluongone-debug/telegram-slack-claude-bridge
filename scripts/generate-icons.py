#!/usr/bin/env python3
"""Draws the app icons into assets/ (needs Pillow: pip install pillow).

- icon-1024.png        app icon (macOS .icns and the Windows .ico are built from it)
- icon.ico             Windows executable icon
- menubar-template.png menu bar icon, black on transparent (macOS tints it), 22 pt
- menubar-template@2x.png
"""
from pathlib import Path

from PIL import Image, ImageChops, ImageDraw

ASSETS = Path(__file__).resolve().parent.parent / "assets"
SS = 4  # supersampling factor for smooth edges

BG_TOP, BG_BOTTOM = (49, 46, 129), (124, 58, 237)  # indigo → violet
ACCENT = (251, 146, 60)  # orange prompt


def gradient(size, top, bottom):
    img = Image.new("RGB", (size, size))
    draw = ImageDraw.Draw(img)
    for y in range(size):
        t = y / (size - 1)
        draw.line([(0, y), (size, y)], fill=tuple(round(a + (b - a) * t) for a, b in zip(top, bottom)))
    return img


def bubble_mask(size, box, radius, tail):
    """A rounded speech bubble with a tail at the bottom left."""
    mask = Image.new("L", (size, size), 0)
    draw = ImageDraw.Draw(mask)
    draw.rounded_rectangle(box, radius=radius, fill=255)
    draw.polygon(tail, fill=255)
    return mask


def prompt_mask(size, origin, scale, stroke):
    """The terminal prompt ">_" drawn as thick rounded strokes."""
    mask = Image.new("L", (size, size), 0)
    draw = ImageDraw.Draw(mask)
    x, y = origin
    chevron = [(x, y), (x + 0.9 * scale, y + 0.9 * scale), (x, y + 1.8 * scale)]
    draw.line(chevron, fill=255, width=stroke, joint="curve")
    for px, py in (chevron[0], chevron[2]):
        draw.ellipse([px - stroke / 2, py - stroke / 2, px + stroke / 2, py + stroke / 2], fill=255)
    ux, uy = x + 1.35 * scale, y + 1.8 * scale
    draw.rounded_rectangle([ux, uy - stroke / 2, ux + 1.2 * scale, uy + stroke / 2], radius=stroke / 2, fill=255)
    return mask


def app_icon():
    n = 1024 * SS
    s = lambda v: v * SS  # noqa: E731  (design units are 1024-px pixels)

    # macOS icon grid: 824-px rounded square centred on a 1024 canvas.
    tile = Image.new("L", (n, n), 0)
    ImageDraw.Draw(tile).rounded_rectangle([s(100), s(100), s(924), s(924)], radius=s(185), fill=255)
    icon = Image.new("RGBA", (n, n), (0, 0, 0, 0))
    icon.paste(gradient(n, BG_TOP, BG_BOTTOM), mask=tile)

    bubble = bubble_mask(n, [s(230), s(250), s(794), s(690)], s(110),
                         [(s(300), s(640)), (s(300), s(800)), (s(450), s(680))])
    icon.paste((255, 255, 255, 255), mask=bubble)
    icon.paste(ACCENT + (255,), mask=prompt_mask(n, (s(340), s(355)), s(130), s(62)))
    return icon.resize((1024, 1024), Image.LANCZOS)


def menubar_icon(pt, scale):
    px = pt * scale
    n = px * SS
    u = n / 22  # design units: a 22-pt grid
    bubble = bubble_mask(n, [u * 1.5, u * 2.5, u * 20.5, u * 16.5], u * 4,
                         [(u * 4, u * 15), (u * 4, u * 20.5), (u * 9, u * 16)])
    prompt = prompt_mask(n, (u * 5.5, u * 5.6), u * 3.4, round(u * 2.2))
    alpha = ImageChops.subtract(bubble, prompt)  # the prompt is cut out of the bubble
    img = Image.new("RGBA", (n, n), (0, 0, 0, 0))
    img.putalpha(alpha)
    return img.resize((px, px), Image.LANCZOS)


def main():
    ASSETS.mkdir(exist_ok=True)
    icon = app_icon()
    icon.save(ASSETS / "icon-1024.png")
    icon.save(ASSETS / "icon.ico", sizes=[(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)])
    menubar_icon(22, 1).save(ASSETS / "menubar-template.png")
    menubar_icon(22, 2).save(ASSETS / "menubar-template@2x.png")
    print("icons written to", ASSETS)


if __name__ == "__main__":
    main()
