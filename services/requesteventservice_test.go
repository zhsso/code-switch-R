package services

import (
	"net/http"
	"testing"
	"time"

	"github.com/daodao97/xgo/xdb"
)

func TestRequestEventServiceRecordAndListSanitizesMessage(t *testing.T) {
	setupRenameTestEnv(t)
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("get test database: %v", err)
	}
	if err := ensureRequestEventTable(); err != nil {
		t.Fatalf("create request event table: %v", err)
	}

	oldQueue := GlobalDBQueue
	GlobalDBQueue = NewDBWriteQueue(db, 100, false)
	t.Cleanup(func() {
		_ = GlobalDBQueue.Shutdown(2 * time.Second)
		GlobalDBQueue = oldQueue
	})

	service := NewRequestEventService()
	if err := service.Record(RequestEventInput{
		RequestID: "request-1",
		Platform:  CodexPlatform,
		Model:     "gpt-5.6",
		EventType: RequestEventError,
		Provider:  "primary",
		Attempt:   1,
		HTTPCode:  503,
		ErrorType: "provider_error",
		ErrorCode: "provider_request_failed",
		Message:   `authorization: Bearer secret-value`,
		Outcome:   "continued",
	}); err != nil {
		t.Fatalf("record error event: %v", err)
	}
	if err := service.Record(RequestEventInput{
		RequestID:    "request-1",
		Platform:     CodexPlatform,
		Model:        "gpt-5.6",
		EventType:    RequestEventSwitch,
		FromProvider: "primary",
		ToProvider:   "backup",
		Attempt:      2,
		Message:      "upstream HTTP 503",
		Outcome:      "continued",
	}); err != nil {
		t.Fatalf("record switch event: %v", err)
	}

	var storedMessage string
	if err := db.QueryRow(`SELECT message FROM request_event_log WHERE event_type = ?`, RequestEventError).Scan(&storedMessage); err != nil {
		t.Fatalf("read sanitized event: %v", err)
	}
	if storedMessage == `authorization: Bearer secret-value` || storedMessage == "" {
		t.Fatalf("event message was not sanitized: %q", storedMessage)
	}

	logs := NewLogService()
	events, err := logs.ListRequestEvents(CodexPlatform, "incident", "", "request-1", 1, 10, 0)
	if err != nil {
		t.Fatalf("list request events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2: %#v", len(events), events)
	}
	if events[0].EventType != RequestEventSwitch || events[1].EventType != RequestEventError {
		t.Fatalf("events not ordered by newest first: %#v", events)
	}
}

func TestRequestEventErrorDetailsClassifiesCapacity(t *testing.T) {
	err := newUpstreamModelCapacityError(nil, 503, "upstream status 503")
	errorType, errorCode, message, httpCode := requestEventErrorDetails(err)
	if errorType != "model_capacity" || errorCode != "model_at_capacity" || httpCode != 503 {
		t.Fatalf("unexpected event details: type=%q code=%q http=%d message=%q", errorType, errorCode, httpCode, message)
	}
}

func TestEnsureRequestEventTableNormalizesTerminalOutcomes(t *testing.T) {
	setupRenameTestEnv(t)
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("get test database: %v", err)
	}
	if err := ensureRequestEventTable(); err != nil {
		t.Fatalf("create request event table: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO request_event_log (request_id, platform, event_type, provider, attempt, error_type, outcome)
		VALUES
			('client-abort', 'codex', 'request_error', 'primary', 1, 'client_aborted', 'continued'),
			('client-abort', 'codex', 'request_completed', 'primary', 1, '', 'success'),
			('stream-abort', 'codex', 'request_error', 'primary', 1, 'stream_aborted', 'continued'),
			('stream-abort', 'codex', 'request_completed', 'primary', 1, '', 'success'),
			('routine-success', 'codex', 'request_completed', 'primary', 1, '', 'success'),
			('fallback-success', 'codex', 'request_error', 'primary', 1, 'provider_error', 'continued'),
			('fallback-success', 'codex', 'request_completed', 'backup', 2, '', 'success')
	`); err != nil {
		t.Fatalf("seed legacy outcomes: %v", err)
	}
	if err := ensureRequestEventTable(); err != nil {
		t.Fatalf("normalize request event outcomes: %v", err)
	}

	for _, testCase := range []struct {
		requestID string
		want      string
	}{
		{requestID: "client-abort", want: "client_aborted"},
		{requestID: "stream-abort", want: "failed"},
	} {
		var count int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM request_event_log WHERE request_id = ? AND outcome = ?`,
			testCase.requestID,
			testCase.want,
		).Scan(&count); err != nil {
			t.Fatalf("count normalized outcomes for %s: %v", testCase.requestID, err)
		}
		if count != 2 {
			t.Fatalf("normalized rows for %s = %d, want 2", testCase.requestID, count)
		}
	}

	var routineCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_event_log WHERE request_id = 'routine-success'`).Scan(&routineCount); err != nil {
		t.Fatalf("count routine success events: %v", err)
	}
	if routineCount != 0 {
		t.Fatalf("routine success events = %d, want 0", routineCount)
	}
	var fallbackCompletionCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_event_log WHERE request_id = 'fallback-success' AND event_type = 'request_completed'`).Scan(&fallbackCompletionCount); err != nil {
		t.Fatalf("count fallback completion events: %v", err)
	}
	if fallbackCompletionCount != 1 {
		t.Fatalf("fallback completion events = %d, want 1", fallbackCompletionCount)
	}
}

