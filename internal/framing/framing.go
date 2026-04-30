// Package framing implements the wire format for the ee-tunnel HTTP-over-WS
// protocol. It mirrors backend/enricher/services/tunnels/framing.py exactly.
//
// All frames are JSON objects sent as WebSocket TEXT messages. See the backend
// docstring for the full frame catalog.
package framing

import (
	"encoding/json"
	"fmt"
)

const ProtocolVersion = "1"

type FrameType string

const (
	TypeHello         FrameType = "hello"
	TypeRequest       FrameType = "request"
	TypeResponseStart FrameType = "response_start"
	TypeResponseChunk FrameType = "response_chunk"
	TypeResponseEnd   FrameType = "response_end"
	TypeError         FrameType = "error"
	TypeCancel        FrameType = "cancel"
	TypePing          FrameType = "ping"
	TypePong          FrameType = "pong"
)

// Generic envelope used to peek at a frame's type before unmarshalling.
type envelope struct {
	Type FrameType `json:"type"`
}

type Hello struct {
	Type    FrameType `json:"type"`
	Version string    `json:"version"`
	Agent   string    `json:"agent"`
	OS      string    `json:"os"`
	Arch    string    `json:"arch"`
}

type Request struct {
	Type    FrameType         `json:"type"`
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	BodyB64 string            `json:"body_b64"`
}

type ResponseStart struct {
	Type    FrameType         `json:"type"`
	ID      string            `json:"id"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
}

type ResponseChunk struct {
	Type   FrameType `json:"type"`
	ID     string    `json:"id"`
	DataB64 string   `json:"data_b64"`
}

type ResponseEnd struct {
	Type FrameType `json:"type"`
	ID   string    `json:"id"`
}

type ErrorFrame struct {
	Type    FrameType `json:"type"`
	ID      string    `json:"id,omitempty"`
	Code    string    `json:"code"`
	Message string    `json:"message"`
}

type Cancel struct {
	Type FrameType `json:"type"`
	ID   string    `json:"id"`
}

type Ping struct {
	Type FrameType `json:"type"`
}

type Pong struct {
	Type FrameType `json:"type"`
}

// Frame is the discriminated union of all frame types.
type Frame interface {
	frameType() FrameType
}

func (Hello) frameType() FrameType         { return TypeHello }
func (Request) frameType() FrameType       { return TypeRequest }
func (ResponseStart) frameType() FrameType { return TypeResponseStart }
func (ResponseChunk) frameType() FrameType { return TypeResponseChunk }
func (ResponseEnd) frameType() FrameType   { return TypeResponseEnd }
func (ErrorFrame) frameType() FrameType    { return TypeError }
func (Cancel) frameType() FrameType        { return TypeCancel }
func (Ping) frameType() FrameType          { return TypePing }
func (Pong) frameType() FrameType          { return TypePong }

// Encode marshals a frame to JSON, ensuring the type discriminator is set.
func Encode(f Frame) ([]byte, error) {
	switch v := f.(type) {
	case Hello:
		v.Type = TypeHello
		return json.Marshal(v)
	case Request:
		v.Type = TypeRequest
		return json.Marshal(v)
	case ResponseStart:
		v.Type = TypeResponseStart
		return json.Marshal(v)
	case ResponseChunk:
		v.Type = TypeResponseChunk
		return json.Marshal(v)
	case ResponseEnd:
		v.Type = TypeResponseEnd
		return json.Marshal(v)
	case ErrorFrame:
		v.Type = TypeError
		return json.Marshal(v)
	case Cancel:
		v.Type = TypeCancel
		return json.Marshal(v)
	case Ping:
		v.Type = TypePing
		return json.Marshal(v)
	case Pong:
		v.Type = TypePong
		return json.Marshal(v)
	default:
		return nil, fmt.Errorf("framing: unknown frame type %T", f)
	}
}

// Decode parses a frame from JSON, returning the appropriate concrete type.
func Decode(data []byte) (Frame, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("framing: malformed envelope: %w", err)
	}
	switch env.Type {
	case TypeHello:
		var f Hello
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, err
		}
		return f, nil
	case TypeRequest:
		var f Request
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, err
		}
		return f, nil
	case TypeResponseStart:
		var f ResponseStart
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, err
		}
		return f, nil
	case TypeResponseChunk:
		var f ResponseChunk
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, err
		}
		return f, nil
	case TypeResponseEnd:
		var f ResponseEnd
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, err
		}
		return f, nil
	case TypeError:
		var f ErrorFrame
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, err
		}
		return f, nil
	case TypeCancel:
		var f Cancel
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, err
		}
		return f, nil
	case TypePing:
		return Ping{Type: TypePing}, nil
	case TypePong:
		return Pong{Type: TypePong}, nil
	default:
		return nil, fmt.Errorf("framing: unknown frame type %q", env.Type)
	}
}
