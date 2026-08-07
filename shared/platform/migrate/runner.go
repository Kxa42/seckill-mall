// Package migrate 提供按文件名顺序执行的版本化 SQL migration。
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Run 执行尚未记录在 schema_migrations 中的 SQL 文件。
func Run(ctx context.Context, db *sql.DB, source fs.FS) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		return fmt.Errorf("创建 schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(source, ".")
	if err != nil {
		return fmt.Errorf("读取 migration 目录: %w", err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".sql" {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)

	for _, name := range files {
		applied, err := isApplied(ctx, db, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := fs.ReadFile(source, name)
		if err != nil {
			return fmt.Errorf("读取 migration %s: %w", name, err)
		}
		if err := apply(ctx, db, name, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func isApplied(ctx context.Context, db *sql.DB, version string) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&count); err != nil {
		return false, fmt.Errorf("查询 migration %s: %w", version, err)
	}
	return count > 0, nil
}

func apply(ctx context.Context, db *sql.DB, version, body string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始 migration %s: %w", version, err)
	}
	defer tx.Rollback()

	for _, statement := range splitStatements(body) {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("执行 migration %s: %w", version, err)
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES (?)", version); err != nil {
		return fmt.Errorf("记录 migration %s: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交 migration %s: %w", version, err)
	}
	return nil
}

func splitStatements(body string) []string {
	var statements []string
	var builder strings.Builder
	var quote rune
	escaped := false
	inLineComment := false
	inBlockComment := false
	runes := []rune(body)

	flush := func() {
		statement := strings.TrimSpace(builder.String())
		builder.Reset()
		if statement != "" {
			statements = append(statements, statement)
		}
	}

	for index := 0; index < len(runes); index++ {
		current := runes[index]
		next := rune(0)
		if index+1 < len(runes) {
			next = runes[index+1]
		}

		if inLineComment {
			if current == '\n' {
				inLineComment = false
				builder.WriteRune(current)
			}
			continue
		}
		if inBlockComment {
			if current == '*' && next == '/' {
				inBlockComment = false
				index++
			}
			continue
		}
		if quote == 0 {
			if current == '-' && next == '-' {
				inLineComment = true
				index++
				continue
			}
			if current == '#' {
				inLineComment = true
				continue
			}
			if current == '/' && next == '*' {
				inBlockComment = true
				index++
				continue
			}
			if current == '\'' || current == '"' || current == '`' {
				quote = current
				builder.WriteRune(current)
				continue
			}
			if current == ';' {
				flush()
				continue
			}
			builder.WriteRune(current)
			continue
		}

		builder.WriteRune(current)
		if escaped {
			escaped = false
			continue
		}
		if current == '\\' && quote != '`' {
			escaped = true
			continue
		}
		if current == quote {
			if next == quote {
				builder.WriteRune(next)
				index++
				continue
			}
			quote = 0
		}
	}
	flush()
	return statements
}
