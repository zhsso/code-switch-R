package services

import (
	"testing"
)

// newCaptureTestRelay 构造仅用于会话测试的 relay（不启动 HTTP 服务）
func newCaptureTestRelay(t *testing.T) *ProviderRelayService {
	t.Helper()
	appSettings := NewAppSettingsService()
	notificationService := NewNotificationService(appSettings)
	blacklistService := NewBlacklistService(NewSettingsService(), notificationService)
	return NewProviderRelayService(NewProviderService(),
		blacklistService, notificationService, appSettings, "")
}

// 开关生命周期：开启建会话、关闭封会话，重复置位幂等
func TestCaptureSessionLifecycle(t *testing.T) {
	setupCaptureDBEnv(t)
	relay := newCaptureTestRelay(t)

	if err := relay.SetRequestCapture(true); err != nil {
		t.Fatalf("开启失败: %v", err)
	}
	if err := relay.SetRequestCapture(true); err != nil {
		t.Fatalf("重复开启应幂等: %v", err)
	}
	sessions, err := relay.ListCaptureSessions()
	if err != nil {
		t.Fatalf("列会话失败: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("应恰有 1 个会话, 实际 %d", len(sessions))
	}
	if !sessions[0].Active || sessions[0].EndedAt != "" {
		t.Errorf("录制中的会话应 Active 且未结束: %+v", sessions[0])
	}

	if err := relay.SetRequestCapture(false); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	sessions, _ = relay.ListCaptureSessions()
	if len(sessions) != 1 || sessions[0].Active || sessions[0].EndedAt == "" || sessions[0].Interrupted {
		t.Errorf("关闭后会话应已封存且非中断: %+v", sessions[0])
	}
}

// 捕获行携带会话 id；单会话删除清内容并摘关联；活动会话删除后原地轮换
func TestCaptureSessionTaggingAndDelete(t *testing.T) {
	db := setupCaptureDBEnv(t)
	relay := newCaptureTestRelay(t)

	if err := relay.SetRequestCapture(true); err != nil {
		t.Fatalf("开启失败: %v", err)
	}
	first := relay.captureSessionID.Load()
	if first == 0 {
		t.Fatal("开启后应有活动会话")
	}

	// 模拟一条带抓包内容的落库
	requestLog := &ReqeustLog{
		Platform: "codex", Provider: "p", Model: "m",
		RequestHeaders: `{"a":"b"}`, RequestBody: `{"x":1}`, BodyBytes: 7,
		CaptureSessionID: first, captureGen: relay.captureClearGen.Load(),
	}
	relay.captureWriteMu.RLock()
	relay.stripStaleCapture(requestLog)
	err := relay.writeRequestLog(requestLog)
	relay.captureWriteMu.RUnlock()
	if err != nil {
		t.Fatalf("落库失败: %v", err)
	}

	var gotSession int64
	if err := db.QueryRow(`SELECT capture_session_id FROM request_log ORDER BY id DESC LIMIT 1`).Scan(&gotSession); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if gotSession != first {
		t.Fatalf("捕获行应携带会话 id %d, 实际 %d", first, gotSession)
	}

	rows, err := relay.GetCaptureSessionLogs(first, 0, 0, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("会话行查询: rows=%d err=%v", len(rows), err)
	}

	// 删除活动会话：内容清空、会话轮换、墓碑生效
	affected, err := relay.DeleteCaptureSession(first)
	if err != nil {
		t.Fatalf("删除会话失败: %v", err)
	}
	if affected != 1 {
		t.Errorf("应清理 1 行, 实际 %d", affected)
	}
	second := relay.captureSessionID.Load()
	if second == 0 || second == first {
		t.Fatalf("活动会话删除后应轮换出新会话: first=%d second=%d", first, second)
	}
	if !relay.captureRequests.Load() {
		t.Error("轮换后录制应保持开启")
	}
	var headers string
	var sid int64
	if err := db.QueryRow(`SELECT request_headers, capture_session_id FROM request_log ORDER BY id DESC LIMIT 1`).
		Scan(&headers, &sid); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if headers != "" || sid != 0 {
		t.Errorf("删除后行内容应清空且摘除关联: headers=%q sid=%d", headers, sid)
	}

	// 墓碑：属于已删除会话的在途行落库时自我置空
	late := &ReqeustLog{
		Platform: "codex", Provider: "p", Model: "m",
		RequestHeaders: `{"late":"1"}`, RequestBody: `{"late":1}`, BodyBytes: 9,
		CaptureSessionID: first, captureGen: relay.captureClearGen.Load(),
	}
	relay.captureWriteMu.RLock()
	relay.stripStaleCapture(late)
	relay.captureWriteMu.RUnlock()
	if late.RequestHeaders != "" || late.CaptureSessionID != 0 {
		t.Errorf("已删除会话的在途行应被置空: %+v", late)
	}
}

// 清空全部：所有会话与旧数据一并清除、代次推进、录制中轮换新会话
func TestCaptureClearAllRotatesActiveSession(t *testing.T) {
	db := setupCaptureDBEnv(t)
	relay := newCaptureTestRelay(t)

	// 预置旧版抓包行（capture_session_id=0）
	if _, err := db.Exec(`INSERT INTO request_log (platform, provider, model, request_headers, request_body, body_bytes)
		VALUES ('codex', 'legacy', 'm', '{"h":"1"}', '{"b":1}', 7)`); err != nil {
		t.Fatalf("预置旧数据失败: %v", err)
	}

	if err := relay.SetRequestCapture(true); err != nil {
		t.Fatalf("开启失败: %v", err)
	}
	first := relay.captureSessionID.Load()
	genBefore := relay.captureClearGen.Load()

	sessions, _ := relay.ListCaptureSessions()
	foundLegacy := false
	for _, s := range sessions {
		if s.Legacy && s.RequestCount == 1 {
			foundLegacy = true
		}
	}
	if !foundLegacy {
		t.Errorf("应看见旧数据伪会话: %+v", sessions)
	}

	affected, err := relay.ClearCapturedRequests()
	if err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	if affected != 1 {
		t.Errorf("应清理 1 行旧数据, 实际 %d", affected)
	}
	if relay.captureClearGen.Load() != genBefore+1 {
		t.Error("清空应推进代次")
	}
	rotated := relay.captureSessionID.Load()
	if rotated == 0 || rotated == first {
		t.Errorf("清空后应轮换新会话: first=%d rotated=%d", first, rotated)
	}
	var sessionCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM capture_session`).Scan(&sessionCount); err != nil {
		t.Fatal(err)
	}
	if sessionCount != 1 {
		t.Errorf("清空后应只剩轮换出的 1 个会话, 实际 %d", sessionCount)
	}
}

// 遗留未关闭会话在进程首次触碰会话状态时标记为已中断
func TestCaptureStaleSessionRecovery(t *testing.T) {
	db := setupCaptureDBEnv(t)

	if _, err := db.Exec(`INSERT INTO capture_session (started_at) VALUES ('2026-08-01 00:00:00')`); err != nil {
		t.Fatalf("预置遗留会话失败: %v", err)
	}
	var staleID int64
	if err := db.QueryRow(`SELECT id FROM capture_session`).Scan(&staleID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO request_log (platform, provider, model, request_headers, capture_session_id, created_at)
		VALUES ('codex', 'p', 'm', '{"h":"1"}', ?, '2026-08-01 00:10:00')`, staleID); err != nil {
		t.Fatal(err)
	}

	relay := newCaptureTestRelay(t)
	sessions, err := relay.ListCaptureSessions()
	if err != nil {
		t.Fatalf("列会话失败: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("应有 1 个会话, 实际 %d", len(sessions))
	}
	s := sessions[0]
	if !s.Interrupted {
		t.Error("遗留会话应标记为已中断")
	}
	if s.EndedAt != "2026-08-01 00:10:00" {
		t.Errorf("结束时间应取最后一条捕获时间, 实际 %q", s.EndedAt)
	}
	if s.RequestCount != 1 {
		t.Errorf("会话行数应为 1, 实际 %d", s.RequestCount)
	}
}

// 增量与翻页游标
func TestCaptureSessionLogCursors(t *testing.T) {
	db := setupCaptureDBEnv(t)
	relay := newCaptureTestRelay(t)
	if err := relay.SetRequestCapture(true); err != nil {
		t.Fatal(err)
	}
	sid := relay.captureSessionID.Load()
	for i := 0; i < 5; i++ {
		if _, err := db.Exec(`INSERT INTO request_log (platform, provider, model, request_headers, capture_session_id)
			VALUES ('codex', 'p', 'm', '{"h":"1"}', ?)`, sid); err != nil {
			t.Fatal(err)
		}
	}
	all, err := relay.GetCaptureSessionLogs(sid, 0, 0, 0)
	if err != nil || len(all) != 5 {
		t.Fatalf("初始查询应 5 行: %d %v", len(all), err)
	}
	if all[0].ID < all[4].ID {
		t.Error("初始模式应新行在前")
	}
	// 翻页：取比最旧行更旧的（应为空集之前先验证 beforeID 生效）
	older, err := relay.GetCaptureSessionLogs(sid, 0, all[0].ID, 2)
	if err != nil || len(older) != 2 {
		t.Fatalf("beforeID 翻页应 2 行: %d %v", len(older), err)
	}
	if older[0].ID >= all[0].ID {
		t.Error("翻页行应早于游标")
	}
	// 增量：sinceID 之后的新行、升序
	newer, err := relay.GetCaptureSessionLogs(sid, all[4].ID, 0, 0)
	if err != nil || len(newer) != 4 {
		t.Fatalf("sinceID 增量应 4 行: %d %v", len(newer), err)
	}
	if newer[0].ID > newer[len(newer)-1].ID {
		t.Error("增量模式应升序")
	}
}

func TestCaptureSessionsExposeCodexRowsOnly(t *testing.T) {
	db := setupCaptureDBEnv(t)
	relay := newCaptureTestRelay(t)

	newSession := func(start string) int64 {
		t.Helper()
		result, err := db.Exec(`INSERT INTO capture_session (started_at, ended_at) VALUES (?, ?)`, start, start)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	mixedID := newSession("2026-08-01 00:00:00")
	removedOnlyID := newSession("2026-08-01 01:00:00")
	emptyID := newSession("2026-08-01 02:00:00")

	rows := []struct {
		platform string
		body     string
		session  int64
	}{
		{CodexPlatform, "codex-mixed", mixedID},
		{"claude", "removed-mixed", mixedID},
		{"claude", "removed-only", removedOnlyID},
		{CodexPlatform, "codex-legacy", 0},
		{"claude", "removed-legacy", 0},
	}
	for _, row := range rows {
		if _, err := db.Exec(`INSERT INTO request_log
			(platform, provider, model, request_body, capture_session_id)
			VALUES (?, 'p', 'm', ?, ?)`, row.platform, row.body, row.session); err != nil {
			t.Fatal(err)
		}
	}

	sessions, err := relay.ListCaptureSessions()
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[int64]int64, len(sessions))
	for _, session := range sessions {
		counts[session.ID] = session.RequestCount
	}
	if counts[mixedID] != 1 || counts[emptyID] != 0 || counts[0] != 1 {
		t.Fatalf("Codex 会话计数异常: %+v", sessions)
	}
	if _, ok := counts[removedOnlyID]; ok {
		t.Fatalf("仅含旧平台数据的会话不应展示: %+v", sessions)
	}

	total, err := relay.GetCaptureTotalBytes()
	if err != nil {
		t.Fatal(err)
	}
	wantBytes := int64(len("codex-mixed") + len("codex-legacy"))
	if total != wantBytes {
		t.Fatalf("Codex 抓包字节 = %d, want %d", total, wantBytes)
	}

	logs, err := relay.GetCaptureSessionLogs(mixedID, 0, 0, 10)
	if err != nil || len(logs) != 1 || logs[0].Platform != CodexPlatform {
		t.Fatalf("混合会话日志 = %+v, err=%v", logs, err)
	}
	legacy, err := relay.GetCaptureSessionLogs(0, 0, 0, 10)
	if err != nil || len(legacy) != 1 || legacy[0].Platform != CodexPlatform {
		t.Fatalf("旧版伪会话日志 = %+v, err=%v", legacy, err)
	}

}

func TestCaptureCleanupPreservesRemovedPlatformRows(t *testing.T) {
	db := setupCaptureDBEnv(t)
	relay := newCaptureTestRelay(t)

	newSession := func(start string) int64 {
		t.Helper()
		result, err := db.Exec(`INSERT INTO capture_session (started_at, ended_at) VALUES (?, ?)`, start, start)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	deleteMixedID := newSession("2026-08-02 00:00:00")
	removedOnlyID := newSession("2026-08-02 01:00:00")
	clearMixedID := newSession("2026-08-02 02:00:00")

	rows := []struct {
		platform string
		body     string
		session  int64
	}{
		{CodexPlatform, "codex-delete", deleteMixedID},
		{"claude", "removed-delete", deleteMixedID},
		{"claude", "removed-only", removedOnlyID},
		{CodexPlatform, "codex-clear", clearMixedID},
		{"claude", "removed-clear", clearMixedID},
		{CodexPlatform, "codex-legacy", 0},
		{"claude", "removed-legacy", 0},
	}
	for _, row := range rows {
		if _, err := db.Exec(`INSERT INTO request_log
			(platform, provider, model, request_body, capture_session_id)
			VALUES (?, 'p', 'm', ?, ?)`, row.platform, row.body, row.session); err != nil {
			t.Fatal(err)
		}
	}

	affected, err := relay.DeleteCaptureSession(deleteMixedID)
	if err != nil || affected != 1 {
		t.Fatalf("删除混合会话 affected=%d err=%v, want 1 条 Codex", affected, err)
	}
	var removedBody string
	var removedSession int64
	if err := db.QueryRow(`SELECT request_body, capture_session_id FROM request_log
		WHERE platform = 'claude' AND request_body = 'removed-delete'`).Scan(&removedBody, &removedSession); err != nil {
		t.Fatal(err)
	}
	if removedBody != "removed-delete" || removedSession != deleteMixedID {
		t.Fatalf("删除会话改写了旧平台行: body=%q session=%d", removedBody, removedSession)
	}
	var metadataCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM capture_session WHERE id = ?`, deleteMixedID).Scan(&metadataCount); err != nil {
		t.Fatal(err)
	}
	if metadataCount != 1 {
		t.Fatal("仍被旧平台行引用的会话元数据不应删除")
	}

	affected, err = relay.ClearCapturedRequests()
	if err != nil || affected != 2 {
		t.Fatalf("清空全部 affected=%d err=%v, want 2 条 Codex", affected, err)
	}
	var removedPayloads, codexPayloads int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_log WHERE platform = 'claude' AND request_body != ''`).Scan(&removedPayloads); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_log WHERE platform = 'codex' AND request_body != ''`).Scan(&codexPayloads); err != nil {
		t.Fatal(err)
	}
	if removedPayloads != 4 || codexPayloads != 0 {
		t.Fatalf("清空后旧平台 payload=%d, Codex payload=%d", removedPayloads, codexPayloads)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM capture_session`).Scan(&metadataCount); err != nil {
		t.Fatal(err)
	}
	if metadataCount != 3 {
		t.Fatalf("旧平台仍引用的会话元数据应保留 3 条,实际 %d", metadataCount)
	}
	if sessions, err := relay.ListCaptureSessions(); err != nil || len(sessions) != 0 {
		t.Fatalf("清空 Codex 后旧平台会话应全部隐藏: %+v err=%v", sessions, err)
	}
}
