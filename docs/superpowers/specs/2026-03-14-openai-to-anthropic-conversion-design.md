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

Detection logic replaces the existing `shouldConvertProtocol` function:

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

The existing test `TestShouldConvertProtocol` must be renamed to `TestDetectConversionDirection` and updated to test `detectConversionDirection` returning `conversionDirection` values instead of booleans.

### Target Routing Fix for OtoA

`preferredProvidersByPath` in `sproxy.go` currently maps `/v1/chat/completions` → `{openai: true, ollama: true}`, which excludes Anthropic targets. For OtoA conversion, the physical upstream path is `/v1/messages`, not `/v1/chat/completions`.

**Fix**: When `convDir == conversionOtoA`, compute `effectivePath = "/v1/messages"` and pass it to both the initial `pickLLMTarget` call AND to `buildRetryTransport`'s `PickNext` closure. Concretely:

```go
// In serveProxy, after detecting convDir:
effectivePath := r.URL.Path
if convDir == conversionOtoA {
    effectivePath = "/v1/messages"  // use for both initial pick and retries
}
target := pickLLMTarget(effectivePath, ...)
// Later, pass effectivePath to buildRetryTransport so the PickNext closure
// uses it rather than r.URL.Path (which is /v1/chat/completions before Director fires).
rt := buildRetryTransport(effectivePath, ...)
```

This ensures `preferredProvidersByPath["/v1/messages"]` matches Anthropic targets correctly for both the first attempt and any retries. Note: during retries `req.URL.Path` has already been rewritten by the Director to `/v1/messages`, so using `effectivePath` is consistent with what the Director produces.

### Data Flow

**New direction (OtoA):**
```
OpenAI client
  → [/v1/chat/completions] → sproxy
  → (target selected via effective path /v1/messages → Anthropic target chosen)
  → convertOpenAIToAnthropicRequest() → Director rewrites path to [/v1/messages] → Anthropic
  ← Anthropic SSE/JSON response
  → TeeResponseWriter wraps AnthropicToOpenAIStreamConverter
    (TeeResponseWriter sees raw Anthropic bytes → AnthropicSSEParser for token counting)
  → AnthropicToOpenAIStreamConverter (SSE: Anthropic events → OpenAI chunks)
  ← OpenAI SSE/JSON → client
```

**Streaming chain layout** (same pattern as existing AtoO chain):
- `AnthropicToOpenAIStreamConverter` implements `http.ResponseWriter`, wraps the real client writer `w`
- `TeeResponseWriter` wraps the stream converter: `tw = NewTeeResponseWriter(streamConverter, parser, ...)`
- `proxy.ServeHTTP(tw)` — upstream writes raw Anthropic bytes to `tw`; `tw` tees to token parser AND forwards to `streamConverter` which translates to OpenAI format for the client

Token counting requires no changes: `targetProvider="anthropic"` causes `NewResponseParser` to select `AnthropicSSEParser` on the raw upstream bytes inside `TeeResponseWriter`.

### Model Extraction (requestedModel)

The existing `extractModel(r)` / `extractModelFromBody(bodyBytes)` mechanism already handles model extraction for both AtoO and OtoA: it checks the `X-PairProxy-Model` header and falls back to parsing `{"model":"..."}` from the body. Since both OpenAI and Anthropic formats use a top-level `model` field, no new extraction logic is needed. The `requestedModel` variable populated by the existing code is available for use in `convertAnthropicToOpenAIResponseReverse` for OtoA without modification.

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

### stream_options Handling

`injectOpenAIStreamOptions` runs unconditionally on `/v1/chat/completions` bodies before conversion detection. The `stream_options` field it injects must be discarded inside `convertOpenAIToAnthropicRequest` (it is an OpenAI-specific field not recognized by Anthropic). It is included in the discard list alongside `n`, `logprobs`, `presence_penalty`, `frequency_penalty`, `user`, etc.

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
| `n`, `logprobs`, `presence_penalty`, `frequency_penalty`, `user`, `stream_options`, etc. | Discard | Anthropic does not support |

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

**tool messages** (role=tool): Consecutive tool role messages following an assistant message are merged into a single Anthropic user message containing multiple `tool_result` content blocks. The `content` field of a tool message may be either a string or an array of content objects; both forms are normalized to a string for the Anthropic `tool_result.content` field (array items of type `text` are joined with `\n`):
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

`requestedModel` is the OpenAI model name extracted from the original request body before conversion. It is used as the `model` field in the response so the client sees the model it originally requested.

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

