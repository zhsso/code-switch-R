package services

import (
	"testing"
	"time"
)

// TestHealthCheckPolling_StopStartLeavesSinglePoller 验证 Stop→Start 快速切换后只剩一个巡检协程。
// Stop 必须等待旧调度循环退出，避免新旧循环并行导致失败计数和历史记录翻倍。
func TestHealthCheckPolling_StopStartLeavesSinglePoller(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// Windows 的 os.UserHomeDir() 读的是 USERPROFILE,只设 HOME 会让测试写到真实用户配置目录
	t.Setenv("USERPROFILE", tmpHome)
	assertHomeIsolated(t, tmpHome)

	hcs := NewHealthCheckService(NewProviderService(), nil, nil, nil)
	t.Cleanup(func() {
		hcs.StopBackgroundPolling()
	})

	hcs.StartBackgroundPolling()
	// 立即 Stop→Start：关闭旧 channel 并换新，触发旧实现的竞态窗口。
	hcs.StopBackgroundPolling()
	hcs.StartBackgroundPolling()

	// 无供应商配置，调度轮次应迅速完成并稳定为一个循环。
	if got := waitForStableGoroutineCount(t, "StartBackgroundPolling.func", 1, 25*time.Second); got != 1 {
		t.Errorf("巡检开启中应恰有 1 个巡检协程，实际 %d（旧协程未退出说明 Stop→Start 竞态仍在）", got)
	}
}
