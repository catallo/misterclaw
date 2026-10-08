#!/usr/bin/env python3
"""Build the supported downloads and fail-closed when assembling CI artifacts."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess

TARGETS = (
    ('linux', 'amd64', ''), ('linux', 'arm64', ''), ('linux', 'arm', '7'),
    ('darwin', 'amd64', ''), ('darwin', 'arm64', ''), ('windows', 'amd64', ''),
)
CLIENTS = ('misterclaw-send', 'misterclaw-mcp')


def suffix(target):
    goos, goarch, goarm = target
    return f'{goos}-{goarch}{goarm}'


def binary_names(target):
    extension = '.exe' if target[0] == 'windows' else ''
    names = [f'{command}-{suffix(target)}{extension}' for command in CLIENTS]
    if target == ('linux', 'arm', '7'):
        names.append('misterclaw-linux-arm7')
    return names


def digest(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def new_directory(path):
    path.mkdir(parents=True, exist_ok=True)
    if any(path.iterdir()):
        raise ValueError(f'Output directory must be empty: {path}')


def build(target, output):
    if target not in TARGETS:
        raise ValueError(f'Unsupported release target: {target}')
    new_directory(output)
    env = os.environ.copy()
    env.update(GOOS=target[0], GOARCH=target[1], CGO_ENABLED='0')
    env.pop('GOARM', None)
    if target[2]:
        env['GOARM'] = target[2]
    commands = list(CLIENTS)
    if target == ('linux', 'arm', '7'):
        commands.append('misterclaw')
    for command, name in zip(commands, binary_names(target)):
        subprocess.run(['go', 'build', '-trimpath', '-ldflags=-s -w', '-o', str(output / name), f'./cmd/{command}/'], env=env, check=True)
    metadata = {
        'target': list(target),
        'source_commit': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
        'go_version': subprocess.check_output(['go', 'version'], text=True).strip(),
        'sha256': {name: digest(output / name) for name in binary_names(target)},
    }
    (output / f'build-info-{suffix(target)}.json').write_text(json.dumps(metadata, indent=2) + '\n', encoding='utf-8')


def assemble(source, output, commit):
    if not re.fullmatch(r'[0-9a-f]{40}', commit):
        raise ValueError('Expected source commit must be a full Git SHA-1')
    expected = {name for target in TARGETS for name in binary_names(target)}
    metadata_names = {f'build-info-{suffix(target)}.json' for target in TARGETS}
    all_names = expected | metadata_names
    files = {}
    for path in source.rglob('*'):
        if path.is_symlink():
            raise ValueError(f'Symlink artifact is not allowed: {path}')
        if not path.is_file():
            continue
        if path.name not in all_names or path.name in files:
            raise ValueError(f'Unexpected or duplicate artifact: {path.name}')
        files[path.name] = path
    if set(files) != all_names:
        raise ValueError(f'Missing artifacts: {sorted(all_names - set(files))}')
    builds = []
    for target in TARGETS:
        metadata = json.loads(files[f'build-info-{suffix(target)}.json'].read_text(encoding='utf-8'))
        if metadata['target'] != list(target) or metadata['source_commit'] != commit:
            raise ValueError(f'Wrong build provenance: {target}')
        if not isinstance(metadata.get('go_version'), str) or not metadata['go_version'].strip():
            raise ValueError(f'Missing compiler version: {target}')
        if set(metadata['sha256']) != set(binary_names(target)):
            raise ValueError(f'Wrong build inventory: {target}')
        for name in binary_names(target):
            if files[name].stat().st_size == 0 or digest(files[name]) != metadata['sha256'][name]:
                raise ValueError(f'Empty or corrupted binary: {name}')
        builds.append(metadata)
    new_directory(output)
    for name in sorted(expected):
        shutil.copyfile(files[name], output / name)
        (output / name).chmod(0o644 if name.endswith('.exe') else 0o755)
    (output / 'build-metadata.json').write_text(json.dumps({'source_commit': commit, 'builds': builds}, indent=2) + '\n', encoding='utf-8')
    checksum_names = sorted(expected | {'build-metadata.json'})
    (output / 'SHA256SUMS').write_text(''.join(f'{digest(output / name)}  {name}\n' for name in checksum_names), encoding='utf-8')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    builder = commands.add_parser('build')
    builder.add_argument('--goos', required=True)
    builder.add_argument('--goarch', required=True)
    builder.add_argument('--goarm', default='')
    builder.add_argument('--output', type=Path, required=True)
    collector = commands.add_parser('assemble')
    collector.add_argument('--input', type=Path, required=True)
    collector.add_argument('--output', type=Path, required=True)
    collector.add_argument('--commit', required=True)
    args = parser.parse_args()
    if args.command == 'build':
        build((args.goos, args.goarch, args.goarm), args.output)
    else:
        assemble(args.input, args.output, args.commit)


if __name__ == '__main__':
    main()