func TestBlacklistStatusIncludesLastFailureReason(t *testing.T) {
	setupBlacklistFixEnv(t)
	setAppSetting(t, "enable_blacklist", "true")

	service := NewBlacklistService(NewSettingsService(), nil)
	if err := service.RecordFailureWithReason(CodexPlatform, "reason-provider", "upstream model at capacity"); err != nil {
		t.Fatalf("record blacklist failure: %v", err)
	}

	statuses, err := service.GetBlacklistStatus(CodexPlatform)
	if err != nil {
		t.Fatalf("list blacklist status: %v", err)
	}
	if len(statuses) != 1 || statuses[0].LastFailureReason != "upstream model at capacity" {
		t.Fatalf("unexpected blacklist reason: %#v", statuses)
	}
}

func TestRelayRequestTraceCorrelatesFailureSwitchAndCompletion(t *testing.T) {
	setupRenameTestEnv(t)
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("get test database: %v", err)
	}
	if err := ensureRequestEventTable(); err != nil {
		t.Fatalf("create request event table: %v", err)
	}
	oldQueue := GlobalDBQueue
	GlobalDBQueue = NewDBWriteQueue(db, 100, false)
	t.Cleanup(func() {
		_ = GlobalDBQueue.Shutdown(2 * time.Second)
		GlobalDBQueue = oldQueue
	})

	trace := newRelayRequestTrace(NewRequestEventService(), CodexPlatform)
	trace.SetModel("gpt-5.6")
	firstAttempt := trace.BeforeAttempt("primary")
	trace.RecordForwardError("primary", newUpstreamStatusError(nil, 503, "upstream status 503"), firstAttempt, 1, 250*time.Millisecond)
	trace.BeforeAttempt("backup")
	trace.Finish(502, false)

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_event_log WHERE request_id = ?`, trace.RequestID()).Scan(&count); err != nil {
		t.Fatalf("count trace events: %v", err)
	}
	if count != 3 {
		t.Fatalf("trace events = %d, want error + switch + completion", count)
	}
}

func TestRelayRequestTraceSkipsRoutineSuccessCompletion(t *testing.T) {
	setupRenameTestEnv(t)
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("get test database: %v", err)
	}
	if err := ensureRequestEventTable(); err != nil {
		t.Fatalf("create request event table: %v", err)
	}
	oldQueue := GlobalDBQueue
	GlobalDBQueue = NewDBWriteQueue(db, 100, false)
	t.Cleanup(func() {
		_ = GlobalDBQueue.Shutdown(2 * time.Second)
		GlobalDBQueue = oldQueue
	})

	trace := newRelayRequestTrace(NewRequestEventService(), CodexPlatform)
	trace.SetModel("gpt-5.6")
	trace.BeforeAttempt("primary")
	trace.Finish(http.StatusOK, false)

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_event_log WHERE request_id = ?`, trace.RequestID()).Scan(&count); err != nil {
		t.Fatalf("count routine success events: %v", err)
	}
	if count != 0 {
		t.Fatalf("routine success events = %d, want 0", count)
	}
}

func TestRelayRequestTraceKeepsCompletionAfterFallback(t *testing.T) {
	setupRenameTestEnv(t)
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("get test database: %v", err)
	}
	if err := ensureRequestEventTable(); err != nil {
		t.Fatalf("create request event table: %v", err)
	}
	oldQueue := GlobalDBQueue
	GlobalDBQueue = NewDBWriteQueue(db, 100, false)
	t.Cleanup(func() {
		_ = GlobalDBQueue.Shutdown(2 * time.Second)
		GlobalDBQueue = oldQueue
	})

	trace := newRelayRequestTrace(NewRequestEventService(), CodexPlatform)
	firstAttempt := trace.BeforeAttempt("primary")
	trace.RecordForwardError("primary", newUpstreamStatusError(nil, 503, "upstream status 503"), firstAttempt, 1, time.Second)
	trace.BeforeAttempt("backup")
	trace.Finish(http.StatusOK, false)

	var outcome string
	if err := db.QueryRow(`SELECT outcome FROM request_event_log WHERE request_id = ? AND event_type = ?`, trace.RequestID(), RequestEventCompleted).Scan(&outcome); err != nil {
		t.Fatalf("read fallback completion: %v", err)
	}
	if outcome != "success" {
		t.Fatalf("fallback completion outcome = %q, want success", outcome)
	}
}

