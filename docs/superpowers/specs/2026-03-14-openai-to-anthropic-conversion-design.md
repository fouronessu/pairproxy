# OpenAI → Anthropic Protocol Conversion Design

**Date**: 2026-03-14
**Status**: Approved
**Scope**: `internal/proxy/protocol_converter.go`, `internal/proxy/sproxy.go`

---

## Problem

When an OpenAI-compatible client sends requests to sproxy with an Anthropic target (`provider: anthropic`), sproxy forwards the request path `/v1/chat/completions` unchanged to Anthropic's API. Anthropic does not have this endpoint and returns HTTP 404.

The existing protocol conversion only handles the reverse direction: Anthropic client → OpenAI/Ollama target.

---

## Solution Overview

Implement symmetric protocol conversion for the OpenAI→Anthropic direction:

1. **Request**: Convert OpenAI Chat Completions (`/v1/chat/completions`) → Anthropic Messages (`/v1/messages`)
2. **Response (non-streaming)**: Convert Anthropic Messages response → OpenAI Chat Completions response
3. **Response (streaming)**: Convert Anthropic SSE events → OpenAI SSE chunks

---

## Architecture

### Conversion Direction Type

Replace the existing `needsConversion bool` in `sproxy.go` with a typed direction:

```go
type conversionDirection int

const (
    conversionNone conversionDirection = iota
    conversionAtoO  // Anthropic client → OpenAI/Ollama target (existing)
    conversionOtoA  // OpenAI client → Anthropic target (new)
)
```

Detection logic:

```go
func detectConversionDirection(requestPath, targetProvider string) conversionDirection {
    if strings.HasPrefix(requestPath, "/v1/messages") &&
        (targetProvider == "openai" || targetProvider == "ollama") {
        return conversionAtoO
    }
    if strings.HasPrefix(requestPath, "/v1/chat/completions") &&
        targetProvider == "anthropic" {
        return conversionOtoA
    }
    return conversionNone
}
```

### Data Flow

**New direction (OtoA):**
```
OpenAI client
  → [/v1/chat/completions] → sproxy
  → convertOpenAIToAnthropicRequest() → [/v1/messages] → Anthropic
  ← Anthropic SSE/JSON response
  → TeeResponseWriter (AnthropicSSEParser, token counting)
  → AnthropicToOpenAIStreamConverter (SSE: Anthropic events → OpenAI chunks)
  ← OpenAI SSE/JSON → client
```

Token counting is correct without changes: `targetProvider="anthropic"` causes TeeResponseWriter to use `AnthropicSSEParser` on the raw upstream response.

---

## Request Conversion: OpenAI → Anthropic

### Function Signature

```go
func convertOpenAIToAnthropicRequest(
    body []byte,
    logger *zap.Logger,
    reqID string,
    modelMapping map[string]string,
) (converted []byte, newPath string, err error)
```

Returns `newPath = "/v1/messages"`.

### Field Mapping

| OpenAI field | Anthropic field | Notes |
|---|---|---|
| `model` | `model` | Apply modelMapping if configured |
| `messages[role=system].content` | `system` | Extract all system messages, join with `\n\n` |
| `messages[role=user]` | `messages[role=user]` | Content items converted (see below) |
| `messages[role=assistant]` | `messages[role=assistant]` | tool_calls → tool_use blocks |
| `messages[role=tool]` | Merged into adjacent `user` message | As `tool_result` content blocks |
| `max_tokens` | `max_tokens` | Pass through |
| `temperature` | `temperature` | Pass through |
| `top_p` | `top_p` | Pass through |
| `stop` (string or array) | `stop_sequences` | Normalize to array |
| `stream` | `stream` | Pass through |
| `tools` | `tools` | Unwrap function wrapper; `parameters` → `input_schema` |
| `tool_choice` | `tool_choice` | See mapping table below |
| `n`, `logprobs`, `presence_penalty`, `frequency_penalty`, `user`, etc. | Discard | Anthropic does not support |

### tool_choice Mapping

| OpenAI | Anthropic |
|---|---|
| `"auto"` | `{"type": "auto"}` |
| `"none"` | `{"type": "none"}` |
| `"required"` | `{"type": "any"}` |
| `{"type":"function","function":{"name":"X"}}` | `{"type":"tool","name":"X"}` |

### Message Conversion Details

**system messages**: Extracted and removed from the messages array. If multiple system messages exist, their content is joined with `"\n\n"` into the top-level `system` field.

**user messages**:
- String content → `{"role":"user","content":"..."}`
- Array content items:
  - `{type:"text"}` → `{type:"text","text":"..."}`
  - `{type:"image_url",image_url:{url:"data:TYPE;base64,DATA"}}` → `{type:"image","source":{"type":"base64","media_type":"TYPE","data":"DATA"}}`
  - `{type:"image_url",image_url:{url:"https://..."}}` → `{type:"image","source":{"type":"url","url":"..."}}`

**assistant messages**:
- String content → `{type:"text","text":"..."}` block
- `tool_calls[]` → `{type:"tool_use","id":"...","name":"...","input":{...}}` blocks (arguments JSON string → parsed object)
- Content + tool_calls → text block + tool_use blocks in same content array

**tool messages** (role=tool): Consecutive tool role messages following an assistant message are merged into a single Anthropic user message containing multiple `tool_result` content blocks:
```json
{"role":"user","content":[
  {"type":"tool_result","tool_use_id":"call_id","content":"result text"},
  ...
]}
```

### tools Conversion

```
OpenAI: {"type":"function","function":{"name":"...","description":"...","parameters":{...}}}
Anthropic: {"name":"...","description":"...","input_schema":{...}}
```

