pragma ComponentBehavior: Bound
import QtQuick
import QtQuick.Controls
import QtLocation
import QtPositioning

// Embed in a QGC FlyViewCustomLayer and set mapControl to its mapControl.
Item {
    id: root
    required property var mapControl
    property string endpoint: "http://127.0.0.1:14552/v1/restrictions"
    property var polygons: []
    property bool stale: true
    property bool fetching: false
    property string detail: "Waiting for airspace restrictions"

    function clearPolygons() {
        for (const polygon of polygons) {
            mapControl.removeMapItem(polygon);
            polygon.destroy();
        }
        polygons = [];
    }
    function showMap(document) {
        if (document.type !== "FeatureCollection" || !Array.isArray(document.features))
            throw new Error("Invalid restriction map");
        const next = [];
        try {
            for (const feature of document.features) {
                const geometry = feature.geometry;
                if (!geometry || !["Polygon", "MultiPolygon"].includes(geometry.type))
                    throw new Error("Unsupported restriction geometry");
                const areas = geometry.type === "Polygon" ? [geometry.coordinates] : geometry.coordinates;
                for (const area of areas) {
                    const path = area[0].map(p => QtPositioning.coordinate(p[1], p[0]));
                    if (path.length < 4 || path.some(p => !p.isValid))
                        throw new Error("Invalid restriction polygon");
                    // Fill the exterior conservatively, including any interior holes.
                    next.push(polygonComponent.createObject(root, {
                        path: path
                    }));
                }
            }
        } catch (e) {
            for (const polygon of next)
                polygon.destroy();
            throw e;
        }
        clearPolygons();
        polygons = next;
        for (const polygon of polygons)
            mapControl.addMapItem(polygon);
        stale = document.stale === true;
        detail = `${polygons.length} airspace boundaries · ${stale ? 'cached / stale' : 'current'} · check published altitude and validity limits`;
    }
    function refresh() {
        if (fetching || !mapControl)
            return;
        fetching = true;
        const request = new XMLHttpRequest();
        request.onreadystatechange = function () {
            if (request.readyState !== XMLHttpRequest.DONE)
                return;
            fetching = false;
            try {
                if (request.status !== 200)
                    throw new Error("Adapter unavailable");
                showMap(JSON.parse(request.responseText));
            } catch (e) {
                stale = true;
                detail = "Airspace feed unavailable · cached boundaries retained";
            }
        };
        request.open("GET", endpoint);
        request.send();
    }
    Component {
        id: polygonComponent
        MapPolygon {
            color: root.stale ? "#30ffb300" : "#30ff3030"
            border.color: root.stale ? "#ffb300" : "#ff3030"
            border.width: 2
        }
    }
    Label {
        anchors.top: parent.top
        anchors.horizontalCenter: parent.horizontalCenter
        text: root.detail
        color: root.stale ? "#ffb300" : "white"
        background: Rectangle {
            color: "#cc202020"
        }
        padding: 8
    }
    Timer {
        interval: 10000
        running: true
        repeat: true
        triggeredOnStart: true
        onTriggered: root.refresh()
    }
    Component.onDestruction: clearPolygons()
}
