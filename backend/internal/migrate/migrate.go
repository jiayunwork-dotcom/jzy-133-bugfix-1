package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

// Runner 负责把嵌入 FS 中的 SQL 迁移按文件名顺序应用一次。
type Runner struct {
	db  *sql.DB
	fs  embed.FS
	dir string
}

// New 创建迁移执行器。dir 为 FS 中的迁移目录名（如 "migrations"）。
func New(db *sql.DB, fs embed.FS, dir ...string) *Runner {
	d := "migrations"
	if len(dir) > 0 {
		d = dir[0]
	}
	return &Runner{db: db, fs: fs, dir: d}
}

// Up 执行所有未应用的迁移，每个迁移在独立事务中提交。
func (r *Runner) Up(ctx context.Context) error {
	entries, err := r.fs.ReadDir(r.dir)
	if err != nil {
		return fmt.Errorf("读取迁移目录: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	if _, err := r.db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("建迁移登记表: %w", err)
	}

	for _, name := range names {
		var ok bool
		if err := r.db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name=$1)`, name).Scan(&ok); err != nil {
			return err
		}
		if ok {
			continue
		}
		content, err := r.fs.ReadFile(r.dir + "/" + name)
		if err != nil {
			return err
		}
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("迁移 %s 执行失败: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(name) VALUES($1)`, name); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
