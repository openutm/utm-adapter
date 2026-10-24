#!/usr/bin/env python3
"""Prepare a separate native QGC custom directory; never replace an existing one."""
import argparse
from pathlib import Path
import shutil
import xml.etree.ElementTree as ET

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('qgc_source', type=Path)
parser.add_argument('--directory', default='custom-openutm-adapter')
args = parser.parse_args()
source = args.qgc_source.resolve()
destination = source / args.directory
if destination.parent != source or destination.exists():
    parser.error('choose a new directory directly inside the QGC source checkout')
root = Path(__file__).resolve().parent.parent
original = source / 'src/FlyView/FlyViewCustomLayer.qml'
if not original.is_file() or not (source / 'src/API/QGCCorePlugin.h').is_file():
    parser.error('this integration requires the QGC 5.1 FlyViewCustomLayer / core plugin API')
shutil.copytree(root / 'integrations/qgroundcontrol/custom', destination)
shutil.copy(root / 'integrations/qgroundcontrol/AdapterRestrictions.qml', destination / 'src')
layer = original.read_text()
closing = layer.rfind('}')
layer = layer[:closing] + '\n    AdapterRestrictions { anchors.fill: parent; mapControl: _root.mapControl }\n' + layer[closing:]
(destination / 'src/FlyViewCustomLayer.qml').write_text(layer)
qrc = destination / 'custom.qrc'
tree = ET.parse(qrc)
for resource in tree.getroot():
    for entry in resource:
        if not (destination / entry.text).is_file():
            parser.error(f'missing custom resource: {entry.text}')
print(f'Prepared {destination}. Configure QGC with -DQGC_CUSTOM_DIR={args.directory}')
