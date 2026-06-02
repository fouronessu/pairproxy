package proxy

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// estimateModelRouterInputTokens 估算请求 input token。
// 逻辑与 vllm-router.py 的 estimate_tokens 保持一致：汇总文本后按 CJK 占比折算。
func estimateModelRouterInputTokens(bodyBytes []byte) (int, error) {
	if len(bodyBytes) == 0 {
		return 0, nil
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return 0, fmt.Errorf("parse request body for token estimate: %w", err)
	}

	var text string
	if raw, ok := body["system"]; ok {
		text += collectRouterText(raw)
	}

	if raw, ok := body["messages"]; ok {
		var messages []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &messages); err == nil {
			for _, msg := range messages {
				if content, ok := msg["content"]; ok {
					text += collectRouterText(content)
				}
				if toolCalls, ok := msg["tool_calls"]; ok {
					text += collectRouterToolCallsText(toolCalls)
				}
			}
		}
	}

	if raw, ok := body["tools"]; ok {
		var tools []interface{}
		if err := json.Unmarshal(raw, &tools); err == nil {
			for _, tool := range tools {
				data, err := json.Marshal(tool)
				if err == nil {
					text += string(data)
				}
			}
		}
	}

	return charsToRouterTokens(text), nil
}

func collectRouterText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}

	var blocks []map[string]json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}

	var text string
	for _, block := range blocks {
		var typ string
		if rawType, ok := block["type"]; ok {
			_ = json.Unmarshal(rawType, &typ)
		}
		if typ != "text" {
			continue
		}
		if rawText, ok := block["text"]; ok {
			var part string
			if json.Unmarshal(rawText, &part) == nil {
				text += part
			}
		}
	}
	return text
}

func collectRouterToolCallsText(raw json.RawMessage) string {
	var calls []struct {
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &calls) != nil {
		return ""
	}

	var text string
	for _, call := range calls {
		text += call.Function.Name
		text += call.Function.Arguments
	}
	return text
}

func charsToRouterTokens(text string) int {
	if text == "" {
		return 0
	}

	total := utf8.RuneCountInString(text)
	if total == 0 {
		return 0
	}

	cjk := 0
	for _, r := range text {
		if r >= '\u4e00' && r <= '\u9fff' {
			cjk++
		}
	}

	ratio := float64(cjk) / float64(total)
	divisor := 4 - 2*ratio
	tokens := int(float64(total) / divisor)
	if tokens < 1 {
		return 1
	}
	return tokens
}
