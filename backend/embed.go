package spc

import "embed"

// MigrationsFS 嵌入 migrations 目录下的全部 SQL。
//
//go:embed all:migrations
var MigrationsFS embed.FS