---

## Non-Streaming Response Conversion: Anthropic → OpenAI

### Function Signature

```go
func convertAnthropicToOpenAIResponseReverse(
    body []byte,
    logger *zap.Logger,
    reqID string,
    requestedModel string,
) ([]byte, error)
```

### Field Mapping

| Anthropic field | OpenAI field | Notes |
|---|---|---|
| `id` (msg_xxx) | `id` (chatcmpl-xxx) | Prefix swap |
| `model` | `model` | Use `requestedModel` if non-empty |
| `content[type=text].text` | `choices[0].message.content` | Join multiple text blocks with `\n` |
| `content[type=tool_use]` | `choices[0].message.tool_calls[]` | `input` object → `arguments` JSON string |
| `stop_reason` | `choices[0].finish_reason` | See mapping below |
| `usage.input_tokens` | `usage.prompt_tokens` | |
| `usage.output_tokens` | `usage.completion_tokens` | |
| `usage.cache_read_input_tokens` | `usage.prompt_tokens_details.cached_tokens` | |

### stop_reason Mapping

| Anthropic `stop_reason` | OpenAI `finish_reason` |
|---|---|
| `end_turn` | `stop` |
| `max_tokens` | `length` |
| `tool_use` | `tool_calls` |
| `stop_sequence` | `stop` |
| others | pass through |

### Response Envelope

```json
{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "model": "...",
  "choices": [{
    "index": 0,
    "message": {"role": "assistant", "content": "...", "tool_calls": [...]},
    "finish_reason": "stop"
  }],
  "usage": {"prompt_tokens": N, "completion_tokens": N, "total_tokens": N}
}
```

---

## Streaming Response Conversion: Anthropic SSE → OpenAI SSE

### AnthropicToOpenAIStreamConverter

Implements `http.ResponseWriter`. Sits between TeeResponseWriter and the client.

**Internal state:**
- `messageID string` — extracted from `message_start`, used in all output chunks
- `model string` — from constructor (original model requested by client)
- `inputTokens int` — from `message_start.message.usage.input_tokens`
- `outputTokens int` — from `message_delta.usage.output_tokens`
- `toolCallIndex map[int]int` — Anthropic block index → OpenAI tool_calls index

**Event handling:**

| Anthropic event | Action |
|---|---|
| `message_start` | Extract `id` → `messageID`; extract `input_tokens`; emit first chunk: `{"id":"chatcmpl-...","object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant","content":""},"index":0,"finish_reason":null}]}` |
| `content_block_start` (type=text) | No output (wait for deltas) |
| `content_block_delta` (text_delta) | Emit: `{"choices":[{"delta":{"content":"TEXT"},"index":0}]}` |
| `content_block_start` (type=tool_use) | Emit: `{"choices":[{"delta":{"tool_calls":[{"index":N,"id":"ID","type":"function","function":{"name":"NAME","arguments":""}}]},"index":0}]}` |
| `content_block_delta` (input_json_delta) | Emit: `{"choices":[{"delta":{"tool_calls":[{"index":N,"function":{"arguments":"PARTIAL"}}]},"index":0}]}` |
| `content_block_stop` | No output |
| `message_delta` | Extract `stop_reason`, `output_tokens`; emit final content chunk: `{"choices":[{"delta":{},"finish_reason":"stop","index":0}],"usage":{"prompt_tokens":N,"completion_tokens":M,"total_tokens":N+M}}` |
| `message_stop` | Emit `data: [DONE]\n\n` |

All output chunks include the full envelope: `{"id":"chatcmpl-...","object":"chat.completion.chunk","created":TIMESTAMP,"model":"...","choices":[...]}`

---

## Error Response Conversion

Anthropic errors → OpenAI format:

```json
// Anthropic: {"type":"error","error":{"type":"authentication_error","message":"..."}}
// OpenAI:    {"error":{"type":"authentication_error","message":"..."}}
```

New function `convertAnthropicErrorResponseToOpenAI()` — inverse of existing `convertOpenAIErrorResponse`.

---

## Changes to sproxy.go

1. Replace `needsConversion bool` with `convDir conversionDirection` at line 1190
2. Request conversion block: branch on `convDir == conversionOtoA` to call `convertOpenAIToAnthropicRequest()`
3. Stream converter: `conversionOtoA` → `NewAnthropicToOpenAIStreamConverter(w, ...)`; `conversionAtoO` → existing
4. Director path rewrite: apply for both directions
5. Non-streaming response conversion: branch on `convDir == conversionOtoA` to call `convertAnthropicToOpenAIResponseReverse()`
6. Error response conversion: add OtoA error conversion branch

---

## Testing

New test cases in `internal/proxy/protocol_converter_test.go`:

### Request conversion
- Basic text messages with system
- Multiple consecutive tool role messages merged
- assistant message with tool_calls history
- user message with image_url (data URI + https URL)
- tools array conversion (unwrap function wrapper)
- tool_choice all variants
- stop field normalization (string → array)

### Non-streaming response conversion
- Text response
- Tool use response
- stop_reason all variants
- usage fields

### Streaming conversion (AnthropicToOpenAIStreamConverter)
- Text streaming
- Tool use streaming (content_block_start tool_use + input_json_delta)
- Empty response
- finish_reason mapping

### Integration (shouldConvert / direction detection)
- `/v1/chat/completions` + `anthropic` → `conversionOtoA`
- `/v1/messages` + `openai` → `conversionAtoO`
- `/v1/messages` + `anthropic` → `conversionNone`
- `/v1/chat/completions` + `openai` → `conversionNone`
