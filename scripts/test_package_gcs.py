"""Exercise the release archive and checksum contract, including incomplete builds."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
import zipfile

spec = importlib.util.spec_from_file_location('package_gcs', Path(__file__).with_name('package-gcs.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class PackageGCS(unittest.TestCase):
    def test_qgc_platform_packages_and_checksums(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for target, suffix in [('macos-universal', '.dmg'), ('windows-amd64', '.exe'), ('linux-amd64', '.AppImage')]:
                source = root / ('QGroundControl' + suffix)
                source.write_bytes(b'packaged upstream application')
                artifact = module.package('qgroundcontrol', target, source, root / target, 'adapter-sha')
                self.assertEqual(source.read_bytes(), artifact.read_bytes())
                self.assertEqual(artifact.with_name(artifact.name + '.sha256').read_text(),
                                 f'{hashlib.sha256(artifact.read_bytes()).hexdigest()}  {artifact.name}\n')
                manifest = json.loads(next(artifact.parent.glob('*.build.json')).read_text())
                self.assertEqual(manifest['target'], target)
                self.assertEqual(manifest['adapterCommit'], 'adapter-sha')
                self.assertRegex(manifest['upstream']['commit'], '^[a-f0-9]{40}$')
            with self.assertRaises(ValueError):
                module.package('qgroundcontrol', 'windows-amd64', source, root / 'invalid', 'sha')

    def test_plugin_archive_has_single_installable_plugin_and_provenance(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for name, data in [('plugins/OpenUTMAdapter.dll', b'MZcompiled'), ('source/OpenUTMAdapter.cs', b'generated'), ('README.md', b'installation')]:
                path = root / 'input' / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(data)
            artifact = module.package('missionPlanner', 'windows-amd64', root / 'input', root / 'output', 'sha')
            with zipfile.ZipFile(artifact) as archive:
                self.assertEqual(set(archive.namelist()), {'plugins/OpenUTMAdapter.dll', 'source/OpenUTMAdapter.cs', 'README.md', 'build.json'})
                self.assertEqual(json.loads(archive.read('build.json'))['upstream']['version'], '1.3.83')
            (root / 'input/plugins/OpenUTMAdapter.dll').unlink()
            with self.assertRaises(ValueError):
                module.package('missionPlanner', 'windows-amd64', root / 'input', root / 'output', 'sha')
            with self.assertRaises(ValueError):
                module.package('missionPlanner', 'linux-amd64', root / 'input', root / 'output', 'sha')


if __name__ == '__main__':
    unittest.main()
