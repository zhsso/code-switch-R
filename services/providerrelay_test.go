package services

import (
	"encoding/json"
	"testing"

	"github.com/tidwall/gjson"
)

func TestResponseUsageSnapshotIsNotAccumulated(t *testing.T) {
	payload := `{
		"type": "response.completed",
		"response": {
			"usage": {
				"input_tokens": 100,
				"output_tokens": 20,
				"input_tokens_details": {"cached_tokens": 30},
				"output_tokens_details": {"reasoning_tokens": 4}
			}
		}
	}`
	usage := &RequestLog{}
	hook := RequestLogHook(nil, "co"+"dex", usage)

	hook([]byte(payload))
	hook([]byte(payload))

	if usage.InputTokens != 70 {
		t.Fatalf("InputTokens=%d, want 70", usage.InputTokens)
	}
	// Responses 的 output_tokens 含 reasoning_tokens,而计费是 OutputCost+ReasoningCost 相加,
	// 入库前必须拆成互不重叠的两桶,否则推理 token 被计两次
	if usage.OutputTokens != 16 {
		t.Fatalf("OutputTokens=%d, want 16(20-4 推理)", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 30 {
		t.Fatalf("CacheReadTokens=%d, want 30", usage.CacheReadTokens)
	}
	if usage.ReasoningTokens != 4 {
		t.Fatalf("ReasoningTokens=%d, want 4", usage.ReasoningTokens)
	}
}

func TestResponseUsageSeparatesCachedInputTokens(t *testing.T) {
	payload := `{
		"type": "response.completed",
		"response": {
			"usage": {
				"input_tokens": 100,
				"output_tokens": 20,
				"input_tokens_details": {"cached_tokens": 30},
				"output_tokens_details": {"reasoning_tokens": 4}
			}
		}
	}`
	usage := &RequestLog{}
	hook := RequestLogHook(nil, "co"+"dex", usage)

	hook([]byte(payload))

	if usage.InputTokens != 70 {
		t.Fatalf("InputTokens=%d, want 70", usage.InputTokens)
	}
	if usage.CacheReadTokens != 30 {
		t.Fatalf("CacheReadTokens=%d, want 30", usage.CacheReadTokens)
	}
}

func TestResponseUsageLatestSnapshotCanReduceUncachedInput(t *testing.T) {
	firstPayload := `{
		"type": "response.in_progress",
		"response": {
			"usage": {
				"input_tokens": 100,
				"output_tokens": 10
			}
		}
	}`
	finalPayload := `{
		"type": "response.completed",
		"response": {
			"usage": {
				"input_tokens": 100,
				"output_tokens": 20,
				"input_tokens_details": {"cached_tokens": 30},
				"output_tokens_details": {"reasoning_tokens": 4}
			}
		}
	}`
	usage := &RequestLog{}
	hook := RequestLogHook(nil, "co"+"dex", usage)

	hook([]byte(firstPayload))
	hook([]byte(finalPayload))

	if usage.InputTokens != 70 {
		t.Fatalf("InputTokens=%d, want 70", usage.InputTokens)
	}
	// 同上:output_tokens 已含 reasoning_tokens,入库前拆分避免重复计费
	if usage.OutputTokens != 16 {
		t.Fatalf("OutputTokens=%d, want 16(20-4 推理)", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 30 {
		t.Fatalf("CacheReadTokens=%d, want 30", usage.CacheReadTokens)
	}
	if usage.ReasoningTokens != 4 {
		t.Fatalf("ReasoningTokens=%d, want 4", usage.ReasoningTokens)
	}
}

// ==================== ReplaceModelInRequestBody 测试 ====================

func TestReplaceModelInRequestBody(t *testing.T) {
	tests := []struct {
		name          string
		inputJSON     string
		newModel      string
		expectError   bool
		expectedModel string
	}{
		// 成功场景
		{
			name: "简单替换",
			inputJSON: `{
				"model": "gpt-sonnet-4",
				"messages": [{"role": "user", "content": "Hello"}]
			}`,
			newModel:      "gateway/gpt-sonnet-4",
			expectError:   false,
			expectedModel: "gateway/gpt-sonnet-4",
		},
		{
			name: "复杂嵌套JSON",
			inputJSON: `{
				"model": "gpt-opus-4",
				"messages": [
					{
						"role": "user",
						"content": "Test"
					}
				],
				"temperature": 0.7,
				"max_tokens": 1000,
				"metadata": {
					"user_id": "12345"
				}
			}`,
			newModel:      "gpt-4",
			expectError:   false,
			expectedModel: "gpt-4",
		},
		{
			name: "模型名包含特殊字符",
			inputJSON: `{
				"model": "gpt-sonnet-4",
				"messages": []
			}`,
			newModel:      "gateway/gpt-3.5-sonnet@20241022",
			expectError:   false,
			expectedModel: "gateway/gpt-3.5-sonnet@20241022",
		},

		// 错误场景
		{
			name: "缺少model字段",
			inputJSON: `{
				"messages": [{"role": "user", "content": "Hello"}]
			}`,
			newModel:    "any-model",
			expectError: true,
		},
		{
			name: "空JSON",
			inputJSON: `{
			}`,
			newModel:    "any-model",
			expectError: true,
		},
		{
			name:        "无效JSON",
			inputJSON:   `{invalid json}`,
			newModel:    "any-model",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bodyBytes := []byte(tt.inputJSON)
			result, err := ReplaceModelInRequestBody(bodyBytes, tt.newModel)

			// 检查错误预期
			if tt.expectError && err == nil {
				t.Errorf("期望返回错误，但没有错误")
			}
			if !tt.expectError && err != nil {
				t.Errorf("不期望错误，但返回了: %v", err)
			}

			// 如果不期望错误，验证结果
			if !tt.expectError {
				// 验证返回的JSON是否有效
				if !json.Valid(result) {
					t.Errorf("返回的JSON无效")
				}

				// 验证模型名是否正确替换
				actualModel := gjson.GetBytes(result, "model").String()
				if actualModel != tt.expectedModel {
					t.Errorf("替换后的模型名 = %q, 期望 %q", actualModel, tt.expectedModel)
				}

				// 验证其他字段未被修改
				if gjson.GetBytes(bodyBytes, "messages").Exists() {
					originalMessages := gjson.GetBytes(bodyBytes, "messages").Raw
					resultMessages := gjson.GetBytes(result, "messages").Raw
					if originalMessages != resultMessages {
						t.Errorf("messages 字段被意外修改")
					}
				}
			}
		})
	}
}

// ==================== 端到端场景测试 ====================

func TestModelMappingEndToEnd(t *testing.T) {
	// 模拟兼容网关对不同 OpenAI 模型族使用不同前缀的场景。
	provider := Provider{
		Name: "OpenRouter",
		SupportedModels: map[string]bool{
			"gateway/gpt-sonnet-4":      true,
			"gateway/gpt-opus-4":        true,
			"openai/gpt-4":              true,
			"openai/o-model-pro":        true,
			"meta-llama/llama-3.1-405b": true,
			"gateway/gpt-3.5-sonnet":    true,
			"gateway/gpt-3.5-haiku":     true,
		},
		ModelMapping: map[string]string{
			"gpt-sonnet-*": "gateway/gpt-sonnet-*",
			"gpt-opus-*":   "gateway/gpt-opus-*",
			"gpt-*-sonnet": "gateway/gpt-*-sonnet",
			"gpt-*":        "openai/gpt-*",
			"o-model-*":    "openai/o-model-*",
			"llama-*":      "meta-llama/llama-*",
		},
	}

	scenarios := []struct {
		requestedModel string
		shouldSupport  bool
		effectiveModel string
	}{
		// 通配符映射场景
		{"gpt-sonnet-4", true, "gateway/gpt-sonnet-4"},
		{"gpt-opus-4", true, "gateway/gpt-opus-4"},
		{"gpt-3.5-sonnet", true, "gateway/gpt-3.5-sonnet"},
		{"gpt-4", true, "openai/gpt-4"},
		{"gpt-4-turbo", true, "openai/gpt-4-turbo"},
		{"o-model-pro", true, "openai/o-model-pro"},
		{"llama-3.1-405b", true, "meta-llama/llama-3.1-405b"},

		// 不支持的模型
		{"deepseek-v3", false, "deepseek-v3"},
		{"qwen-max", false, "qwen-max"},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.requestedModel, func(t *testing.T) {
			// 1. 检查是否支持
			supported := provider.IsModelSupported(scenario.requestedModel)
			if supported != scenario.shouldSupport {
				t.Errorf("IsModelSupported(%q) = %v, 期望 %v",
					scenario.requestedModel, supported, scenario.shouldSupport)
			}

			// 2. 获取有效模型名
			effectiveModel := provider.GetEffectiveModel(scenario.requestedModel)
			if effectiveModel != scenario.effectiveModel {
				t.Errorf("GetEffectiveModel(%q) = %q, 期望 %q",
					scenario.requestedModel, effectiveModel, scenario.effectiveModel)
			}

			// 3. 如果支持，测试请求体替换
			if scenario.shouldSupport {
				requestBody := `{"model": "` + scenario.requestedModel + `", "messages": []}`
				result, err := ReplaceModelInRequestBody([]byte(requestBody), effectiveModel)
				if err != nil {
					t.Fatalf("ReplaceModelInRequestBody 失败: %v", err)
				}

				actualModel := gjson.GetBytes(result, "model").String()
				if actualModel != scenario.effectiveModel {
					t.Errorf("请求体中的模型 = %q, 期望 %q", actualModel, scenario.effectiveModel)
				}
			}
		})
	}
}

// ==================== 配置验证集成测试 ====================

func TestProviderConfigValidation(t *testing.T) {
	// 场景 1：完美配置
	validProvider := Provider{
		Name: "ValidProvider",
		SupportedModels: map[string]bool{
			"gateway/gpt-sonnet-4": true,
			"gateway/gpt-opus-4":   true,
		},
		ModelMapping: map[string]string{
			"gpt-sonnet-4": "gateway/gpt-sonnet-4",
			"gpt-opus-4":   "gateway/gpt-opus-4",
		},
	}

	errors := validProvider.ValidateConfiguration()
	if len(errors) != 0 {
		t.Errorf("完美配置不应有错误，但返回了: %v", errors)
	}

	// 场景 2：错误配置 - 映射目标为空
	invalidProvider := Provider{
		Name: "InvalidProvider",
		ModelMapping: map[string]string{
			"external": "",
		},
	}

	errors = invalidProvider.ValidateConfiguration()
	if len(errors) == 0 {
		t.Errorf("错误配置应该返回验证错误")
	}

	// 场景 3：通配符配置
	wildcardProvider := Provider{
		Name: "WildcardProvider",
		SupportedModels: map[string]bool{
			"gateway/gpt-*": true,
			"openai/gpt-*":  true,
		},
		ModelMapping: map[string]string{
			"gpt-sonnet-*": "gateway/gpt-sonnet-*",
			"gpt-*":        "openai/gpt-*",
		},
	}

	errors = wildcardProvider.ValidateConfiguration()
	if len(errors) != 0 {
		t.Errorf("通配符配置不应有错误，但返回了: %v", errors)
	}
}

// ==================== 性能测试 ====================

func BenchmarkIsModelSupported(b *testing.B) {
	provider := Provider{
		SupportedModels: map[string]bool{
			"gpt-sonnet-4": true,
			"gpt-opus-4":   true,
			"gpt-4":        true,
			"gpt-4-turbo":  true,
		},
		ModelMapping: map[string]string{
			"gpt-sonnet-*": "gateway/gpt-sonnet-*",
			"gpt-*":        "openai/gpt-*",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = provider.IsModelSupported("gpt-sonnet-4")
	}
}

func BenchmarkGetEffectiveModel(b *testing.B) {
	provider := Provider{
		ModelMapping: map[string]string{
			"gpt-sonnet-*": "gateway/gpt-sonnet-*",
			"gpt-*":        "openai/gpt-*",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = provider.GetEffectiveModel("gpt-sonnet-4")
	}
}

func BenchmarkReplaceModelInRequestBody(b *testing.B) {
	bodyBytes := []byte(`{
		"model": "gpt-sonnet-4",
		"messages": [{"role": "user", "content": "Hello"}],
		"temperature": 0.7,
		"max_tokens": 1000
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ReplaceModelInRequestBody(bodyBytes, "gateway/gpt-sonnet-4")
	}
}
