import QtQuick
import QtTest
import ".."

TestCase {
    name: "AdapterRestrictions"
    QtObject {
        id: map
        property var items: []
        function addMapItem(item) {
            items.push(item);
        }
        function removeMapItem(item) {
            items = items.filter(i => i !== item);
        }
    }
    AdapterRestrictions {
        id: overlay
        mapControl: map
        endpoint: "http://127.0.0.1:1"
    }
    function polygon() {
        return {
            type: "Feature",
            geometry: {
                type: "Polygon",
                coordinates: [[[19, 47], [20, 47], [20, 48], [19, 47]]]
            },
            properties: {
                name: "Airport"
            }
        };
    }
    function test_render_and_cache() {
        overlay.showMap({
            type: "FeatureCollection",
            features: [polygon()],
            stale: false
        });
        compare(map.items.length, 1);
        compare(map.items[0].path.length, 4);
        compare(map.items[0].path[0].latitude, 47);
        compare(map.items[0].path[0].longitude, 19);
        verify(!overlay.stale);
        const invalid = polygon();
        invalid.geometry.coordinates[0][0] = [400, 400];
        let rejected = false;
        try {
            overlay.showMap({
                type: "FeatureCollection",
                features: [invalid],
                stale: false
            });
        } catch (e) {
            rejected = true;
        }
        verify(rejected);
        compare(map.items.length, 1); // Preserve last usable map on invalid updates.
        const multi = polygon();
        multi.geometry = {
            type: "MultiPolygon",
            coordinates: [multi.geometry.coordinates, multi.geometry.coordinates]
        };
        overlay.showMap({
            type: "FeatureCollection",
            features: [multi],
            stale: true
        });
        compare(map.items.length, 2);
        verify(overlay.stale);
        overlay.showMap({
            type: "FeatureCollection",
            features: [],
            stale: false
        });
        compare(map.items.length, 0);
    }
}
