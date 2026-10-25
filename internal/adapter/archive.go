package adapter

import (
	"encoding/json"
	"fmt"
	"time"
)

// The shared Flight Blender archive contract. Local reports and Aerobridge keep
// their existing formats; conversion is confined to this transport boundary.
type archiveSession struct {
	SchemaVersion    int               `json:"schemaVersion"`
	ID               string            `json:"id"`
	Source           string            `json:"source"`
	SourceData       map[string]any    `json:"sourceData"`
	Assignment       archiveAssignment `json:"assignment"`
	StartedAt        time.Time         `json:"startedAt"`
	EndedAt          *time.Time        `json:"endedAt"`
	Revision         int64             `json:"revision"`
	FinalSampleCount *int64            `json:"finalSampleCount"`
}
type archiveAssignment struct {
	DeclarationID string `json:"declarationID"`
	VehicleID     string `json:"vehicleID"`
	OperatorID    string `json:"operatorID"`
	Pilot         Pilot  `json:"pilot"`
}
type archiveSample struct {
	Sequence   int64          `json:"sequence"`
	RecordedAt time.Time      `json:"recordedAt"`
	Position   map[string]any `json:"position"`
	SourceData Vehicle        `json:"sourceData"`
}
type archiveBatch struct {
	Session archiveSession  `json:"session"`
	Samples []archiveSample `json:"samples"`
	Archive *struct {
		Complete            bool   `json:"complete"`
		ReceivedSampleCount int64  `json:"receivedSampleCount"`
		FinalSampleCount    *int64 `json:"finalSampleCount"`
	} `json:"archive,omitempty"`
}

func (r *Records) archive(b SessionBatch) (archiveBatch, error) {
	s, a := b.Session, b.Session.Assignment
	result := archiveBatch{
		Session: archiveSession{SchemaVersion: 2, ID: s.ID, Source: "mavlink", Assignment: archiveAssignment{a.DeclarationID, a.VehicleID, a.OperatorID, a.Pilot}, StartedAt: s.StartedAt, EndedAt: s.EndedAt, Revision: s.Revision,
			SourceData: map[string]any{"systemID": a.SystemID, "aerobridgeOperationID": a.AerobridgeOperationID, "blenderURL": s.BlenderURL, "aerobridgeURL": s.AerobridgeURL}},
		Samples: []archiveSample{},
	}
	if s.EndedAt != nil {
		var count int64
		if err := r.db.QueryRow("SELECT COUNT(*) FROM samples WHERE session_id=?", s.ID).Scan(&count); err != nil {
			return result, err
		}
		result.Session.FinalSampleCount = &count
	}
	for _, sample := range b.Samples {
		v := sample.Vehicle
		if v.Latitude == nil || v.Longitude == nil {
			return result, fmt.Errorf("archive sample requires a position")
		}
		result.Samples = append(result.Samples, archiveSample{Sequence: sample.Sequence - 1, RecordedAt: v.GPSAt,
			Position: map[string]any{"latitude": v.Latitude, "longitude": v.Longitude, "altitudeWGS84": v.AltitudeWGS84, "relativeAltitude": v.RelativeAltitude, "speed": v.Speed, "heading": v.Heading, "motorsOn": v.Armed, "satellites": v.Satellites}, SourceData: v})
	}
	return result, nil
}

func validateArchiveReceipt(raw []byte, b archiveBatch) error {
	var receipt struct {
		ID      string `json:"id"`
		Archive *struct {
			Complete            bool   `json:"complete"`
			ReceivedSampleCount int64  `json:"receivedSampleCount"`
			FinalSampleCount    *int64 `json:"finalSampleCount"`
		} `json:"archive"`
	}
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return fmt.Errorf("invalid archive acknowledgement: %w", err)
	}
	if receipt.ID != b.Session.ID || receipt.Archive == nil {
		return fmt.Errorf("server did not acknowledge the shared session archive API")
	}
	if final := b.Session.FinalSampleCount; final != nil && (len(b.Samples) == 0 || b.Samples[len(b.Samples)-1].Sequence+1 == *final) {
		if !receipt.Archive.Complete || receipt.Archive.FinalSampleCount == nil || *receipt.Archive.FinalSampleCount != *final || receipt.Archive.ReceivedSampleCount != *final {
			return fmt.Errorf("server archive is incomplete; local records retained")
		}
	}

	return nil
}
