#!/usr/bin/env python3
import pathlib
import shutil
import subprocess

root = pathlib.Path(__file__).resolve().parents[2]
frontend = root / 'frontend'
subprocess.run(['pnpm', '--dir', str(frontend), 'typecheck'], check=True)
subprocess.run(['pnpm', '--dir', str(frontend), 'build'], check=True)
target = frontend / 'dist-site'
target.mkdir(exist_ok=True)
for app, door in [('console','console'),('worker','worker'),('field','enrolment'),('verify','verify')]:
    source = frontend / 'apps' / app / 'dist'
    if not (source / 'index.html').is_file():
        raise SystemExit('Missing built application: ' + app)
    shutil.copytree(source, target / door, dirs_exist_ok=True)
(target / 'index.html').write_text('''<!doctype html><html lang="en"><meta charset="utf-8"><title>CREST local acceptance</title><main><h1>CREST local acceptance</h1><p>Application access requires the configured identity provider.</p><ul><li><a href="/console/">Console</a></li><li><a href="/worker/">Worker</a></li><li><a href="/enrolment/">Field</a></li><li><a href="/verify/">Verify</a></li></ul></main></html>''')
print('Published current application builds to frontend/dist-site; no business data created.')
