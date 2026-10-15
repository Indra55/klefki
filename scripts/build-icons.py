#!/usr/bin/env python3
"""Export the original app mark with rsvg-convert (librsvg)."""

from pathlib import Path
import shutil
import subprocess
import xml.etree.ElementTree as ET


def main():
    renderer = shutil.which("rsvg-convert")
    if not renderer:
        raise SystemExit("Install librsvg's rsvg-convert to regenerate icons.")
    root = Path(__file__).resolve().parent.parent
    public = root / "web/public"
    source = (public / "icon.svg").read_bytes()
    tree = ET.fromstring(source)
    tree.find("{http://www.w3.org/2000/svg}rect").set("rx", "0")
    full_bleed = ET.tostring(tree)
    outputs = [
        (public / "icon-192.png", 192, source),
        (public / "icon-512.png", 512, source),
        (public / "apple-touch-icon.png", 180, full_bleed),
        (public / "icon-maskable-192.png", 192, full_bleed),
        (public / "icon-maskable-512.png", 512, full_bleed),
        (root / "desktop/build/appicon.png", 1024, source),
    ]
    for output, size, svg in outputs:
        subprocess.run(
            [renderer, "--width", str(size), "--height", str(size), "--output", str(output)],
            input=svg, check=True,
        )
        print(output.relative_to(root))


if __name__ == "__main__":
    main()
