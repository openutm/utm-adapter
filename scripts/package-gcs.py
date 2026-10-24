#!/usr/bin/env python3
"""Stage a verified GCS build with provenance and SHA-256 download checksums."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import zipfile

ROOT = Path(__file__).resolve().parent.parent


def package(component, target, source, output, commit):
    pins = json.loads((ROOT / 'build/gcs-versions.json').read_text())
    version = json.loads((ROOT / 'frontend/package.json').read_text())['version']
    upstream = pins[component]
    manifest = {'adapterVersion': version, 'adapterCommit': commit,
                'component': component, 'target': target, 'upstream': upstream}
    output.mkdir(parents=True, exist_ok=True)
    stem = f'utm-adapter-gcs-{component}-{upstream["version"]}-{target}'
    if component == 'qgroundcontrol':
        suffix = {'macos-universal': '.dmg', 'windows-amd64': '.exe',
                  'linux-amd64': '.AppImage'}[target]
        if source.suffix != suffix or not source.is_file() or source.stat().st_size == 0:
            raise ValueError(f'expected a nonempty {suffix} package: {source}')
        artifact = output / (stem + suffix)
        shutil.copyfile(source, artifact)
    else:
        if target != 'windows-amd64':
            raise ValueError('Mission Planner plugin requires windows-amd64')
        files = ['plugins/OpenUTMAdapter.dll', 'source/OpenUTMAdapter.cs', 'README.md']
        for name in files:
            if not (source / name).is_file() or (source / name).stat().st_size == 0:
                raise ValueError(f'missing plugin package member: {name}')
        if (source / files[0]).read_bytes()[:2] != b'MZ':
            raise ValueError('plugin must be a compiled Windows DLL')
        artifact = output / (stem + '.zip')
        with zipfile.ZipFile(artifact, 'w', zipfile.ZIP_DEFLATED) as archive:
            for name in files:
                archive.write(source / name, name)
            archive.writestr('build.json', json.dumps(manifest, indent=2) + '\n')
    provenance = output / (stem + '.build.json')
    provenance.write_text(json.dumps(manifest, indent=2) + '\n')
    for path in [artifact, provenance]:
        with path.open('rb') as stream:
            checksum = hashlib.sha256()
            for chunk in iter(lambda: stream.read(1024 * 1024), b''):
                checksum.update(chunk)
            digest = checksum.hexdigest()
        path.with_name(path.name + '.sha256').write_text(f'{digest}  {path.name}\n')
    return artifact


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--component', choices=['qgroundcontrol', 'missionPlanner'], required=True)
    parser.add_argument('--target', required=True)
    parser.add_argument('--input', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--adapter-commit', required=True)
    args = parser.parse_args()
    print(package(args.component, args.target, args.input, args.output, args.adapter_commit))
