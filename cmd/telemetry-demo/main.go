// telemetry-demo sends explicitly synthetic, disarmed MAVLink data to a local adapter.
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"os/signal"
	"time"

	"github.com/bluenviron/gomavlib/v3/pkg/dialect"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/ardupilotmega"
	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
	"github.com/bluenviron/gomavlib/v3/pkg/frame"
	"github.com/bluenviron/gomavlib/v3/pkg/streamwriter"
)

func main() {
	address := flag.String("address", "127.0.0.1:14551", "UDP destination (adapter, or QGC on port 14550)")
	flag.Parse()
	conn, e := net.Dial("udp", *address)
	if e != nil {
		log.Fatal(e)
	}
	defer conn.Close()
	dr := &dialect.ReadWriter{Dialect: ardupilotmega.Dialect}
	if e = dr.Initialize(); e != nil {
		log.Fatal(e)
	}
	fw := &frame.Writer{ByteWriter: conn, DialectRW: dr}
	if e = fw.Initialize(); e != nil {
		log.Fatal(e)
	}
	w := &streamwriter.Writer{FrameWriter: fw, Version: streamwriter.V2, SystemID: 1, ComponentID: 1}
	if e = w.Initialize(); e != nil {
		log.Fatal(e)
	}
	fmt.Printf("DEMO ONLY · synthetic disarmed vehicle 1 → %s. Keep Flight Blender sending off for a UI-only test. Ctrl+C stops.\n", *address)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	i := 0.0
	for {
		select {
		case <-sig:
			return
		case <-ticker.C:
			lat := int32((47.4979 + math.Sin(i/20)*.0001) * 1e7)
			lon := int32((19.0402 + math.Cos(i/20)*.0001) * 1e7)
			e = w.Write(&common.MessageHeartbeat{Type: common.MAV_TYPE_QUADROTOR, Autopilot: common.MAV_AUTOPILOT_ARDUPILOTMEGA, SystemStatus: common.MAV_STATE_STANDBY, MavlinkVersion: 3})
			if e != nil {
				log.Print(e)
			}
			e = w.Write(&common.MessageGpsRawInt{TimeUsec: uint64(time.Now().UnixMicro()), FixType: common.GPS_FIX_TYPE_3D_FIX, Lat: lat, Lon: lon, Alt: 120000, AltEllipsoid: 164000, Vel: 210, Cog: 9000, SatellitesVisible: 14})
			if e != nil {
				log.Print(e)
			}
			e = w.Write(&common.MessageGlobalPositionInt{Lat: lat, Lon: lon, Alt: 120000, RelativeAlt: 20000, Vx: 210, Hdg: 9000})
			if e != nil {
				log.Print(e)
			}
			i++
		}
	}
}
