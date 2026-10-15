"""Generate the guide's offline SVG diagrams using only the Python standard library."""

from html import escape
from pathlib import Path


def diagram(root, name, title, nodes, edges, height=480, notes=()):
    parts = [f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 840 {height}" role="img" aria-labelledby="title desc">
<title id="title">{escape(title)}</title><desc id="desc">{escape('; '.join(n[3] + ': ' + n[4] for n in nodes))}</desc>
<defs><marker id="arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8" fill="none" stroke="#6e8079" stroke-width="1.5"/></marker></defs>
<rect width="840" height="{height}" rx="16" fill="#f6f8f4"/>
<style>text{{font-family:system-ui,sans-serif}}.title{{font-size:18px;font-weight:650;fill:#22332c}}.label{{font-size:14px;font-weight:650;fill:#22332c}}.detail{{font-size:12px;fill:#53665d}}.edge{{font-size:11px;fill:#53665d;paint-order:stroke;stroke:#f6f8f4;stroke-width:7px;stroke-linejoin:round}}</style>
<text class="title" x="28" y="34">{escape(title)}</text>''']
    by_id = {n[0]: n for n in nodes}
    for start, end, label in edges:
        a, b = by_id[start], by_id[end]
        ax, ay, bx, by = a[1], a[2], b[1], b[2]
        if abs(ay - by) < 15:
            x1, y1, x2, y2 = ax + (220 if bx > ax else 0), ay + 39, bx + (0 if bx > ax else 220), by + 39
        else:
            x1, y1, x2, y2 = ax + 110, ay + (78 if by > ay else 0), bx + 110, by + (0 if by > ay else 78)
        mid = (y1 + y2) / 2
        path = f'M{x1},{y1} L{x2},{y2}' if y1 == y2 else f'M{x1},{y1} C{x1},{mid} {x2},{mid} {x2},{y2}'
        parts.append(f'<path d="{path}" fill="none" stroke="#6e8079" stroke-width="1.7" marker-end="url(#arrow)"/>')
        if label:
            parts.append(f'<text class="edge" text-anchor="middle" x="{(x1+x2)/2}" y="{(y1+y2)/2-8}">{escape(label)}</text>')
    for ident, x, y, label, detail, *accent in nodes:
        fill = '#e3ece4' if accent else '#ffffff'
        parts.append(f'<rect x="{x}" y="{y}" width="220" height="78" rx="10" fill="{fill}" stroke="#cad6cc"/>')
        parts.append(f'<text class="label" x="{x+16}" y="{y+29}">{escape(label)}</text>')
        for i, line in enumerate(detail.split('|')):
            parts.append(f'<text class="detail" x="{x+16}" y="{y+49+i*16}">{escape(line)}</text>')
    for i, note in enumerate(notes):
        parts.append(f'<text class="detail" x="28" y="{height-40+i*17}">{escape(note)}</text>')
    parts.append('</svg>')
    (root / f'{name}.svg').write_text('\n'.join(parts), encoding='utf-8')


def generate(root):
    root.mkdir(parents=True, exist_ok=True)
    diagram(root, 'system', 'Clients hold keys; the server transports ciphertext', [
        ('cli', 30, 70, 'Go CLI', 'Local commands and recovery'),
        ('desktop', 310, 70, 'Desktop process', 'Native runtime and session'),
        ('web', 590, 70, 'Browser client', 'Independent TypeScript core'),
        ('core', 170, 225, 'Shared Go core', 'Crypto / SQLite / search', True),
        ('idb', 590, 225, 'Browser store', 'Crypto / IndexedDB / sync', True),
        ('server', 310, 380, 'Ciphertext server', 'Sessions / log / blobs', True),
    ], [('cli', 'core', ''), ('desktop', 'core', ''), ('web', 'idb', ''), ('core', 'server', 'encrypted records'), ('idb', 'server', 'encrypted records')], 520,
        ['Plaintext processing occurs on unlocked clients. The server does not receive document keys.'])
    diagram(root, 'startup', 'Process initialization is separate from account unlock', [
        ('start', 30, 70, 'Start process', 'Flags / config / runtime'),
        ('server', 30, 225, 'Server.Open', 'Schema / blobs / routes'),
        ('account', 310, 70, 'Load account.v1', 'Descriptor and sealed keys'),
        ('unlock', 310, 225, 'Unlock locally', 'Password or recovery'),
        ('session', 590, 225, 'Unlocked session', 'Keys / store / cancellation', True),
    ], [('start', 'server', 'server path'), ('start', 'account', 'client path'), ('account', 'unlock', ''), ('unlock', 'session', '')], 390,
        ['Starting the server never unwraps a client master key.'])
    diagram(root, 'keys', 'Password-derived access and randomly generated document keys', [
        ('password', 310, 65, 'Password + salt', 'Argon2id: 64-byte output'),
        ('kek', 30, 210, 'KEK: first 32 bytes', 'Opens password wrapper'),
        ('auth', 590, 210, 'Auth: last 32 bytes', 'HMAC -> server credential'),
        ('master', 30, 355, 'Random master key', 'Metadata and key wrappers', True),
        ('recovery', 310, 355, 'Random recovery key', 'Alternative master wrapper'),
        ('file', 30, 500, 'Random file key', 'Encrypts one content stream', True),
        ('chunks', 310, 500, 'Content chunks', 'Fresh nonces; bound context'),
    ], [('password', 'kek', ''), ('password', 'auth', ''), ('kek', 'master', 'unwrap'), ('recovery', 'master', 'unwrap'), ('master', 'file', 'unwrap'), ('file', 'chunks', 'AEAD')], 660,
        ['The server credential authorizes access. It is separate from the document master key.'])
    diagram(root, 'records', 'AEAD checks both the payload and its expected context', [
        ('context', 30, 80, 'Public header / AAD', 'Account / purpose / object'),
        ('payload', 310, 80, 'Private payload', 'Metadata, key, or chunk'),
        ('key', 590, 80, 'Secret key + nonce', 'Correct key for record kind'),
        ('seal', 310, 230, 'XChaCha20-Poly1305', 'Ciphertext + authentication tag', True),
        ('verify', 310, 380, 'Client verification', 'Check context and completeness', True),
    ], [('context', 'seal', ''), ('payload', 'seal', ''), ('key', 'seal', ''), ('seal', 'verify', '')], 525,
        ['A valid chunk tag does not establish that all expected chunks arrived.'])
    diagram(root, 'revisions', 'Concurrent edits preserve two heads of one logical object', [
        ('base', 310, 80, 'V0: original edition', 'Object O / content F0'),
        ('left', 100, 240, 'V1: desktop edit', 'base=V0 / tag Utilities', True),
        ('right', 520, 240, 'V2: offline browser edit', 'base=V0 / favorite=true', True),
        ('next', 100, 400, 'V3: edit of V1', 'base=V1 / V2 still survives'),
    ], [('base', 'left', ''), ('base', 'right', ''), ('left', 'next', '')], 560,
        ['After V1 and V2: both are heads. After V3: the heads are V3 and V2.', 'Neither timestamps nor upload order select a winner.'])
    flow(root, 'mutations', 'Content import and metadata-only edits have different costs', [
        ('Read source bytes', 'Bill PDF / validate length'),
        ('Fresh IDs and file key', 'Encrypt chunks + wrap key'),
        ('Atomic local commit', 'Ciphertext + pending revision'),
        ('Later rename / move', 'Encrypt new metadata only'),
        ('Child revision', 'Reuse unchanged content chunks'),
        ('Future sync', 'Publish immutable editions'),
    ], 'A changed content stream requires a fresh file key. A metadata edit can reuse content.')
    flow(root, 'server', 'Server request processing preserves account scope', [
        ('Incoming HTTP request', 'Transport / size / rate bounds'),
        ('Authenticate session', 'Token hash -> account/device'),
        ('Check account rules', 'Deletion / relay / route scope'),
        ('Handle immutable data', 'Validate framing and links'),
        ('Publish transactionally', 'Log / revision / blob rules'),
        ('Return bounded response', 'Client still verifies AEAD'),
    ], 'The server validates structure; an unlocked client validates private content authenticity.')
    flow(root, 'sync', 'A revision becomes durable before receipt is acknowledged', [
        ('A: pending local edit', 'Already committed offline'),
        ('A: upload missing chunks', 'Exact ciphertext; safe retry'),
        ('Server: publish revision', 'Assign change sequence'),
        ('B: fetch and authenticate', 'Metadata / key / all chunks'),
        ('B: local transaction', 'Chunks + revision + cursor'),
        ('B: acknowledge receipt', 'Relay may now use receipt'),
    ], 'Reading bytes, committing a revision, and acknowledging receipt are distinct steps.')
    diagram(root, 'relay', 'Temporary relay cleanup follows durable delivery', [
        ('a', 30, 90, 'Device A', 'Retains encrypted local history'),
        ('relay', 310, 90, 'Delivery buffer', 'Opaque ciphertext on server', True),
        ('b', 590, 90, 'Device B', 'Authenticate and save locally'),
        ('receipts', 310, 250, 'Delivery requirements', 'Linked-device durable cursors'),
        ('cleanup', 310, 405, 'Payload cleanup', 'Local history remains', True),
    ], [('a', 'relay', 'upload'), ('relay', 'b', 'download'), ('a', 'receipts', 'receipt'), ('b', 'receipts', 'receipt'), ('receipts', 'cleanup', 'all required receipts')], 565,
        ['At least two linked devices are required. A new device may need an existing device to reseed.'])
    flow(root, 'lifecycle', 'Locking invalidates ownership before waiting for work', [
        ('Unlocked session', 'Owns keys / store / model'),
        ('Detach + new generation', 'Old results are stale'),
        ('Cancel session context', 'Stop active operations'),
        ('Wait for active work', 'Serialized work / network'),
        ('Wipe and close resources', 'Keys / model / local store'),
        ('Revoke remote session', 'Best effort; bounded attempt'),
    ], 'Runtime wiping is best effort. Locked mode does not run a decrypting sync daemon.')
    diagram(root, 'browser', 'Browser persistence is split by responsibility', [
        ('runtime', 310, 70, 'Unlocked JS runtime', 'Keys and plaintext in memory', True),
        ('db', 30, 240, 'IndexedDB', 'Ciphertext / revisions / cursor'),
        ('cache', 310, 240, 'Cache Storage', 'App shell / verified models'),
        ('local', 590, 240, 'localStorage', 'Encrypted PRF unlock wrapper'),
    ], [('runtime', 'db', 'transactional edits'), ('runtime', 'cache', 'runtime assets'), ('local', 'runtime', 'convenience unlock')], 415,
        ['The service worker excludes API traffic. Browser storage may be cleared or evicted.'])
    diagram(root, 'extraction', 'Local extraction converts supported content into text', [
        ('text', 30, 70, 'UTF-8 text', 'Validate size / Unicode / NUL'),
        ('image', 310, 70, 'PNG / JPEG', 'Decode -> canonical PNG'),
        ('pdf', 590, 70, 'PDF', 'Private staging / Poppler'),
        ('ocr', 450, 235, 'Tesseract OCR', 'Images or rendered pages'),
        ('merged', 310, 390, 'Bounded extracted text', 'Merge / deduplicate terms', True),
    ], [('image', 'ocr', ''), ('pdf', 'ocr', 'scan / mixed content'), ('text', 'merged', ''), ('pdf', 'merged', 'selectable text'), ('ocr', 'merged', '')], 535,
        ['Text and staged PDFs are plaintext while processing on the trusted client.'])
    flow(root, 'search', 'The private word catalog is encrypted and synced as a document', [
        ('Visible document heads', 'Exclude trash and sidecars'),
        ('Extract + count terms', 'Name / tags / document text'),
        ('Validate staleness', 'Same revisions before publish'),
        ('Encrypted index sidecar', 'Ordinary chunks and revision'),
        ('Unlocked local query', 'Stop words / evidence gate'),
        ('Rank candidates', 'BM25-style term weighting'),
    ], 'The server transports the index. It does not perform plaintext document search.')
    flow(root, 'semantic', 'Local inference produces a model-specific vector space', [
        ('Verified model assets', 'Pinned revision and SHA-256'),
        ('Tokenize local text', '480 tokens / 64 overlap'),
        ('Run ONNX inference', 'CLS pooling / L2 normalize'),
        ('Encrypted vector sidecar', '384-dimensional vectors'),
        ('Embed local query', 'Cosine / evidence cutoffs'),
        ('Hybrid result fusion', 'Keyword + semantic ranks'),
    ], 'Cosine similarity is a ranking signal. It is not a calibrated correctness probability.')
    flow(root, 'organization', 'Analysis and accepted edits have separate lifetimes', [
        ('Current indexed documents', 'Text / vectors / revisions'),
        ('Generate suggestions', 'Labels / tags / field patterns'),
        ('Check uncertainty', 'Score and runner-up margin'),
        ('Review / correct', 'Human interpretation'),
        ('Recheck stale plan', 'Generation and current heads'),
        ('Persist encrypted edits', 'Tags / correction sidecars'),
    ], 'Replacing content changes file identity; old corrections must not silently describe new bytes.')
    flow(root, 'backup', 'Restore success follows structure and content verification', [
        ('Existing selected store', 'Close competing owner'),
        ('Consistent snapshot', 'SQLite VACUUM INTO'),
        ('Account + local database', 'Wrapped keys + ciphertext'),
        ('Unlock snapshot locally', 'Password or recovery key'),
        ('Authenticate full history', 'SQLite + metadata + chunks'),
        ('Publish completed restore', 'New destination; no overwrite'),
    ], 'Sync is replication. An independent earlier snapshot preserves a different recovery point.')


def flow(root, name, title, labels, note):
    # A two-row path keeps every diagram readable on a narrow screen when zoomed.
    positions = [(30, 85), (310, 85), (590, 85), (590, 270), (310, 270), (30, 270)]
    nodes = [(str(i), *positions[i], label, detail, True) for i, (label, detail) in enumerate(labels)]
    diagram(root, name, title, nodes, [(str(i), str(i + 1), '') for i in range(len(nodes) - 1)], 430, [note])


if __name__ == '__main__':
    generate(Path(__file__).resolve().parent / 'diagrams')
