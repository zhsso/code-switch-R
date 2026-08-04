package services

import (
	"fmt"
	"strings"
)

const CodexPlatform = "codex"

func requireCodexPlatform(platform string) error {
	if strings.ToLower(strings.TrimSpace(platform)) != CodexPlatform {
		return fmt.Errorf("不支持的平台: %s", platform)
	}
	return nil
}
