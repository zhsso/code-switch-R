package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const volumeMigrationID = "codex-plus-v1"

type volumeMigrationResult struct {
	Database string
	Backup   string
	Migrated bool
}

func migrateVolumeData() (volumeMigrationResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return volumeMigrationResult{}, fmt.Errorf("resolve data home: %w", err)
	}
	home = filepath.Clean(home)
	if home == "" || home == "." || !filepath.IsAbs(home) {
		return volumeMigrationResult{}, fmt.Errorf("resolve data home: invalid path %q", home)
	}
	return migrateVolumeDataAt(home)
}

func migrateVolumeDataAt(home string) (volumeMigrationResult, error) {
	targetDir := filepath.Join(home, ".code-switch")
	targetDB := filepath.Join(targetDir, "app.db")
	result := volumeMigrationResult{Database: targetDB}
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return result, fmt.Errorf("create data directory: %w", err)
	}

	sourceDB := firstExistingFile(
		targetDB,
		filepath.Join(home, "app.db"),
		filepath.Join(home, "code-switch", "app.db"),
		filepath.Join(home, ".config", "code-switch", "app.db"),
	)
	if sourceDB == "" {
		if err := initializeMigrationMarker(targetDB); err != nil {
			return result, fmt.Errorf("initialize data store: %w", err)
		}
		return result, nil
	}

	if err := migrateLegacyConfigFiles(filepath.Dir(sourceDB), targetDir); err != nil {
		return result, err
	}
	if applied, err := migrationApplied(sourceDB); err != nil {
		backup, backupErr := preserveUnreadableDatabase(sourceDB, targetDir)
		result.Backup = backup
		if backupErr != nil {
			return result, fmt.Errorf("inspect migration marker: %w (preserve unreadable database: %v)", err, backupErr)
		}
		return result, fmt.Errorf("inspect migration marker: %w (database preserved at %s)", err, backup)
	} else if applied && sourceDB == targetDB {
		return result, nil
	}

	backupDir := filepath.Join(targetDir, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return result, fmt.Errorf("create backup directory: %w", err)
	}
	backupDB := filepath.Join(backupDir, "app.db.before-"+volumeMigrationID+".sqlite")
	if err := ensureConsistentSQLiteBackup(sourceDB, backupDB); err != nil {
		return result, fmt.Errorf("backup legacy database: %w", err)
	}
	result.Backup = backupDB

	workingDB := targetDB
	replaceTarget := sourceDB != targetDB
	if replaceTarget {
		workingDB = targetDB + ".migrating"
		if err := copyFile(backupDB, workingDB, 0o600); err != nil {
			return result, fmt.Errorf("stage legacy database: %w", err)
		}
	}

	if err := migrateCodexRows(workingDB); err != nil {
		return result, fmt.Errorf("migrate legacy database: %w", err)
	}
	if err := checkSQLiteDatabase(workingDB); err != nil {
		return result, fmt.Errorf("verify migrated database: %w", err)
	}
	if replaceTarget {
		if err := os.Rename(workingDB, targetDB); err != nil {
			return result, fmt.Errorf("activate migrated database: %w", err)
		}
	}
	if err := os.Chmod(targetDB, 0o600); err != nil {
		return result, fmt.Errorf("secure migrated database: %w", err)
	}
	result.Migrated = true
	return result, nil
}

func preserveUnreadableDatabase(source, targetDir string) (string, error) {
	backupDir := filepath.Join(targetDir, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", err
	}
	backup := filepath.Join(backupDir, "app.db.unreadable-"+volumeMigrationID)
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		if err := copyFile(source, backup, 0o600); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		sidecar := source + suffix
		if _, err := os.Stat(sidecar); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return "", err
		}
		if err := copyFile(sidecar, backup+suffix, 0o600); err != nil {
			return "", err
		}
	}
	return backup, nil
}

