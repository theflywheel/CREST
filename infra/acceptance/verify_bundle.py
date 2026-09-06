#!/usr/bin/env python3
import hashlib
import pathlib
import sys
import tarfile

root = pathlib.Path(sys.argv[1]).resolve()
try:
    entries = {}
    for line in (root / 'manifest.sha256').read_text().splitlines():
        digest, name = line.split(None, 1)
        rel = pathlib.PurePosixPath(name)
        if rel.is_absolute() or '..' in rel.parts or name in entries:
            raise ValueError('unsafe or duplicate manifest entry')
        entries[name] = digest
    actual = {str(p.relative_to(root)) for p in root.rglob('*') if p.is_file() and p.name != 'manifest.sha256'}
    if actual != set(entries):
        raise ValueError('manifest must cover every bundle file exactly once')
    for name, expected in entries.items():
        path = root / name
        if path.is_symlink() or not path.resolve().is_relative_to(root):
            raise ValueError('unsafe bundle path')
        digest = hashlib.sha256()
        with path.open('rb') as source:
            for chunk in iter(lambda: source.read(1024 * 1024), b''):
                digest.update(chunk)
        if digest.hexdigest() != expected:
            raise ValueError('bundle checksum mismatch')
    for archive in (root / 'volumes').glob('*.tar'):
        with tarfile.open(archive) as source:
            for member in source:
                path = pathlib.PurePosixPath(member.name)
                if path.is_absolute() or '..' in path.parts or not (member.isfile() or member.isdir()):
                    raise ValueError('unsafe archive entry; only ordinary files and directories supported')
except Exception as exc:
    print('restore: bundle validation failed: ' + str(exc), file=sys.stderr)
    sys.exit(1)
print('restore: complete manifest and archive validation passed')
