import json
from pathlib import Path
import tempfile
import unittest

import release

COMMIT = 'a' * 40


class ReleaseTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / 'artifacts'
        self.output = self.root / 'dist'
        for target in release.TARGETS:
            directory = self.source / release.suffix(target)
            directory.mkdir(parents=True)
            names = release.binary_names(target)
            for name in names:
                (directory / name).write_bytes(('fixture-' + name).encode())
            metadata = {
                'target': list(target), 'source_commit': COMMIT,
                'go_version': 'go version fixture',
                'sha256': {name: release.digest(directory / name) for name in names},
            }
            (directory / f'build-info-{release.suffix(target)}.json').write_text(json.dumps(metadata))

    def assemble(self):
        release.assemble(self.source, self.output, COMMIT)

    def test_preserves_existing_downloads_and_adds_windows(self):
        self.assemble()
        expected = {'misterclaw-linux-arm7', 'build-metadata.json', 'SHA256SUMS'}
        for platform in ('linux-amd64', 'linux-arm64', 'linux-arm7', 'darwin-amd64', 'darwin-arm64'):
            expected.update({f'misterclaw-send-{platform}', f'misterclaw-mcp-{platform}'})
        expected.update({'misterclaw-send-windows-amd64.exe', 'misterclaw-mcp-windows-amd64.exe'})
        self.assertEqual({path.name for path in self.output.iterdir()}, expected)
        checksums = (self.output / 'SHA256SUMS').read_text().splitlines()
        self.assertEqual(len(checksums), 14)
        for line in checksums:
            digest, name = line.split('  ')
            self.assertEqual(digest, release.digest(self.output / name))

    def test_missing_binary(self):
        (self.source / 'linux-amd64' / 'misterclaw-send-linux-amd64').unlink()
        with self.assertRaisesRegex(ValueError, 'Missing artifacts'):
            self.assemble()
        self.assertFalse(self.output.exists())

    def test_duplicate_binary(self):
        (self.source / 'duplicate').mkdir()
        (self.source / 'duplicate' / 'misterclaw-send-linux-amd64').write_bytes(b'duplicate')
        with self.assertRaisesRegex(ValueError, 'duplicate artifact'):
            self.assemble()

    def test_unexpected_binary(self):
        (self.source / 'daemon-windows.exe').write_bytes(b'not a supported daemon')
        with self.assertRaisesRegex(ValueError, 'Unexpected'):
            self.assemble()

    def test_corrupted_binary(self):
        (self.source / 'windows-amd64' / 'misterclaw-mcp-windows-amd64.exe').write_bytes(b'wrong')
        with self.assertRaisesRegex(ValueError, 'corrupted binary'):
            self.assemble()

    def test_wrong_source_commit(self):
        file = self.source / 'darwin-amd64' / 'build-info-darwin-amd64.json'
        data = json.loads(file.read_text())
        data['source_commit'] = 'b' * 40
        file.write_text(json.dumps(data))
        with self.assertRaisesRegex(ValueError, 'Wrong build provenance'):
            self.assemble()

    def test_symlink_artifact(self):
        (self.source / 'unexpected-link').symlink_to(self.source / 'linux-amd64')
        with self.assertRaisesRegex(ValueError, 'Symlink artifact'):
            self.assemble()

    def test_existing_output_not_overwritten(self):
        self.output.mkdir()
        sentinel = self.output / 'keep.txt'
        sentinel.write_text('keep')
        with self.assertRaisesRegex(ValueError, 'must be empty'):
            self.assemble()
        self.assertEqual(sentinel.read_text(), 'keep')

    def test_unsupported_target(self):
        with self.assertRaisesRegex(ValueError, 'Unsupported release target'):
            release.build(('windows', 'arm', '7'), self.output)


if __name__ == '__main__':
    unittest.main()
