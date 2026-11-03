package claude

import (
	"encoding/json"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"

	"github.com/tidwall/sjson"
)

// StructuredJSONResponseToolName 是 OpenAI json_schema 结构化输出仿真工具的固定名称。
// Claude Messages 没有原生 response_format，网关把 json_schema 编译成一个强制调用的
// 工具（tool_choice=tool），模型只能按 input_schema 填 tool_use；响应侧再把这个
// tool_use 的输入原样还原成 JSON 文本内容，客户端看到的就是标准 OpenAI 行为。
// 固定名称让响应侧与计费侧无需额外状态即可识别该仿真工具；若客户自己的工具恰好
// 同名，请求侧会放弃仿真并回退为系统提示注入。
const StructuredJSONResponseToolName = "structured_json_response"

// StructuredJSONResponseToolDescription 仿真工具的描述文案。
const StructuredJSONResponseToolDescription = "Return the final answer as JSON that strictly matches the provided schema. This call replaces a normal text reply."

// JSONObjectSystemInstruction 是 response_format.type=json_object 的系统提示注入文案。
// json_object 没有 schema 可编译成工具，只能用提示词尽力约束（非严格保证）。
const JSONObjectSystemInstruction = "You must respond with a single valid JSON object only. Do not wrap the output in markdown code fences. Do not include any text before or after the JSON."

// FunctionParametersToInputSchema returns function parameters as a Claude
// input_schema, adding type and properties when they are missing or null.
// Otherwise the client's bytes are kept, so the properties keep their order.
// ok is false when parameters are not a JSON object; the result is then an
// empty object schema.
func FunctionParametersToInputSchema(parameters json.RawMessage) (schema json.RawMessage, ok bool) {
	empty := json.RawMessage(`{"type":"object","properties":{}}`)
	switch kitutil.GetJsonType(parameters) {
	case "unknown", "null":
		return empty, true
	case "object":
	default:
		return empty, false
	}
	var fields struct {
		Type       json.RawMessage `json:"type"`
		Properties json.RawMessage `json:"properties"`
	}
	if err := kitutil.Unmarshal(parameters, &fields); err != nil {
		return empty, false
	}
	schema = parameters
	var err error
	if len(fields.Type) == 0 || kitutil.GetJsonType(fields.Type) == "null" {
		if schema, err = sjson.SetRawBytes(schema, "type", []byte(`"object"`)); err != nil {
			return empty, false
		}
	}
	if len(fields.Properties) == 0 || kitutil.GetJsonType(fields.Properties) == "null" {
		if schema, err = sjson.SetRawBytes(schema, "properties", []byte(`{}`)); err != nil {
			return empty, false
		}
	}
	return schema, true
}
