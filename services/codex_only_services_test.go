package services

import "testing"

func TestDefaultModelPolicySupportsCodexOnly(t *testing.T) {
	policy := NewDefaultModelPolicy()
	if got := policy.ProbeModel(CodexPlatform); got == "" {
		t.Fatal("codex probe model should not be empty")
	}
	for _, platform := range []string{"claude", "gemini"} {
		if got := policy.ProbeModel(platform); got != "" {
			t.Errorf("ProbeModel(%q) = %q, want empty", platform, got)
		}
		if got := policy.ProbeCandidates(platform); len(got) != 0 {
			t.Errorf("ProbeCandidates(%q) = %v, want nil/empty", platform, got)
		}
	}
}