func firstExistingFile(paths ...string) string {
	for _, path := range paths {
		info, err := os.Stat(path)
		if err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

func initializeMigrationMarker(path string) error {
	db, err := openMigrationDB(path)
	if err != nil {
		return err
	}
	defer db.Close()
	return applyMigrationTransaction(db, false)
}

func migrationApplied(path string) (bool, error) {
	db, err := openMigrationDB(path)
	if err != nil {
		return false, err
	}
	defer db.Close()
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'codeswitch_migrations'`).Scan(&exists); err != nil {
		return false, err
	}
	if exists == 0 {
		return false, nil
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM codeswitch_migrations WHERE id = ?`, volumeMigrationID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func ensureConsistentSQLiteBackup(source, backup string) error {
	if _, err := os.Stat(backup); err == nil {
		return checkSQLiteDatabase(backup)
	} else if !os.IsNotExist(err) {
		return err
	}

	db, err := openMigrationDB(source)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA wal_checkpoint(FULL)"); err != nil {
		return err
	}
	statement := "VACUUM INTO '" + strings.ReplaceAll(backup, "'", "''") + "'"
	if _, err := db.Exec(statement); err != nil {
		return err
	}
	if err := os.Chmod(backup, 0o600); err != nil {
		return err
	}
	return checkSQLiteDatabase(backup)
}

func migrateCodexRows(path string) error {
	db, err := openMigrationDB(path)
	if err != nil {
		return err
	}
	defer db.Close()
	return applyMigrationTransaction(db, true)
}

func applyMigrationTransaction(db *sql.DB, purgeLegacy bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS codeswitch_migrations (
		id TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	if purgeLegacy {
		for _, table := range []string{"request_log", "provider_blacklist", "health_check_history", "provider_alias"} {
			exists, err := tableHasColumn(tx, table, "platform")
			if err != nil {
				return err
			}
			if exists {
				query := fmt.Sprintf(`DELETE FROM %s
					WHERE LOWER(TRIM(COALESCE(platform, ''))) <> ?`, table)
				if _, err := tx.Exec(query, "codex"); err != nil {
					return err
				}
			}
		}
		captureTable, err := tableExists(tx, "capture_session")
		if err != nil {
			return err
		}
		requestCaptureColumn, err := tableHasColumn(tx, "request_log", "capture_session_id")
		if err != nil {
			return err
		}
		if captureTable && requestCaptureColumn {
			if _, err := tx.Exec(`DELETE FROM capture_session
				WHERE NOT EXISTS (
					SELECT 1 FROM request_log
					WHERE request_log.capture_session_id = capture_session.id
					  AND request_log.capture_session_id <> 0
				)`); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO codeswitch_migrations (id, applied_at) VALUES (?, ?)`,
		volumeMigrationID, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

type migrationQueryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

func tableExists(db migrationQueryer, table string) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count)
	return count > 0, err
}

func tableHasColumn(db migrationQueryer, table, column string) (bool, error) {
	exists, err := tableExists(db, table)
	if err != nil || !exists {
		return false, err
	}
	var count int
	query := fmt.Sprintf(`SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name = ?`,
		strings.ReplaceAll(table, "'", "''"))
	err = db.QueryRow(query, column).Scan(&count)
	return count > 0, err
}

func checkSQLiteDatabase(path string) error {
	db, err := openMigrationDB(path)
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("quick_check returned %q", result)
	}
	return nil
}

func openMigrationDB(path string) (*sql.DB, error) {
	escaped := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(filepath.ToSlash(path))
	db, err := sql.Open("sqlite", "file:"+escaped+"?_pragma=busy_timeout(30000)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func copyFile(source, target string, mode os.FileMode) (resultErr error) {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, in.Close())
	}()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	return errors.Join(copyErr, syncErr, closeErr)
}

func migrateLegacyConfigFiles(sourceDir, targetDir string) error {
	if sourceDir == targetDir {
		return nil
	}
	for _, name := range []string{"codex.json", "app.json"} {
		source := filepath.Join(sourceDir, name)
		target := filepath.Join(targetDir, name)
		if _, err := os.Stat(target); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect %s: %w", name, err)
		}
		if _, err := os.Stat(source); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect legacy %s: %w", name, err)
		}
		staged := target + ".migrating"
		if err := copyFile(source, staged, 0o600); err != nil {
			return fmt.Errorf("stage legacy %s: %w", name, err)
		}
		if err := os.Rename(staged, target); err != nil {
			return fmt.Errorf("activate legacy %s: %w", name, err)
		}
	}
	return nil
}
