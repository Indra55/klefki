#!/usr/bin/env python3
"""Install the Linux desktop executable and launcher without touching app data."""

import argparse
import filecmp
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


def desktop_string(value):
    return str(value).replace("\\", "\\\\").replace("\n", "\\n").replace("\r", "\\r")


def desktop_command(path):
    value = str(path).replace("%", "%%")
    for char in ("\\", '"', "`", "$"):
        value = value.replace(char, "\\" + char)
    return desktop_string('"' + value + '"')


def main():
    root = Path(__file__).resolve().parent.parent
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--prefix", type=Path, default=Path.home() / ".local")
    parser.add_argument("--binary", type=Path, default=root / "desktop/build/bin/klefki-desktop")
    parser.add_argument("--rollback", action="store_true")
    args = parser.parse_args()
    if os.getuid() == 0:
        parser.error("run as your normal desktop user, without sudo")

    prefix = args.prefix.expanduser().resolve()
    data = Path(os.environ.get("XDG_DATA_HOME") or prefix / "share").expanduser().resolve()
    executable = prefix / "bin/klefki-desktop"
    previous = data / "klefki/klefki-desktop.previous"
    source = previous if args.rollback else args.binary.expanduser().resolve()
    if not source.is_file() or not os.access(source, os.X_OK):
        parser.error(f"no executable at {source}")
    if args.rollback and not executable.is_file():
        parser.error("no installed executable to roll back")

    staged = []

    def stage(target, source=None, text=None, mode=0o755):
        target.parent.mkdir(parents=True, exist_ok=True)
        fd, name = tempfile.mkstemp(prefix=".klefki-", suffix=target.suffix, dir=target.parent)
        os.close(fd)
        temporary = Path(name)
        staged.append(temporary)
        if source is not None:
            shutil.copyfile(source, temporary)
        else:
            temporary.write_text(text, encoding="utf-8")
        temporary.chmod(mode)
        return temporary

    try:
        binary = stage(executable, source=source)
        if not args.rollback:
            icon = data / "icons/hicolor/512x512/apps/klefki.png"
            desktop = data / "applications/klefki.desktop"
            image = stage(icon, source=root / "web/public/icon-512.png", mode=0o644)
            entry = stage(desktop, text=(
                "[Desktop Entry]\nType=Application\nName=Klefki\n"
                "Comment=Private documents and encrypted sync\n"
                f"Exec={desktop_command(executable)}\n"
                f"Icon={desktop_string(icon)}\n"
                "Terminal=false\nCategories=Office;\n"
            ), mode=0o644)
            validator = shutil.which("desktop-file-validate")
            if validator:
                subprocess.run([validator, str(entry)], check=True)
            os.replace(image, icon)
            os.replace(entry, desktop)

        if executable.exists() and not filecmp.cmp(executable, binary, shallow=False):
            backup = stage(previous, source=executable)
            os.replace(backup, previous)
        os.replace(binary, executable)
    finally:
        for temporary in staged:
            temporary.unlink(missing_ok=True)

    updater = shutil.which("update-desktop-database")
    if updater and not args.rollback:
        subprocess.run([updater, str(data / "applications")], check=True)
    print(f"Klefki installed: {executable}")
    if previous.exists():
        print(f"Previous executable: {previous}")
    print("Close any running Klefki window, then open Klefki from the app drawer.")


if __name__ == "__main__":
    main()
