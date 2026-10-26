"""Regression coverage for AppImage/Xvfb launcher cleanup."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest

from test_qgc_package import stop_process_tree


class ProcessTreeCleanupTest(unittest.TestCase):
    @unittest.skipIf(os.name == 'nt', 'POSIX process-group regression')
    def test_cleanup_stops_child_after_launcher_exits(self):
        with tempfile.TemporaryDirectory() as temporary:
            heartbeat = Path(temporary) / 'heartbeat'
            child = (
                'import pathlib, signal, sys, time; '
                'signal.signal(signal.SIGTERM, signal.SIG_IGN); '
                'path = pathlib.Path(sys.argv[1]); '
                'exec("while True:\\n path.write_text(str(time.monotonic_ns()))\\n time.sleep(0.01)")'
            )
            launcher = (
                'import pathlib, subprocess, sys, time; '
                'subprocess.Popen([sys.executable, "-c", sys.argv[1], sys.argv[2]]); '
                'path = pathlib.Path(sys.argv[2]); '
                'exec("while not path.exists():\\n time.sleep(0.01)")'
            )
            process = subprocess.Popen([sys.executable, '-c', launcher, child, str(heartbeat)],
                                       start_new_session=True)
            try:
                process.wait(timeout=5)
                self.assertTrue(heartbeat.exists(), 'child must be running before cleanup')
                stop_process_tree(process)
                last_write = heartbeat.read_text()
                time.sleep(0.2)
                self.assertEqual(last_write, heartbeat.read_text(), 'orphaned child kept writing')
            finally:
                stop_process_tree(process)


if __name__ == '__main__':
    unittest.main()