Implements `http.ResponseWriter` **and `http.Flusher`**. Wraps the real client `http.ResponseWriter` `w`. The `Flush()` method delegates to `w.(http.Flusher).Flush()` so SSE chunks are pushed to the client immediately. `TeeResponseWriter` wraps this converter so the raw Anthropic bytes are visible to the token parser before being translated.

**Internal state:**
- `messageID string` — extracted from `message_start`, used in all output chunks
- `model string` — from constructor (original OpenAI model name requested by client)
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

When the upstream returns an HTTP error status (>= 400), conversion branching is:

- `convDir == conversionOtoA`: call `convertAnthropicErrorResponseToOpenAI()` — converts Anthropic error format to OpenAI format
- `convDir == conversionAtoO`: call existing `convertOpenAIErrorResponse()` — converts OpenAI error format to Anthropic format
- `convDir == conversionNone`: pass through unchanged

Anthropic → OpenAI error format conversion:

```json
// Anthropic: {"type":"error","error":{"type":"authentication_error","message":"..."}}
// OpenAI:    {"error":{"type":"authentication_error","message":"..."}}
```

New function `convertAnthropicErrorResponseToOpenAI()` — inverse of existing `convertOpenAIErrorResponse`.

---

## Changes to sproxy.go

1. Replace `needsConversion bool` with `convDir conversionDirection` at line ~1190
2. Replace `shouldConvertProtocol(...)` call with `detectConversionDirection(path, targetProvider)`
3. **Target selection + retry**: compute `effectivePath = "/v1/messages"` when `convDir == conversionOtoA`; pass to both `pickLLMTarget` and `buildRetryTransport` (so the `PickNext` closure uses the effective path, not `r.URL.Path`)
4. **Request conversion**: branch on `convDir == conversionOtoA` to call `convertOpenAIToAnthropicRequest()`; if it fails, return HTTP 400 to the client (do not degrade silently — the original path `/v1/chat/completions` would 404 on Anthropic). The existing AtoO graceful degradation behavior is unchanged.
5. **Stream converter setup**: `conversionOtoA` → `NewAnthropicToOpenAIStreamConverter(w, requestedModel, ...)`; `conversionAtoO` → existing `NewOpenAIToAnthropicStreamConverter`; wrap with `TeeResponseWriter` in both cases
6. **Director path rewrite**: apply for both directions (already done for AtoO; add OtoA branch rewriting `/v1/chat/completions` → `/v1/messages`)
7. **Non-streaming response conversion and token counting**: for OtoA, token counting (`tw.RecordNonStreaming`) must be called with the **pre-conversion raw Anthropic response body** before calling `convertAnthropicToOpenAIResponseReverse`. The `AnthropicSSEParser` (selected because `targetProvider="anthropic"`) must see the Anthropic JSON, not the already-converted OpenAI JSON. Sequence: read raw body → call `tw.RecordNonStreaming(rawBody)` → call `convertAnthropicToOpenAIResponseReverse(rawBody, ...)` → write converted body to client.
8. **Error response conversion**: branch on `convDir` — OtoA calls `convertAnthropicErrorResponseToOpenAI()`, AtoO calls existing `convertOpenAIErrorResponse()`, None passes through unchanged
9. **`requestedModel`**: the existing `extractModel(r)` / `extractModelFromBody(bodyBytes)` already populates this correctly for OtoA (both formats use a top-level `model` field); no additional extraction needed

---

## Testing

New and updated test cases in `internal/proxy/protocol_converter_test.go`:

### Direction detection (replaces TestShouldConvertProtocol)
Rename `TestShouldConvertProtocol` → `TestDetectConversionDirection`:
- `/v1/chat/completions` + `anthropic` → `conversionOtoA`
- `/v1/messages` + `openai` → `conversionAtoO`
- `/v1/messages` + `anthropic` → `conversionNone`
- `/v1/chat/completions` + `openai` → `conversionNone`

### Request conversion
- Basic text messages with system
- Multiple consecutive tool role messages merged (string content)
- tool role message with array-typed content (text items joined with `\n`)
- assistant message with tool_calls history
- user message with image_url (data URI + https URL)
- tools array conversion (unwrap function wrapper)
- tool_choice all variants
- stop field normalization (string → array)
- stream_options field is discarded

### Non-streaming response conversion
- Text response
- Tool use response
- stop_reason all variants
- usage fields
- requestedModel propagated to response model field

### Streaming conversion (AnthropicToOpenAIStreamConverter)
- Text streaming
- Tool use streaming (content_block_start tool_use + input_json_delta)
- Empty response
- finish_reason mapping

### Error conversion
- Anthropic error → OpenAI format (conversionOtoA path)

### OtoA failure path
- Malformed/invalid OpenAI request body → `convertOpenAIToAnthropicRequest` fails → return HTTP 400 to client (not silent degradation)
