package protocol

import (
	"encoding/json"
	"fmt"
)

// WebSocket frame opcodes (RFC 6455) used to select the envelope encoding.
// Kept as literals so this package stays free of the websocket dependency.
const (
	FrameText   = 1 // legacy JSON envelope
	FrameBinary = 2 // protobuf WireEnvelope
)

// Inbound is a decoded transport frame. Exactly one of the payload carriers
// is populated: Payload for legacy JSON frames, Decoded for protobuf frames
// (already converted to the domain type named by MsgType).
type Inbound struct {
	// Opcode records the framing of the source frame (FrameText/FrameBinary),
	// letting peers infer the other side's encoding support from what it
	// actually sent instead of trusting an advertised flag.
	Opcode  int
	MsgType string
	Payload json.RawMessage
	Decoded any
}

// DecodeFrame turns a raw WebSocket frame into an Inbound. Both encodings
// are accepted regardless of connection state: every frame is
// self-describing by its opcode, which makes the negotiated upgrade of a
// live connection race-free.
func DecodeFrame(opcode int, raw []byte) (Inbound, error) {
	switch opcode {
	case FrameText:
		var env Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return Inbound{}, err
		}
		return Inbound{Opcode: opcode, MsgType: env.Type, Payload: env.Payload}, nil
	case FrameBinary:
		msgType, decoded, err := UnmarshalBinaryEnvelope(raw)
		if err != nil {
			return Inbound{}, err
		}
		return Inbound{Opcode: opcode, MsgType: msgType, Decoded: decoded}, nil
	default:
		return Inbound{}, fmt.Errorf("wire: unsupported frame opcode %d", opcode)
	}
}

// PayloadOf extracts the typed domain payload from an Inbound, decoding the
// JSON branch on demand.
func PayloadOf[T any](in Inbound) (T, error) {
	if in.Decoded != nil {
		v, ok := in.Decoded.(T)
		if !ok {
			return *new(T), fmt.Errorf("wire: %s payload is %T, want %T", in.MsgType, in.Decoded, *new(T))
		}
		return v, nil
	}
	return UnmarshalPayload[T](Envelope{Type: in.MsgType, Payload: in.Payload})
}
