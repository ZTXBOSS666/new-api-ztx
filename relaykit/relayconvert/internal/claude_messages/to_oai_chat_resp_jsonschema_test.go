package claudemessages

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jsonToolNameFixture 构造一次仿真工具的 Claude 响应：
// text 前导块（preamble）+ 仿真 tool_use（字段名符合 schema）。
func jsonToolNameFixture() *dto.ClaudeResponse {
	return &dto.ClaudeResponse{
		Id:         "msg_01",
		Model:      "claude-3-5-sonnet",
		StopReason: "tool_use",
		Content: []dto.ClaudeMediaMessage{
			{Type: "text", Text: strPtr("下面是分诊结果：")},
			{Type: "tool_use", Id: "toolu_01", Name: sharedclaude.StructuredJSONResponseToolName, Input: map[string]any{
				"age_years":         42,
				"primary_diagnosis": "急性阑尾炎",
				"triage":            "red",
			}},
		},
	}
}

// TestStructuredJSONNonStreamRestoresContent 非流式：仿真 tool_use 的输入必须
// 还原成 JSON 文本内容，丢弃 text 前导块，finish_reason=stop，无 tool_calls。
func TestStructuredJSONNonStreamRestoresContent(t *testing.T) {
	resp := ResponseClaude2OpenAIWithStructuredJSON(jsonToolNameFixture(), sharedclaude.StructuredJSONResponseToolName)
	require.Len(t, resp.Choices, 1)
	choice := resp.Choices[0]

	content := choice.Message.StringContent()
	assert.NotContains(t, content, "分诊结果", "text 前导块必须被丢弃")
	assert.Contains(t, content, `"age_years":42`, "仿真工具输入必须成为 content")
	assert.Contains(t, content, `"primary_diagnosis":"急性阑尾炎"`)
	assert.Contains(t, content, `"triage":"red"`)
	assert.Nil(t, choice.Message.ParseToolCalls(), "不能暴露 tool_calls")
	assert.Equal(t, "stop", choice.FinishReason, "强制工具的 tool_use 必须映射为 stop")
}

// TestStructuredJSONNonStreamInactiveWithoutName 工具名为空（仿真未激活）时，
// 走原有行为：tool_use 转成 tool_calls，text 进 content。
func TestStructuredJSONNonStreamInactiveWithoutName(t *testing.T) {
	resp := ResponseClaude2OpenAI(jsonToolNameFixture())
	require.Len(t, resp.Choices, 1)
	choice := resp.Choices[0]

	assert.Contains(t, choice.Message.StringContent(), "分诊结果", "text 必须保留")
	toolCalls := choice.Message.ParseToolCalls()
	require.Len(t, toolCalls, 1, "tool_use 必须转成 tool_calls")
	assert.Equal(t, sharedclaude.StructuredJSONResponseToolName, toolCalls[0].Function.Name)
}

// TestStructuredJSONStreamRestoresContent 流式：仿真块的 input_json_delta 必须改道为
// 文本 content delta，起始事件被吞掉，stop_reason=tool_use 映射为 stop。
func TestStructuredJSONStreamRestoresContent(t *testing.T) {
	state := NewClaudeToChatStreamState()
	state.SetStructuredJSONTool(sharedclaude.StructuredJSONResponseToolName)

	// text 前导块 delta 必须被丢弃
	textDelta, err := state.ConvertChunk(&dto.ClaudeResponse{
		Type:  "content_block_delta",
		Index: intPtr(0),
		Delta: &dto.ClaudeMediaMessage{Type: "text_delta", Text: strPtr("下面是分诊结果：")},
	})
	require.NoError(t, err)
	assert.Nil(t, textDelta, "text 前导块必须被丢弃")

	// 仿真 tool_use 块起始事件被吞掉
	startChunk, err := state.ConvertChunk(&dto.ClaudeResponse{
		Type:  "content_block_start",
		Index: intPtr(1),
		ContentBlock: &dto.ClaudeMediaMessage{
			Type: "tool_use",
			Id:   "toolu_01",
			Name: sharedclaude.StructuredJSONResponseToolName,
		},
	})
	require.NoError(t, err)
	assert.Nil(t, startChunk, "仿真块的 content_block_start 不产生 tool_calls chunk")

	// JSON 片段改道为文本 content delta
	jsonChunk, err := state.ConvertChunk(&dto.ClaudeResponse{
		Type:  "content_block_delta",
		Index: intPtr(1),
		Delta: &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: strPtr(`{"age_years":42`)},
	})
	require.NoError(t, err)
	require.NotNil(t, jsonChunk, "JSON 片段必须产生 chunk")
	require.Len(t, jsonChunk.Choices, 1)
	require.NotNil(t, jsonChunk.Choices[0].Delta.Content)
	assert.Equal(t, `{"age_years":42`, *jsonChunk.Choices[0].Delta.Content, "JSON 片段必须走 content 通道")
	assert.Nil(t, jsonChunk.Choices[0].Delta.ToolCalls, "不能走 tool_calls 通道")

	jsonChunk2, err := state.ConvertChunk(&dto.ClaudeResponse{
		Type:  "content_block_delta",
		Index: intPtr(1),
		Delta: &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: strPtr(`,"triage":"red"}`)},
	})
	require.NoError(t, err)
	require.NotNil(t, jsonChunk2)
	require.NotNil(t, jsonChunk2.Choices[0].Delta.Content)
	assert.Equal(t, `,"triage":"red"}`, *jsonChunk2.Choices[0].Delta.Content)

	// stop_reason=tool_use 必须映射为 stop
	finishChunk, err := state.ConvertChunk(&dto.ClaudeResponse{
		Type:  "message_delta",
		Delta: &dto.ClaudeMediaMessage{StopReason: strPtr("tool_use")},
	})
	require.NoError(t, err)
	require.NotNil(t, finishChunk)
	require.NotNil(t, finishChunk.Choices[0].FinishReason)
	assert.Equal(t, "stop", *finishChunk.Choices[0].FinishReason)
}

// TestStructuredJSONStreamRegularToolUnaffected 流式下普通工具调用不受仿真影响。
func TestStructuredJSONStreamRegularToolUnaffected(t *testing.T) {
	state := NewClaudeToChatStreamState()
	state.SetStructuredJSONTool(sharedclaude.StructuredJSONResponseToolName)

	startChunk, err := state.ConvertChunk(&dto.ClaudeResponse{
		Type:  "content_block_start",
		Index: intPtr(0),
		ContentBlock: &dto.ClaudeMediaMessage{
			Type: "tool_use",
			Id:   "toolu_02",
			Name: "get_weather",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, startChunk, "普通工具的起始事件必须正常产生 tool_calls")
	require.Len(t, startChunk.Choices, 1)
	require.NotNil(t, startChunk.Choices[0].Delta.ToolCalls)

	deltaChunk, err := state.ConvertChunk(&dto.ClaudeResponse{
		Type:  "content_block_delta",
		Index: intPtr(0),
		Delta: &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: strPtr(`{"city":"北京"}`)},
	})
	require.NoError(t, err)
	require.NotNil(t, deltaChunk)
	require.NotNil(t, deltaChunk.Choices[0].Delta.ToolCalls, "普通工具的 JSON 参数必须走 tool_calls 通道")
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
