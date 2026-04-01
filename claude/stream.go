package claude

import (
	"encoding/json"
	"io"

	agentsdk "github.com/chenhg5/go-agent-sdk"
	"github.com/chenhg5/go-agent-sdk/internal/sse"
)

type stream struct {
	reader *sse.Reader
	body   io.ReadCloser
	closed bool
}

var _ agentsdk.Stream = (*stream)(nil)

func newStream(body io.ReadCloser) *stream {
	return &stream{reader: sse.NewReader(body), body: body}
}

func (s *stream) Recv() (agentsdk.StreamEvent, error) {
	if s.closed {
		return agentsdk.StreamEvent{}, io.EOF
	}
	for {
		raw, err := s.reader.Next()
		if err != nil {
			return agentsdk.StreamEvent{}, err
		}
		evt, skip, err := parseRawEvent(raw)
		if err != nil {
			return agentsdk.StreamEvent{}, err
		}
		if skip {
			continue
		}
		return evt, nil
	}
}

func (s *stream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.body.Close()
}

// ---------------------------------------------------------------------------
// SSE → StreamEvent
// ---------------------------------------------------------------------------

func parseRawEvent(raw *sse.Event) (agentsdk.StreamEvent, bool, error) {
	evtType := raw.Type
	if evtType == "" {
		evtType = "message"
	}

	switch evtType {
	case "message_start":
		return parseMessageStart(raw.Data)
	case "content_block_start":
		return parseContentBlockStart(raw.Data)
	case "content_block_delta":
		return parseContentBlockDelta(raw.Data)
	case "content_block_stop":
		return parseContentBlockStop(raw.Data)
	case "message_delta":
		return parseMessageDelta(raw.Data)
	case "message_stop":
		return agentsdk.StreamEvent{Type: agentsdk.StreamEventMessageStop}, false, nil
	case "ping":
		return agentsdk.StreamEvent{Type: agentsdk.StreamEventPing}, true, nil
	default:
		return agentsdk.StreamEvent{}, true, nil
	}
}

type messageStartPayload struct {
	Message struct {
		ID    string         `json:"id"`
		Type  string         `json:"type"`
		Role  string         `json:"role"`
		Model string         `json:"model"`
		Usage agentsdk.Usage `json:"usage"`
	} `json:"message"`
}

func parseMessageStart(data string) (agentsdk.StreamEvent, bool, error) {
	var p messageStartPayload
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return agentsdk.StreamEvent{}, false, err
	}
	return agentsdk.StreamEvent{
		Type: agentsdk.StreamEventMessageStart,
		Message: &agentsdk.MessageResponse{
			ID: p.Message.ID, Type: p.Message.Type,
			Role: agentsdk.Role(p.Message.Role), Model: p.Message.Model,
			Usage: p.Message.Usage,
		},
	}, false, nil
}

type contentBlockStartPayload struct {
	Index        int `json:"index"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id,omitempty"`
		Name string `json:"name,omitempty"`
		Text string `json:"text,omitempty"`
	} `json:"content_block"`
}

func parseContentBlockStart(data string) (agentsdk.StreamEvent, bool, error) {
	var p contentBlockStartPayload
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return agentsdk.StreamEvent{}, false, err
	}
	return agentsdk.StreamEvent{
		Type: agentsdk.StreamEventContentStart, Index: p.Index,
		Block: &agentsdk.ContentBlock{
			Type: p.ContentBlock.Type, ID: p.ContentBlock.ID,
			Name: p.ContentBlock.Name, Text: p.ContentBlock.Text,
		},
	}, false, nil
}

type contentBlockDeltaPayload struct {
	Index int `json:"index"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text,omitempty"`
		PartialJSON string `json:"partial_json,omitempty"`
		Thinking    string `json:"thinking,omitempty"`
	} `json:"delta"`
}

func parseContentBlockDelta(data string) (agentsdk.StreamEvent, bool, error) {
	var p contentBlockDeltaPayload
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return agentsdk.StreamEvent{}, false, err
	}
	return agentsdk.StreamEvent{
		Type: agentsdk.StreamEventContentDelta, Index: p.Index,
		Delta: &agentsdk.Delta{
			Type: p.Delta.Type, Text: p.Delta.Text,
			JSON: p.Delta.PartialJSON, Thinking: p.Delta.Thinking,
		},
	}, false, nil
}

type contentBlockStopPayload struct {
	Index int `json:"index"`
}

func parseContentBlockStop(data string) (agentsdk.StreamEvent, bool, error) {
	var p contentBlockStopPayload
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return agentsdk.StreamEvent{}, false, err
	}
	return agentsdk.StreamEvent{Type: agentsdk.StreamEventContentStop, Index: p.Index}, false, nil
}

type messageDeltaPayload struct {
	Delta struct {
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage agentsdk.Usage `json:"usage"`
}

func parseMessageDelta(data string) (agentsdk.StreamEvent, bool, error) {
	var p messageDeltaPayload
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return agentsdk.StreamEvent{}, false, err
	}
	return agentsdk.StreamEvent{
		Type: agentsdk.StreamEventMessageDelta,
		StopReason: agentsdk.StopReason(p.Delta.StopReason),
		Usage: &p.Usage,
	}, false, nil
}