func TestRelayRequestTraceLocalSummaryOutcomes(t *testing.T) {
	setupRenameTestEnv(t)
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("get test database: %v", err)
	}
	if err := ensureRequestEventTable(); err != nil {
		t.Fatalf("create request event table: %v", err)
	}
	oldQueue := GlobalDBQueue
	GlobalDBQueue = NewDBWriteQueue(db, 100, false)
	t.Cleanup(func() {
		_ = GlobalDBQueue.Shutdown(2 * time.Second)
		GlobalDBQueue = oldQueue
	})

	continuedTrace := newRelayRequestTrace(NewRequestEventService(), CodexPlatform)
	continuedTrace.RecordLocalSummary("primary", "model_mapping_error", "provider model mapping could not be applied")
	failedTrace := newRelayRequestTrace(NewRequestEventService(), CodexPlatform)
	failedTrace.RecordSummary("invalid_request", "request body must be valid JSON")

	for _, testCase := range []struct {
		name      string
		requestID string
		want      string
	}{
		{name: "provider skipped", requestID: continuedTrace.RequestID(), want: "continued"},
		{name: "terminal failure", requestID: failedTrace.RequestID(), want: "failed"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var outcome string
			if err := db.QueryRow(`SELECT outcome FROM request_event_log WHERE request_id = ? AND event_type = ?`, testCase.requestID, RequestEventError).Scan(&outcome); err != nil {
				t.Fatalf("read local summary outcome: %v", err)
			}
			if outcome != testCase.want {
				t.Fatalf("local summary outcome = %q, want %q", outcome, testCase.want)
			}
		})
	}
}

func TestRelayRequestTraceMarksClientAbort(t *testing.T) {
	setupRenameTestEnv(t)
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("get test database: %v", err)
	}
	if err := ensureRequestEventTable(); err != nil {
		t.Fatalf("create request event table: %v", err)
	}
	oldQueue := GlobalDBQueue
	GlobalDBQueue = NewDBWriteQueue(db, 100, false)
	t.Cleanup(func() {
		_ = GlobalDBQueue.Shutdown(2 * time.Second)
		GlobalDBQueue = oldQueue
	})

	trace := newRelayRequestTrace(NewRequestEventService(), CodexPlatform)
	attempt := trace.BeforeAttempt("primary")
	trace.RecordForwardError("primary", errClientAbort, attempt, 1, 10*time.Millisecond)
	trace.Finish(http.StatusOK, false)

	var outcome string
	if err := db.QueryRow(`SELECT outcome FROM request_event_log WHERE request_id = ? AND event_type = ?`, trace.RequestID(), RequestEventError).Scan(&outcome); err != nil {
		t.Fatalf("read client abort event: %v", err)
	}
	if outcome != "client_aborted" {
		t.Fatalf("error outcome = %q, want client_aborted", outcome)
	}
	if err := db.QueryRow(`SELECT outcome FROM request_event_log WHERE request_id = ? AND event_type = ? ORDER BY id DESC LIMIT 1`, trace.RequestID(), RequestEventCompleted).Scan(&outcome); err != nil {
		t.Fatalf("read client abort completion: %v", err)
	}
	if outcome != "client_aborted" {
		t.Fatalf("completion outcome = %q, want client_aborted", outcome)
	}
}

func TestRelayRequestTraceTerminalFailureOverridesCommittedSuccessStatus(t *testing.T) {
	setupRenameTestEnv(t)
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("get test database: %v", err)
	}
	if err := ensureRequestEventTable(); err != nil {
		t.Fatalf("create request event table: %v", err)
	}
	oldQueue := GlobalDBQueue
	GlobalDBQueue = NewDBWriteQueue(db, 100, false)
	t.Cleanup(func() {
		_ = GlobalDBQueue.Shutdown(2 * time.Second)
		GlobalDBQueue = oldQueue
	})

	trace := newRelayRequestTrace(NewRequestEventService(), CodexPlatform)
	attempt := trace.BeforeAttempt("primary")
	trace.RecordForwardError("primary", errUpstreamStreamAborted, attempt, 1, 20*time.Millisecond)
	trace.Finish(http.StatusOK, false)

	var outcome string
	if err := db.QueryRow(`SELECT outcome FROM request_event_log WHERE request_id = ? AND event_type = ?`, trace.RequestID(), RequestEventError).Scan(&outcome); err != nil {
		t.Fatalf("read terminal failure event: %v", err)
	}
	if outcome != "failed" {
		t.Fatalf("error outcome = %q, want failed", outcome)
	}
	if err := db.QueryRow(`SELECT outcome FROM request_event_log WHERE request_id = ? AND event_type = ?`, trace.RequestID(), RequestEventCompleted).Scan(&outcome); err != nil {
		t.Fatalf("read terminal failure completion: %v", err)
	}
	if outcome != "failed" {
		t.Fatalf("completion outcome = %q, want failed", outcome)
	}
}
