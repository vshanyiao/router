package stream

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Writer wraps an http.ResponseWriter for sending Server-Sent Events.
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewWriter prepares the response for SSE and returns a writer.
// Returns nil if the response doesn't support flushing.
func NewWriter(w http.ResponseWriter) *Writer {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	return &Writer{w: w, flusher: flusher}
}

// SendJSON marshals data and writes one SSE `data:` event.
func (s *Writer) SendJSON(data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", b); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// SendDone writes the SSE terminator that OpenAI clients expect.
func (s *Writer) SendDone() error {
	if _, err := fmt.Fprint(s.w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
