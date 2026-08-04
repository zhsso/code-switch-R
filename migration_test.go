package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func createLegacyMigrationDB(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := openMigrationDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	statements := []string{
		`CREATE TABLE request_log (id INTEGER PRIMARY KEY, platform TEXT, capture_session_id INTEGER)`,
		`CREATE TABLE provider_blacklist (id INTEGER PRIMARY KEY, platform TEXT)`,
		`CREATE TABLE health_check_history (id INTEGER PRIMARY KEY, platform TEXT)`,
		`CREATE TABLE provider_alias (id INTEGER PRIMARY KEY, platform TEXT)`,
		`CREATE TABLE capture_session (id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT)`,
		`INSERT INTO request_log VALUES (1, 'codex', 10), (2, 'claude', 20)`,
		`INSERT INTO provider_blacklist VALUES (1, 'codex'), (2, 'gemini')`,
		`INSERT INTO health_check_history VALUES (1, 'codex'), (2, 'claude')`,
		`INSERT INTO provider_alias VALUES (1, 'codex'), (2, 'gemini')`,
		`INSERT INTO capture_session VALUES (10, 'codex capture'), (20, 'legacy capture')`,
		`INSERT INTO app_settings VALUES ('history_retention_days', '90')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("execute %q: %v", statement, err)
		}
	}
}

func migrationCount(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return count
}

func assertCodexMigration(t *testing.T, path string) {
	t.Helper()
	db, err := openMigrationDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"request_log", "provider_blacklist", "health_check_history", "provider_alias"} {
		if got := migrationCount(t, db, "SELECT COUNT(*) FROM "+table); got != 1 {
			t.Fatalf("%s rows = %d, want 1 Codex row", table, got)
		}
		if got := migrationCount(t, db, "SELECT COUNT(*) FROM "+table+" WHERE platform = 'codex'"); got != 1 {
			t.Fatalf("%s Codex rows = %d, want 1", table, got)
		}
	}
	if got := migrationCount(t, db, "SELECT COUNT(*) FROM capture_session"); got != 1 {
		t.Fatalf("capture_session rows = %d, want 1", got)
	}
	if got := migrationCount(t, db, "SELECT COUNT(*) FROM app_settings WHERE key = 'history_retention_days'"); got != 1 {
		t.Fatalf("application settings were not retained")
	}
	if got := migrationCount(t, db, "SELECT COUNT(*) FROM codeswitch_migrations WHERE id = ?", volumeMigrationID); got != 1 {
		t.Fatalf("migration marker rows = %d, want 1", got)
	}
}

func TestMigrateVolumeDataPurgesNonCodexRowsAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".code-switch", "app.db")
	createLegacyMigrationDB(t, target)

	result, err := migrateVolumeDataAt(home)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Migrated || result.Backup == "" {
		t.Fatalf("unexpected migration result: %#v", result)
	}
	assertCodexMigration(t, target)

	backup, err := openMigrationDB(result.Backup)
	if err != nil {
		t.Fatal(err)
	}
	if got := migrationCount(t, backup, "SELECT COUNT(*) FROM request_log"); got != 2 {
		backup.Close()
		t.Fatalf("backup request rows = %d, want untouched 2", got)
	}
	backup.Close()

	second, err := migrateVolumeDataAt(home)
	if err != nil {
		t.Fatal(err)
	}
	if second.Migrated {
		t.Fatalf("second migration should be a no-op: %#v", second)
	}
	assertCodexMigration(t, target)
}

func TestMigrateVolumeDataImportsLegacyVolumeRoot(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(home, "app.db")
	createLegacyMigrationDB(t, source)
	if err := os.WriteFile(filepath.Join(home, "codex.json"), []byte(`[{"name":"kept"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "app.json"), []byte(`{"show_heatmap":false}`), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := migrateVolumeDataAt(home)
	if err != nil {
		t.Fatal(err)
	}
	assertCodexMigration(t, result.Database)

	sourceDB, err := openMigrationDB(source)
	if err != nil {
		t.Fatal(err)
	}
	if got := migrationCount(t, sourceDB, "SELECT COUNT(*) FROM request_log"); got != 2 {
		sourceDB.Close()
		t.Fatalf("legacy source was modified: %d rows", got)
	}
	sourceDB.Close()
	for _, name := range []string{"codex.json", "app.json"} {
		want, _ := os.ReadFile(filepath.Join(home, name))
		got, err := os.ReadFile(filepath.Join(home, ".code-switch", name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("migrated %s = %q, err=%v; want %q", name, got, err, want)
		}
	}
}

func TestMigrateVolumeDataAbortsAndPreservesUnreadableDatabase(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(home, "app.db")
	original := []byte("not-a-sqlite-database")
	if err := os.WriteFile(source, original, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := migrateVolumeDataAt(home)
	if err == nil {
		t.Fatal("expected unreadable database migration to fail")
	}
	got, readErr := os.ReadFile(source)
	if readErr != nil || !bytes.Equal(got, original) {
		t.Fatalf("source changed after failed migration: %q, err=%v", got, readErr)
	}
	backup, readErr := os.ReadFile(result.Backup)
	if readErr != nil || !bytes.Equal(backup, original) {
		t.Fatalf("failure backup = %q, err=%v; want original bytes", backup, readErr)
	}
	if _, statErr := os.Stat(result.Database); !os.IsNotExist(statErr) {
		t.Fatalf("target database should not be activated on failure: %v", statErr)
	}
}
