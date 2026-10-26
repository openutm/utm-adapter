"""Exercise custom-layer wiring without modifying an installed ground station."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import xml.etree.ElementTree as ET

SCRIPT = Path(__file__).with_name('prepare-qgc.py')


class PrepareQGC(unittest.TestCase):
    def test_resource_wiring_and_overwrite_protection(self):
        with tempfile.TemporaryDirectory() as temporary:
            source = Path(temporary)
            (source / 'src/FlyView').mkdir(parents=True)
            (source / 'src/API').mkdir()
            (source / 'src/API/QGCCorePlugin.h').touch()
            (source / 'src/FlyView/FlyViewCustomLayer.qml').write_text('import QtQuick\nItem { id: _root; property var mapControl }')
            result = subprocess.run([sys.executable, str(SCRIPT), str(source)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            custom = source / 'custom-openutm-adapter'
            self.assertNotIn('QGC_DISABLE_', (custom / 'cmake/CustomOverrides.cmake').read_text())
            self.assertTrue((custom / 'src/OpenUTMPlugin.cc').is_file())
            self.assertIn('mapControl: _root.mapControl', (custom / 'src/FlyViewCustomLayer.qml').read_text())
            resources = ET.parse(custom / 'custom.qrc').getroot()
            for resource in resources:
                for entry in resource:
                    self.assertTrue((custom / entry.text).is_file())
            self.assertIn('QGroundControl/FlyView/AdapterRestrictions.qml', [entry.attrib['alias'] for resource in resources for entry in resource])
            self.assertEqual(subprocess.run([sys.executable, str(SCRIPT), str(source)], capture_output=True).returncode, 2)
            self.assertEqual(subprocess.run([sys.executable, str(SCRIPT), str(source), '--directory', '../escape'], capture_output=True).returncode, 2)


if __name__ == '__main__':
    unittest.main()
