package modelpricing

import "testing"

// TestReasoningFallbackToOutputRate 回归:条目缺 reasoning 单价时,推理 token 一律按
// output 单价回退计费。ReasoningTokens 与 OutputTokens 在入库前已被拆成互不重叠的两桶
// (OpenAI Responses 的 reasoning_tokens 在 CodexParseTokenUsageFromResponse 里已从
// output_tokens 扣除),所以回退不会重复计费。
func TestReasoningFallbackToOutputRate(t *testing.T) {
	svc := newTestService(t)
	// OpenAI 条目没有 reasoning 单价时,推理 token 必须按 output 单价计费,
	// 否则 codex 的推理消耗全部记 0
	gptEntry, ok := svc.currentSnapshot().pricingMap["gpt-5"]
	if !ok || gptEntry.OutputCostPerReasoningToken > 0 {
		t.Skip("gpt-5 样本不满足前提")
	}
	gpt := svc.CalculateCost("gpt-5", UsageSnapshot{
		InputTokens:     1000,
		OutputTokens:    500,
		ReasoningTokens: 400,
	})
	assertApprox(t, gpt.ReasoningCost, 400*gptEntry.OutputCostPerToken)

	// 带 reasoning 单价的条目仍按专用单价计费,不被回退覆盖
	for _, name := range []string{"qwen-turbo", "dashscope/qwen-turbo"} {
		rich, ok := svc.currentSnapshot().pricingMap[name]
		if !ok || rich.OutputCostPerReasoningToken <= 0 {
			continue
		}
		got := svc.CalculateCost(name, UsageSnapshot{OutputTokens: 100, ReasoningTokens: 100})
		assertApprox(t, got.ReasoningCost, 100*rich.OutputCostPerReasoningToken)
		break
	}
}
