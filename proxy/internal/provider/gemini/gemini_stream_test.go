package gemini

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eofReader is an io.ReadCloser that returns the final chunk together with
// io.EOF in a single Read call — matching real HTTP/1.1 end-of-stream behavior.
type eofReader struct {
	chunks [][]byte
	idx    int
}

func (r *eofReader) Read(p []byte) (int, error) {
	if r.idx >= len(r.chunks) {
		return 0, io.EOF
	}
	c := r.chunks[r.idx]
	n := copy(p, c)
	r.idx++
	if r.idx >= len(r.chunks) {
		return n, io.EOF
	}
	return n, nil
}

func (r *eofReader) Close() error { return nil }

// TestRecv_DrainsBufferOnEOF is the regression test for C1: when the final
// SSE event (containing usageMetadata) arrives in the same Read that returns
// io.EOF, Recv must still parse it before emitting the stop event.
func TestRecv_DrainsBufferOnEOF(t *testing.T) {
	body := &eofReader{
		chunks: [][]byte{
			[]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n\n"),
			// Final event with usage arrives WITH io.EOF
			[]byte("data: {\"usageMetadata\":{\"promptTokenCount\":42,\"candidatesTokenCount\":17}}\n\n"),
		},
	}
	s := &geminiStream{body: body}

	// First Recv should yield the "hello" content event.
	evt, err := s.Recv()
	require.NoError(t, err)
	assert.Equal(t, "content", evt.Type)
	assert.Equal(t, "hello", evt.ContentDelta)

	// Second Recv should drain the usage event and emit stop with the counts.
	evt, err = s.Recv()
	assert.Equal(t, io.EOF, err, "expected io.EOF on stop event")
	assert.Equal(t, "stop", evt.Type)
	require.NotNil(t, evt.Usage)
	assert.Equal(t, 42, evt.Usage.PromptTokens, "C1 regression: prompt tokens lost on EOF")
	assert.Equal(t, 17, evt.Usage.CompletionTokens, "C1 regression: completion tokens lost on EOF")

	// Third Recv should keep returning io.EOF.
	_, err = s.Recv()
	assert.Equal(t, io.EOF, err)
}

// TestRecv_HandlesTrailingEventWithoutDoubleNewline covers the case where the
// server doesn't terminate the last event with `\n\n`.
func TestRecv_HandlesTrailingEventWithoutDoubleNewline(t *testing.T) {
	body := &eofReader{
		chunks: [][]byte{
			[]byte("data: {\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":3}}\n"),
		},
	}
	s := &geminiStream{body: body}

	evt, err := s.Recv()
	assert.Equal(t, io.EOF, err)
	require.NotNil(t, evt.Usage)
	assert.Equal(t, 7, evt.Usage.PromptTokens)
	assert.Equal(t, 3, evt.Usage.CompletionTokens)
}

// TestRecv_PropagatesNonEOFErrors confirms read errors other than EOF do not
// silently become a stop event.
func TestRecv_PropagatesNonEOFErrors(t *testing.T) {
	body := &erroringReader{err: errors.New("network died")}
	s := &geminiStream{body: body}

	_, err := s.Recv()
	assert.EqualError(t, err, "network died")
}

type erroringReader struct{ err error }

func (r *erroringReader) Read(p []byte) (int, error) { return 0, r.err }
func (r *erroringReader) Close() error               { return nil }

// silence "imported and not used" for bytes in some build configurations.
var _ = bytes.Index
