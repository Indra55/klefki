#!/bin/sh
# Synthetic clean-container acceptance. No existing vault or user credentials are read.
set -eu
umask 077
cd "$(dirname "$0")/.."
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM
CGO_ENABLED=0 go build -o "$stage/vault" ./cli
mkdir "$stage/source" "$stage/input" "$stage/output"
printf '%s' 'public M6 synthetic password' > "$stage/input/password"
"$stage/vault" init --vault "$stage/source/account.v1" --password-file "$stage/input/password" --recovery-out "$stage/input/recovery"
python3 - "$stage/source" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
(root / 'document.txt').write_bytes(b'synthetic restore fixture\n' * 200000)
(root / 'empty.txt').write_bytes(b'')
PY
"$stage/vault" add --vault "$stage/source/account.v1" --password-file "$stage/input/password" "$stage/source/document.txt" > "$stage/receipt.json"
"$stage/vault" add --vault "$stage/source/account.v1" --password-file "$stage/input/password" "$stage/source/empty.txt" > /dev/null
"$stage/vault" backup --vault "$stage/source/account.v1" --password-file "$stage/input/password" -o "$stage/input/backup"
"$stage/vault" export-plain --vault "$stage/source/account.v1" --password-file "$stage/input/password" -o "$stage/expected"
chmod 755 "$stage/vault"
# The container receives only the binary, completed backup, and test credentials.
# No original store, document, home directory, network, or server is available.
docker run --rm --network none --read-only --user "$(id -u):$(id -g)" \
  --tmpfs /tmp:rw,nosuid,nodev,size=256m \
  -v "$stage/vault:/vault:ro" -v "$stage/input:/input:ro" \
  -v "$stage/output:/output" --entrypoint /bin/sh "${M6_TEST_IMAGE:-cairn-vault:m3}" -ec '
    /vault restore --password-file /input/password -o /output/password /input/backup
    /vault check --vault /output/password/account.v1 --password-file /input/password
    /vault export-plain --vault /output/password/account.v1 --password-file /input/password -o /output/plain-password
    /vault restore --recovery-file /input/recovery -o /output/recovery /input/backup
    /vault check --vault /output/recovery/account.v1 --recovery-file /input/recovery
    /vault export-plain --vault /output/recovery/account.v1 --recovery-file /input/recovery -o /output/plain-recovery
  '
python3 - "$stage" <<'PY'
import pathlib, sqlite3, sys
root = pathlib.Path(sys.argv[1])
expected = {p.name: p.read_bytes() for p in (root / 'expected').iterdir()}
def rows(path, table):
    with sqlite3.connect(f'file:{path}?mode=ro', uri=True) as db:
        return db.execute(f'SELECT * FROM {table} ORDER BY 1, 2').fetchall()
for method in ('password', 'recovery'):
    actual = {p.name: p.read_bytes() for p in (root / 'output' / f'plain-{method}').iterdir()}
    assert actual == expected, 'plaintext or metadata differs'
    restored = root / 'output' / method
    assert (restored / 'account.v1').read_bytes() == (root / 'input/backup/account.v1').read_bytes()
    for table in ('local_state', 'revisions', 'chunks', 'sqlite_sequence'):
        assert rows(restored / 'local.db', table) == rows(root / 'input/backup/local.db', table), 'store differs'
print('Clean-container password and recovery restores match every account byte, database row, manifest, and document byte.')
PY
