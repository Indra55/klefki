"""Compile Markdown chapters into a self-contained file:// HTML reader."""

import base64
import html
import json
import re
from pathlib import Path
from urllib.parse import unquote, urlsplit

try:
    import markdown
    from markdown.extensions.toc import slugify_unicode
except ImportError:
    raise SystemExit('Rebuilding requires Python-Markdown: python3 -m pip install Markdown')

from diagrams import generate

ROOT = Path(__file__).resolve().parent
CHAPTERS = sorted((ROOT / 'chapters').glob('*.md'))


def build():
    generate(ROOT / 'diagrams')
    records, articles = [], []
    chapter_ids = {path.resolve(): path.stem for path in CHAPTERS}

    for path in CHAPTERS:
        source = path.read_text(encoding='utf-8')
        title = source.splitlines()[0].removeprefix('# ')
        ident = path.stem
        words = len(re.findall(r'\b[\w-]+\b', source))
        body = markdown.markdown(source, extensions=['tables', 'fenced_code', 'toc'], extension_configs={
            'toc': {'slugify': lambda value, sep, prefix=ident: prefix + '-' + slugify_unicode(value, sep)}
        })

        def rewrite(match):
            attr, value = match.group(1), html.unescape(match.group(2))
            url = urlsplit(value)
            if url.scheme or value.startswith('#'):
                return match.group(0)
            target = (path.parent / unquote(url.path)).resolve()
            if not target.exists():
                raise ValueError(f'{path.name}: missing local target: {value}')
            if attr == 'src' and target.suffix == '.svg':
                value = 'data:image/svg+xml;base64,' + base64.b64encode(target.read_bytes()).decode('ascii')
            elif target in chapter_ids:
                value = '#' + chapter_ids[target]
            else:
                from os.path import relpath
                value = relpath(target, ROOT) + ('#' + url.fragment if url.fragment else '')
            return f'{attr}="{html.escape(value, quote=True)}"'

        body = re.sub(r'(href|src)="([^"]+)"', rewrite, body)
        records.append({'id': ident, 'title': title, 'words': words, 'minutes': max(1, round(words / 180)), 'file': 'chapters/' + path.name})
        articles.append(f'<article id="{ident}" class="chapter" data-chapter="{ident}">{body}</article>')

    template = (ROOT / 'reader.html').read_text(encoding='utf-8')
    data = json.dumps(records, ensure_ascii=False).replace('<', '\\u003c')
    output = template.replace('<!-- GUIDE_CSS -->', '<style>' + (ROOT / 'reader.css').read_text() + '</style>')
    output = output.replace('<!-- GUIDE_CHAPTERS -->', '\n'.join(articles))
    output = output.replace('<!-- GUIDE_DATA -->', '<script type="application/json" id="chapters-data">' + data + '</script>')
    output = output.replace('<!-- GUIDE_JS -->', '<script>' + (ROOT / 'reader.js').read_text() + '</script>')
    (ROOT / 'index.html').write_text(output, encoding='utf-8')
    print(f'Built {len(records)} chapters, {sum(r["words"] for r in records):,} words -> {ROOT / "index.html"}')


if __name__ == '__main__':
    build()
