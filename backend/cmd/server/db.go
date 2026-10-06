package main

import (
	"context"
	"database/sql"
	"time"

	"github.com/lib/pq"
)

// pqConnString 兼容两种 DSN：postgres:// URL 与 key=value 串。
func pqConnString(dsn string) (string, error) {
	if len(dsn) > 0 && dsn[0] == 'p' {
		return pq.ParseURL(dsn)
	}
	return dsn, nil
}

// waitDB 等待数据库可连通（compose 下 postgres 首次启动较慢）。
func waitDB(db *sql.DB) error {
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		lastErr = db.PingContext(ctx)
		cancel()
		if lastErr == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return lastErr
}
