#!/usr/bin/env python3
"""Boot a packaged QGC and require its custom overlay to fetch restrictions."""
import argparse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time



def stop_process_tree(process):
    """Stop wrappers and their children before deleting the app's profile."""
    if os.name == 'nt':
        # Terminating only QGC's launcher can leave the installed app running.
        subprocess.run(['taskkill', '/PID', str(process.pid), '/T', '/F'],
                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=False)
    else:
        try:
            os.killpg(process.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            pass
        # xvfb-run can exit before its AppImage children. Kill the entire group
        # even when the original Popen process has already finished.
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    process.wait(timeout=5)
    if os.name != 'nt':
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            try:
                os.killpg(process.pid, 0)
            except ProcessLookupError:
                break
            time.sleep(0.05)


def verify(source, binary, architectures='', build_directory=''):
    requested = threading.Event()
    document = json.dumps({'type': 'FeatureCollection', 'stale': False, 'features': [
        {'type': 'Feature', 'properties': {}, 'geometry': {'type': 'Polygon',
         'coordinates': [[[19, 47], [19.01, 47], [19.01, 47.01], [19, 47]]]}}
    ]}).encode('utf-8')

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path != '/v1/restrictions':
                self.send_error(404)
                return
            self.send_response(200)
            self.send_header('Content-Type', 'application/geo+json')
            self.send_header('Access-Control-Allow-Origin', '*')
            self.send_header('Content-Length', str(len(document)))
            self.end_headers()
            self.wfile.write(document)
            requested.set()

        def log_message(self, *_args):
            pass

    # Use the shipped default endpoint, so a disconnected overlay fails this test.
    with ThreadingHTTPServer(('127.0.0.1', 14552), Handler) as server:
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            environment = dict(os.environ, GITHUB_WORKSPACE=str(source.resolve()))
            subprocess.run([
                sys.executable, str(source / '.github/scripts/verify_executable.py'),
                '--binary-path', str(binary.resolve()), '--expect-archs', architectures,
                '--build-dir', build_directory, '--timeout', '60'
            ], env=environment, check=True)
            # Upstream's boot test exits before app.exec(), so asynchronous
            # HTTP needs a second launch with the real event loop running.
            with tempfile.TemporaryDirectory() as temporary:
                environment.update({name: str(Path(temporary) / name) for name in
                                    ['XDG_CONFIG_HOME', 'XDG_CACHE_HOME', 'XDG_DATA_HOME']})
                command = [str(binary.resolve()), '--allow-multiple']
                if sys.platform.startswith('linux') and binary.suffix == '.AppImage':
                    command = ['xvfb-run', '-a', *command]
                    if not shutil.which('xvfb-run'):
                        raise RuntimeError('xvfb-run is required to test the packaged AppImage')
                else:
                    environment['QT_QPA_PLATFORM'] = 'offscreen'
                with tempfile.TemporaryFile(mode='w+b') as log:
                    process = subprocess.Popen(command, env=environment, stdout=log,
                                               stderr=subprocess.STDOUT, start_new_session=os.name != 'nt')
                    try:
                        deadline = time.monotonic() + 45
                        while not requested.wait(0.2) and process.poll() is None and time.monotonic() < deadline:
                            pass
                        if not requested.is_set():
                            raise RuntimeError('Packaged QGC did not request the custom restriction endpoint')
                    finally:
                        stop_process_tree(process)
                        log.seek(0)
                        print(log.read().decode('utf-8', errors='replace'))
        finally:
            server.shutdown()
            thread.join()
    print('Packaged QGC booted and its restriction overlay fetched the integration feed')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--qgc-source', type=Path, required=True)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--expect-archs', default='')
    parser.add_argument('--build-dir', default='')
    arguments = parser.parse_args()
    verify(arguments.qgc_source, arguments.binary, arguments.expect_archs, arguments.build_dir)
