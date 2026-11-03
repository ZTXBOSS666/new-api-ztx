package oaichat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// jsonSchemaMeta 嵌入 convmeta.Values 补全 Meta 接口，附加仿真工具名 setter
// 供断言（与宿主 RelayInfo 的鸭子类型契约一致）。
type jsonSchemaMeta struct {
	convmeta.Values
	jsonToolName string
}

func (m *jsonSchemaMeta) SetClaudeJSONToolName(name string) { m.jsonToolName = name }

// TestJSONSchemaEmulationInjectsForcedTool 复现客户报告的 bug：
// OpenAI json_schema 请求转发 Claude 渠道时 response_format 被静默丢弃，
// 模型自由发挥字段名（age/condition/status）且包 markdown 代码块。
// 修复后：schema 编译为强制工具调用，模型只能按 schema 填 tool_use。
func TestJSONSchemaEmulationInjectsForcedTool(t *testing.T) {
	meta := &jsonSchemaMeta{}
	request := dto.GeneralOpenAIRequest{
		Model: "claude-3-5-sonnet",
		Messages: []dto.Message{
			{Role: "user", Content: "分诊这个患者"},
		},
		MaxTokens: ptrUint(1024),
		ResponseFormat: &dto.ResponseFormat{
			Type: "json_schema",
			JsonSchema: json.RawMessage(`{"name":"triage","strict":true,"schema":{"type":"object","properties":{"age_years":{"type":"integer"},"primary_diagnosis":{"type":"string"},"triage":{"type":"string"}},"required":["age_years","primary_diagnosis","triage"],"additionalProperties":false}}`),
		},
	}

	claudeReq, err := OpenAIChatRequestToClaudeMessages(context.Background(), meta, request)
	require.NoError(t, err)

	// 1. 仿真工具必须被注入且带上了 schema
	toolsRaw, ok := claudeReq.Tools.([]any)
	require.True(t, ok, "Tools 必须是 []any")
	var jsonTool *dto.Tool
	for _, raw := range toolsRaw {
		if tool, ok := raw.(*dto.Tool); ok && tool.Name == sharedclaude.StructuredJSONResponseToolName {
			jsonTool = tool
			break
		}
	}
	require.NotNil(t, jsonTool, "必须注入 json_schema 仿真工具")
	assert.Contains(t, string(jsonTool.InputSchema), `"age_years"`, "schema 字段必须进 input_schema")
	assert.Contains(t, string(jsonTool.InputSchema), `"primary_diagnosis"`)
	require.NotNil(t, jsonTool.Strict, "strict:true 必须透传给 Claude 工具")
	assert.True(t, *jsonTool.Strict)

	// 2. tool_choice 必须强制指定仿真工具且禁并行
	toolChoice, ok := claudeReq.ToolChoice.(*dto.ClaudeToolChoice)
	require.True(t, ok, "ToolChoice 必须是 *ClaudeToolChoice")
	assert.Equal(t, "tool", toolChoice.Type)
	assert.Equal(t, sharedclaude.StructuredJSONResponseToolName, toolChoice.Name)
	assert.True(t, toolChoice.DisableParallelToolUse)

	// 3. 仿真工具名必须记录到 Meta 供响应侧/计费侧使用
	assert.Equal(t, sharedclaude.StructuredJSONResponseToolName, meta.jsonToolName)
}

// TestJSONObjectInjectsSystemInstruction json_object 无 schema 可编译，
// 必须退化为系统提示注入而不是直接丢弃。
func TestJSONObjectInjectsSystemInstruction(t *testing.T) {
	meta := &jsonSchemaMeta{}
	request := dto.GeneralOpenAIRequest{
		Model: "claude-3-5-sonnet",
		Messages: []dto.Message{
			{Role: "user", Content: "给我一个 JSON"},
		},
		MaxTokens:     ptrUint(512),
		ResponseFormat: &dto.ResponseFormat{
			Type: "json_object",
		},
	}

	claudeReq, err := OpenAIChatRequestToClaudeMessages(context.Background(), meta, request)
	require.NoError(t, err)
	assert.Empty(t, meta.jsonToolName, "json_object 不该启用工具仿真")

	systemBlocks, ok := claudeReq.System.([]dto.ClaudeMediaMessage)
	require.True(t, ok, "必须注入 system 提示")
	require.NotEmpty(t, systemBlocks)
	last := systemBlocks[len(systemBlocks)-1]
	require.NotNil(t, last.Text)
	assert.Contains(t, *last.Text, "valid JSON object", "最后一条 system 必须是 JSON 约束指令")
}

// TestJSONSchemaCollisionFallsBackToInstruction 客户工具撞名时必须回退为提示注入，
// 避免两个同名工具引发 Claude 400。
func TestJSONSchemaCollisionFallsBackToInstruction(t *testing.T) {
	meta := &jsonSchemaMeta{}
	request := dto.GeneralOpenAIRequest{
		Model: "claude-3-5-sonnet",
		Messages: []dto.Message{
			{Role: "user", Content: "分诊"},
		},
		MaxTokens: ptrUint(512),
		Tools: []dto.ToolCallRequest{
			{Type: "function", Function: dto.FunctionRequest{Name: sharedclaude.StructuredJSONResponseToolName, Description: "用户自己的同名工具", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}},
		},
		ResponseFormat: &dto.ResponseFormat{
			Type:       "json_schema",
			JsonSchema: json.RawMessage(`{"name":"triage","schema":{"type":"object","properties":{"x":{"type":"string"}}}}`),
		},
	}

	claudeReq, err := OpenAIChatRequestToClaudeMessages(context.Background(), meta, request)
	require.NoError(t, err)
	assert.Empty(t, meta.jsonToolName, "撞名必须放弃仿真")

	toolsRaw, ok := claudeReq.Tools.([]any)
	require.True(t, ok)
	var toolCount int
	for _, raw := range toolsRaw {
		if tool, ok := raw.(*dto.Tool); ok && tool.Name == sharedclaude.StructuredJSONResponseToolName {
			toolCount++
		}
	}
	assert.Equal(t, 1, toolCount, "同名工具只能剩客户自己的那一个")

	systemBlocks, ok := claudeReq.System.([]dto.ClaudeMediaMessage)
	require.True(t, ok)
	require.NotEmpty(t, systemBlocks)
	assert.Contains(t, *systemBlocks[len(systemBlocks)-1].Text, "valid JSON")
}

// TestNoResponseFormatKeepsNormalToolChoice 没用结构化输出时，tool_choice 映射不受影响。
func TestNoResponseFormatKeepsNormalToolChoice(t *testing.T) {
	meta := &jsonSchemaMeta{}
	request := dto.GeneralOpenAIRequest{
		Model: "claude-3-5-sonnet",
		Messages: []dto.Message{
			{Role: "user", Content: "用工具查天气"},
		},
		MaxTokens: ptrUint(512),
		Tools: []dto.ToolCallRequest{
			{Type: "function", Function: dto.FunctionRequest{Name: "get_weather", Parameters: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`)}},
		},
		ToolChoice: "auto",
	}

	claudeReq, err := OpenAIChatRequestToClaudeMessages(context.Background(), meta, request)
	require.NoError(t, err)
	assert.Empty(t, meta.jsonToolName)
	toolChoice, ok := claudeReq.ToolChoice.(*dto.ClaudeToolChoice)
	require.True(t, ok)
	assert.Equal(t, "auto", toolChoice.Type)
}

func ptrUint(v uint) *uint { return &v }
