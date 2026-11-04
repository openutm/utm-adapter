package adapter

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/bluenviron/gomavlib/v3/pkg/dialects/common"
	"github.com/bluenviron/gomavlib/v3/pkg/message"
)

func (s *Service) vehicleWrite(system byte, m message.Message) error {
	s.mu.Lock()
	if s.node == nil || !s.state.Running || s.vehicleChannels[system] == nil {
		s.mu.Unlock()
		return fmt.Errorf("vehicle needs a bidirectional MAVLink connection")
	}
	node, channel := s.node, s.vehicleChannels[system]
	s.mu.Unlock()
	return node.WriteMessageTo(channel, m)
}
func (s *Service) requestController(ctx context.Context, system byte) error {
	requested := time.Now()
	if err := s.vehicleWrite(system, &common.MessageCommandLong{TargetSystem: system, TargetComponent: 1, Command: common.MAV_CMD_REQUEST_MESSAGE, Param1: 148}); err != nil {
		return err
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.mu.Lock()
		v := s.vehicles[system]
		s.mu.Unlock()
		if v.ControllerID != "" && v.ControllerAt.After(requested) && time.Since(v.HeartbeatAt) < 5*time.Second {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("vehicle identification timed out: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

type ftpError byte

func (e ftpError) Error() string { return fmt.Sprintf("MAVFTP error %d", e) }

// ftp exchanges use the standard MAVLink FTP header. The reply must match the
// target, sequence, request opcode and offset; retries reuse the same sequence.
func (s *Service) ftp(ctx context.Context, system, session, opcode byte, offset uint32, data []byte, size byte) ([251]byte, error) {
	var p [251]byte
	if len(data) > 239 {
		return p, fmt.Errorf("MAVFTP payload too large")
	}
	s.mu.Lock()
	s.ftpSequence += 2
	seq := s.ftpSequence
	replies := make(chan [251]byte, 8)
	s.ftpReplies = replies
	s.ftpSystem = system
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.ftpReplies = nil; s.mu.Unlock() }()
	binary.LittleEndian.PutUint16(p[:2], seq)
	p[2] = session
	p[3] = opcode
	p[4] = size
	binary.LittleEndian.PutUint32(p[8:12], offset)
	copy(p[12:], data)
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		if opcode == 6 || opcode == 7 || opcode == 8 || opcode == 9 {
			if !s.trustedFresh(s.trusted) {
				return p, fmt.Errorf("verified vehicle disconnected during MAVFTP transfer")
			}
			s.mu.Lock()
			armed := s.vehicles[system].Armed
			s.mu.Unlock()
			if armed {
				return p, fmt.Errorf("vehicle armed during MAVFTP transfer")
			}
		}
		if err := s.vehicleWrite(system, &common.MessageFileTransferProtocol{TargetSystem: system, TargetComponent: 1, Payload: p}); err != nil {
			return p, err
		}
		timer := time.NewTimer(750 * time.Millisecond)
	wait:
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				return p, ctx.Err()
			case <-timer.C:
				break wait
			case r := <-replies:
				if binary.LittleEndian.Uint16(r[:2]) != seq+1 || r[5] != opcode || r[4] > 239 {
					continue
				}
				if opcode == 5 || opcode == 7 {
					if binary.LittleEndian.Uint32(r[8:12]) != offset || r[2] != session {
						continue
					}
				}
				if r[3] == 129 {
					timer.Stop()
					if r[4] < 1 {
						return p, fmt.Errorf("invalid MAVFTP NAK")
					}
					return r, ftpError(r[12])
				}
				if r[3] == 128 {
					timer.Stop()
					return r, nil
				}
			}
		}
	}
	return p, fmt.Errorf("MAVFTP request timed out")
}
func (s *Service) ftpClose(system, session byte) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = s.ftp(ctx, system, session, 1, 0, nil, 0)
}
func (s *Service) ftpUpload(ctx context.Context, system byte, path string, data []byte) error {
	if len(path) > 238 {
		return fmt.Errorf("vehicle path is too long")
	}
	r, err := s.ftp(ctx, system, 0, 6, 0, append([]byte(path), 0), byte(len(path)+1))
	if err != nil {
		return err
	}
	session := r[2]
	defer s.ftpClose(system, session)
	for off := 0; off < len(data); {
		n := min(239, len(data)-off)
		if _, err = s.ftp(ctx, system, session, 7, uint32(off), data[off:off+n], byte(n)); err != nil {
			return err
		}
		off += n
	}
	return nil
}
func (s *Service) ftpDownload(ctx context.Context, system byte, path string) ([]byte, error) {
	if len(path) > 238 {
		return nil, fmt.Errorf("vehicle path is too long")
	}
	r, err := s.ftp(ctx, system, 0, 4, 0, append([]byte(path), 0), byte(len(path)+1))
	if err != nil {
		return nil, err
	}
	session := r[2]
	defer s.ftpClose(system, session)
	if r[4] != 4 {
		return nil, fmt.Errorf("invalid MAVFTP open response")
	}
	length := binary.LittleEndian.Uint32(r[12:16])
	if length > 8<<20 {
		return nil, fmt.Errorf("vehicle file exceeds 8 MiB")
	}
	out := make([]byte, 0, length)
	for uint32(len(out)) < length {
		n := byte(min(uint32(239), length-uint32(len(out))))
		r, err = s.ftp(ctx, system, session, 5, uint32(len(out)), nil, n)
		if err != nil {
			return nil, err
		}
		if r[4] == 0 || r[4] > n {
			return nil, fmt.Errorf("invalid MAVFTP read size")
		}
		out = append(out, r[12:12+int(r[4])]...)
	}
	return out, nil
}
func (s *Service) trustedDirectory(ctx context.Context, system byte) error {
	_, err := s.ftp(ctx, system, 0, 9, 0, []byte("trusted_flight\x00"), 15)
	if errors.Is(err, ftpError(8)) {
		return nil
	}
	return err
}
