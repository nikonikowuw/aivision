package migrations

import "embed"

// FS 嵌入所有 PostgreSQL 版本化迁移 SQL。
//
//go:embed *.sql
var FS embed.FS
