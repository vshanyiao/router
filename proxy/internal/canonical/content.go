// Package canonical defines the provider-neutral IR used between API surface
// handlers and provider adapters. Both inbound surfaces (OAI /v1/chat/completions
// and Anthropic /v1/messages) lower into these types; each provider adapter
// reads them and emits its own wire format.
package canonical

import "encoding/json"

// BlockType enumerates the supported content block kinds.
type BlockType string

const (
	BlockText       BlockType = "text"
	BlockImage      BlockType = "image"
	BlockToolUse    BlockType = "tool_use"
	BlockToolResult BlockType = "tool_result"
)

// ContentBlock is the unit of multi-modal content. A message's Content is
// always a slice of these, even for plain text (a string at the wire format
// gets wrapped in a single TextBlock at the surface boundary).
type ContentBlock struct {
	Type BlockType

	// Text: present when Type == BlockText.
	Text string

	// Image: present when Type == BlockImage. Exactly one of ImageURL or
	// ImageData is set. MediaType is the MIME type (e.g. "image/png");
	// required when ImageData is set.
	ImageURL  string
	ImageData []byte
	MediaType string

	// ToolUse: present when Type == BlockToolUse. The assistant is calling a
	// tool. ToolInput is the raw JSON of the arguments.
	ToolUseID string
	ToolName  string
	ToolInput json.RawMessage

	// ToolResult: present when Type == BlockToolResult. The user is supplying
	// the result of a previous tool call. ToolUseID matches the assistant's
	// ToolUseID. ToolResult is the textual result (we don't model multi-modal
	// tool results in Phase 2; defer).
	ToolResult string
}

func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: BlockText, Text: text}
}

func ImageBlock(url string, mediaType string) ContentBlock {
	return ContentBlock{Type: BlockImage, ImageURL: url, MediaType: mediaType}
}

func ImageDataBlock(data []byte, mediaType string) ContentBlock {
	return ContentBlock{Type: BlockImage, ImageData: data, MediaType: mediaType}
}

func ToolUseBlock(id, name string, input json.RawMessage) ContentBlock {
	return ContentBlock{Type: BlockToolUse, ToolUseID: id, ToolName: name, ToolInput: input}
}

func ToolResultBlock(toolUseID, result string) ContentBlock {
	return ContentBlock{Type: BlockToolResult, ToolUseID: toolUseID, ToolResult: result}
}
