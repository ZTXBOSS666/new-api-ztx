package oaichat

import (
	"fmt"
	"strings"

	"context"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/convdiag"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	sharedclaude "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/claude"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func OpenAIChatRequestToClaudeMessages(c context.Context, info convmeta.Meta, textRequest dto.GeneralOpenAIRequest) (*dto.ClaudeRequest, error) {
	opts := convmeta.OptionsOf(info)
	claudeTools := make([]any, 0, len(textRequest.Tools))

	for _, tool := range textRequest.Tools {
		if kitutil.GetJsonType(tool.Function.Parameters) != "object" && tool.Type != "function" {
			continue
		}
		inputSchema, _ := sharedclaude.FunctionParametersToInputSchema(tool.Function.Parameters)
		claudeTools = append(claudeTools, &dto.Tool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: inputSchema,
		})
	}

	if textRequest.WebSearchOptions != nil {
		webSearchTool := dto.ClaudeWebSearchTool{
			Type: "web_search_20250305",
			Name: "web_search",
		}

		if textRequest.WebSearchOptions.UserLocation != nil {
			anthropicUserLocation := &dto.ClaudeWebSearchUserLocation{
				Type: "approximate",
			}

			var userLocationMap map[string]any
			if err := kitutil.Unmarshal(textRequest.WebSearchOptions.UserLocation, &userLocationMap); err == nil {
				if approximateData, ok := userLocationMap["approximate"].(map[string]any); ok {
					if timezone, ok := approximateData["timezone"].(string); ok && timezone != "" {
						anthropicUserLocation.Timezone = timezone
					}
					if country, ok := approximateData["country"].(string); ok && country != "" {
						anthropicUserLocation.Country = country
					}
					if region, ok := approximateData["region"].(string); ok && region != "" {
						anthropicUserLocation.Region = region
					}
					if city, ok := approximateData["city"].(string); ok && city != "" {
						anthropicUserLocation.City = city
					}
				}
			}

			webSearchTool.UserLocation = anthropicUserLocation
		}

		claudeTools = append(claudeTools, &webSearchTool)
	}

	// OpenAI 的 json_schema / json_object 结构化输出在 Claude Messages 协议里没有
	// 原生对应物——直接丢弃 response_format 会导致模型自由发挥字段名并把 JSON 包进
	// markdown 代码块（客户实测踩坑）。这里把 json_schema 编译成一个强制调用的工具：
	// tool_choice=tool 保证模型只能按 input_schema 填 tool_use，响应侧再把该 tool_use
	// 的输入原样还原成 JSON 文本，客户端看到的行为与 OpenAI strict 一致。
	// json_object 没有 schema，只能退化为系统提示注入（尽力约束，非严格保证）。
	jsonToolName := ""
	needsJSONInstruction := false
	if textRequest.ResponseFormat != nil {
		switch textRequest.ResponseFormat.Type {
		case "json_schema":
			if len(textRequest.ResponseFormat.JsonSchema) > 0 {
				jsonSchema := dto.FormatJsonSchema{}
				if err := kitutil.Unmarshal(textRequest.ResponseFormat.JsonSchema, &jsonSchema); err != nil {
					needsJSONInstruction = true
				} else if claudeHasToolNamed(claudeTools, sharedclaude.StructuredJSONResponseToolName) {
					// 客户自己的工具恰好撞名，放弃仿真避免行为冲突，退化为提示注入。
					needsJSONInstruction = true
				} else if schemaRaw, err := kitutil.Marshal(jsonSchema.Schema); err == nil {
					if inputSchema, ok := sharedclaude.FunctionParametersToInputSchema(schemaRaw); ok {
						tool := &dto.Tool{
							Name:        sharedclaude.StructuredJSONResponseToolName,
							Description: sharedclaude.StructuredJSONResponseToolDescription,
							InputSchema: inputSchema,
						}
						if description := strings.TrimSpace(jsonSchema.Description); description != "" {
							tool.Description = description
						}
						if len(jsonSchema.Strict) > 0 {
							var strict bool
							if err := kitutil.Unmarshal(jsonSchema.Strict, &strict); err == nil && strict {
								tool.Strict = &strict
							}
						}
						claudeTools = append(claudeTools, tool)
						jsonToolName = sharedclaude.StructuredJSONResponseToolName
					}
				}
			} else {
				needsJSONInstruction = true
			}
		case "json_object":
			needsJSONInstruction = true
		}
	}
	if setter, ok := info.(interface{ SetClaudeJSONToolName(string) }); ok {
		setter.SetClaudeJSONToolName(jsonToolName)
	}

	claudeRequest := dto.ClaudeRequest{
		Model:         textRequest.Model,
		StopSequences: nil,
		Temperature:   textRequest.Temperature,
	}
	if len(claudeTools) > 0 {
		claudeRequest.Tools = claudeTools
	}
	if textRequest.MaxCompletionTokens != nil && *textRequest.MaxCompletionTokens > 0 {
		claudeRequest.MaxTokens = kitutil.GetPointer(*textRequest.MaxCompletionTokens)
	} else if textRequest.MaxTokens != nil && *textRequest.MaxTokens > 0 {
		claudeRequest.MaxTokens = kitutil.GetPointer(*textRequest.MaxTokens)
	}
	if textRequest.TopP != nil {
		claudeRequest.TopP = kitutil.GetPointer(*textRequest.TopP)
	}
	if textRequest.TopK != nil {
		claudeRequest.TopK = kitutil.GetPointer(*textRequest.TopK)
	}
	if textRequest.IsStream(nil) {
		claudeRequest.Stream = kitutil.GetPointer(true)
	}

	// json_schema 仿真工具强制指定：结构化输出契约优先于用户自带的 tool_choice
	//（OpenAI 语义下 strict json_schema 与 tool_choice=auto 并存时，最终答案仍必须是
	// 符合 schema 的 JSON；Claude 强制工具是能保证这一点的唯一手段）。
	if jsonToolName != "" {
		claudeRequest.ToolChoice = &dto.ClaudeToolChoice{
			Type:                   "tool",
			Name:                   jsonToolName,
			DisableParallelToolUse: true,
		}
	} else if textRequest.ToolChoice != nil || textRequest.ParallelTooCalls != nil {
		claudeToolChoice := sharedclaude.MapOpenAIToolChoice(textRequest.ToolChoice, textRequest.ParallelTooCalls)
		if claudeToolChoice != nil {
			claudeRequest.ToolChoice = claudeToolChoice
		}
	}

	sourceReasoning, diagnostics, err := reasoning.FromOpenAIChat(&textRequest)
	if err != nil {
		return nil, reasoning.AsClientError(err)
	}
	convdiag.Add(c, diagnostics...)
	if err := sharedclaude.ApplyReasoning(c, &claudeRequest, info, sourceReasoning, true); err != nil {
		return nil, reasoning.AsClientError(err)
	}
	if claudeRequest.MaxTokens == nil {
		if defaultMaxTokens, configured := opts.Claude.DefaultMaxTokensFor(claudeRequest.Model); configured {
			value := uint(defaultMaxTokens)
			claudeRequest.MaxTokens = &value
		}
	}

	claudeRequest.StopSequences = parseClaudeStopSequences(textRequest.Stop)

	formatMessages := make([]dto.Message, 0)
	lastMessage := dto.Message{
		Role: "tool",
	}
	lastMessageHasPlaceholder := false
	for _, message := range textRequest.Messages {
		switch message.Role {
		case "":
			message.Role = "user"
		case "developer":
			message.Role = "system"
		case "function":
			if message.ToolCallId != "" {
				message.Role = "tool"
			} else {
				message.Role = "user"
			}
		case "tool":
			if message.ToolCallId == "" {
				message.Role = "user"
			}
		case "system", "user", "assistant":
		default:
			message.Role = "user"
		}
		fmtMessage := dto.Message{
			Role:    message.Role,
			Content: message.Content,
		}
		if message.Role == "tool" {
			fmtMessage.ToolCallId = message.ToolCallId
		}
		if message.Role == "assistant" && message.ToolCalls != nil {
			fmtMessage.ToolCalls = message.ToolCalls
		}
		if lastMessage.Role == message.Role && lastMessage.Role != "tool" {
			if lastMessage.IsStringContent() && message.IsStringContent() {
				previousToolCalls := lastMessage.ParseToolCalls()
				currentToolCalls := fmtMessage.ParseToolCalls()
				if len(previousToolCalls) > 0 || len(currentToolCalls) > 0 {
					// Keep both messages' tool calls so later tool results still
					// match a tool_use block.
					texts := make([]string, 0, 2)
					// Omit only text injected for an empty tool-call message.
					isToolPlaceholder := lastMessageHasPlaceholder && len(previousToolCalls) > 0
					if previousText := lastMessage.StringContent(); previousText != "" && !isToolPlaceholder {
						texts = append(texts, previousText)
					}
					if currentText := message.StringContent(); currentText != "" {
						texts = append(texts, currentText)
					}
					fmtMessage.SetStringContent(strings.Join(texts, " "))
					fmtMessage.SetToolCalls(append(previousToolCalls, currentToolCalls...))
				} else {
					fmtMessage.SetStringContent(fmt.Sprintf("%s %s", lastMessage.StringContent(), message.StringContent()))
				}
				formatMessages = formatMessages[:len(formatMessages)-1]
			}
		}
		hasPlaceholder := fmtMessage.Content == nil || (fmtMessage.IsStringContent() && fmtMessage.StringContent() == "")
		if hasPlaceholder {
			fmtMessage.SetStringContent("...")
		}
		formatMessages = append(formatMessages, fmtMessage)
		lastMessage = fmtMessage
		lastMessageHasPlaceholder = hasPlaceholder
	}

	claudeMessages := make([]dto.ClaudeMessage, 0)
	isFirstMessage := true
	var systemMessages []dto.ClaudeMediaMessage
	placeholderUserMessage := dto.ClaudeMessage{
		Role: "user",
		Content: []dto.ClaudeMediaMessage{
			{
				Type: "text",
				Text: kitutil.GetPointer[string]("..."),
			},
		},
	}

	for _, message := range formatMessages {
		if message.Role == "system" {
			if message.IsStringContent() {
				if text := message.StringContent(); text != "" {
					systemMessages = append(systemMessages, dto.ClaudeMediaMessage{
						Type: "text",
						Text: kitutil.GetPointer[string](text),
					})
				}
			} else {
				for _, ctx := range message.ParseContent() {
					if ctx.Type == "text" && ctx.Text != "" {
						systemMessages = append(systemMessages, dto.ClaudeMediaMessage{
							Type: "text",
							Text: kitutil.GetPointer[string](ctx.Text),
						})
					}
				}
			}
			continue
		}

		if isFirstMessage {
			isFirstMessage = false
			if message.Role != "user" {
				claudeMessages = append(claudeMessages, placeholderUserMessage)
			}
		}

		claudeMessage := dto.ClaudeMessage{
			Role: message.Role,
		}
		if message.Role == "tool" {
			if len(claudeMessages) > 0 && claudeMessages[len(claudeMessages)-1].Role == "user" {
				lastClaudeMessage := claudeMessages[len(claudeMessages)-1]
				if content, ok := lastClaudeMessage.Content.(string); ok {
					lastClaudeMessage.Content = []dto.ClaudeMediaMessage{
						{
							Type: "text",
							Text: kitutil.GetPointer[string](content),
						},
					}
				}
				lastClaudeMessage.Content = append(lastClaudeMessage.Content.([]dto.ClaudeMediaMessage), dto.ClaudeMediaMessage{
					Type:      "tool_result",
					ToolUseId: message.ToolCallId,
					Content:   message.Content,
				})
				claudeMessages[len(claudeMessages)-1] = lastClaudeMessage
				continue
			}

			claudeMessage.Role = "user"
			claudeMessage.Content = []dto.ClaudeMediaMessage{
				{
					Type:      "tool_result",
					ToolUseId: message.ToolCallId,
					Content:   message.Content,
				},
			}
		} else if message.IsStringContent() && message.ToolCalls == nil {
			text := message.StringContent()
			if text == "" {
				text = "..."
			}
			claudeMessage.Content = text
		} else {
			claudeMediaMessages := make([]dto.ClaudeMediaMessage, 0)
			for _, mediaMessage := range message.ParseContent() {
				switch mediaMessage.Type {
				case "text":
					if mediaMessage.Text != "" {
						claudeMediaMessages = append(claudeMediaMessages, dto.ClaudeMediaMessage{
							Type: "text",
							Text: kitutil.GetPointer[string](mediaMessage.Text),
						})
					}
				default:
					source := mediaMessage.ToFileSource()
					if source == nil {
						continue
					}
					// Claude content blocks carry images, PDF documents, and text
					// documents; audio and video have no Claude block.
					reason := fmt.Sprintf("Claude Messages cannot carry %s content", mediaMessage.Type)
					var block dto.ClaudeMediaMessage
					if mediaMessage.Type != dto.ContentTypeInputAudio && mediaMessage.Type != dto.ContentTypeVideoUrl {
						base64Data, mimeType, err := relaymedia.ResolveBase64Data(c, source, "formatting image for Claude")
						if err != nil {
							return nil, fmt.Errorf("get file data failed: %s", err.Error())
						}
						block, reason = sharedclaude.MediaBlock(base64Data, mimeType, mediaMessage.Type == dto.ContentTypeImageURL)
					}
					if reason != "" {
						convdiag.Add(c, types.ConversionDiagnostic{
							Code:     "unsupported_media_type",
							Path:     "messages.content",
							Message:  reason + "; the part was omitted",
							Severity: types.ConversionDiagnosticError,
						})
						continue
					}
					claudeMediaMessages = append(claudeMediaMessages, block)
					continue
				}
			}

			if message.ToolCalls != nil {
				for _, toolCall := range message.ParseToolCalls() {
					inputObj := make(map[string]any)
					if args := toolCall.Function.Arguments; args != "" {
						if err := kitutil.Unmarshal([]byte(args), &inputObj); err != nil {
							kitutil.LogInfo("tool call function arguments is not a map[string]any: " + fmt.Sprintf("%v", toolCall.Function.Arguments))
						}
					}
					claudeMediaMessages = append(claudeMediaMessages, dto.ClaudeMediaMessage{
						Type:  "tool_use",
						Id:    toolCall.ID,
						Name:  toolCall.Function.Name,
						Input: inputObj,
					})
				}
			}
			if len(claudeMediaMessages) == 0 {
				// Every part was omitted; Claude rejects an empty content array.
				claudeMediaMessages = append(claudeMediaMessages, dto.ClaudeMediaMessage{Type: "text", Text: kitutil.GetPointer("...")})
			}
			claudeMessage.Content = claudeMediaMessages
		}
		claudeMessages = append(claudeMessages, claudeMessage)
	}
	if len(claudeMessages) == 0 && len(systemMessages) > 0 {
		claudeMessages = append(claudeMessages, placeholderUserMessage)
	}

	if len(systemMessages) > 0 {
		claudeRequest.System = systemMessages
	}

	// json_object（或 json_schema 仿真失败）的提示注入：放在最后保持最高指令权重。
	if needsJSONInstruction {
		if claudeRequest.System == nil {
			claudeRequest.System = []dto.ClaudeMediaMessage{}
		}
		if systemBlocks, ok := claudeRequest.System.([]dto.ClaudeMediaMessage); ok {
			claudeRequest.System = append(systemBlocks, dto.ClaudeMediaMessage{
				Type: "text",
				Text: kitutil.GetPointer[string](sharedclaude.JSONObjectSystemInstruction),
			})
		}
	}

	claudeRequest.Prompt = ""
	claudeRequest.Messages = claudeMessages
	// Checked last so every injection path (default hook, thinking adapter
	// floor) has had its chance to satisfy the required field.
	if claudeRequest.MaxTokens == nil {
		return nil, sharedclaude.ErrMissingMaxTokens
	}
	return &claudeRequest, nil
}

// claudeHasToolNamed 判断已转换的工具列表里是否已有同名工具（防仿真工具撞名）。
func claudeHasToolNamed(claudeTools []any, name string) bool {
	for _, tool := range claudeTools {
		if t, ok := tool.(*dto.Tool); ok && t.Name == name {
			return true
		}
	}
	return false
}

func parseClaudeStopSequences(stop any) []string {
	switch stop := stop.(type) {
	case string:
		return []string{stop}
	case []string:
		return stop
	case []any:
		sequences := make([]string, 0, len(stop))
		for _, item := range stop {
			if sequence, ok := item.(string); ok {
				sequences = append(sequences, sequence)
			}
		}
		return sequences
	default:
		return nil
	}
}
