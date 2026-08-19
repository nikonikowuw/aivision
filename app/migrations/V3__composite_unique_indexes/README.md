# V3: 修复软删除的唯一索引冲突

由于 `gorm.DeletedAt` 采用 `NULL` 作为未删除的标记，当尝试重复创建曾经被软删除的数据时，由于单列唯一索引会触发唯一键冲突。

我们修改了 `Role` 的 `Code` 和 `User` 的 `Username` 上的唯一索引为结合 `DeletedAt` 的复合唯一索引：
- MySQL 默认允许多个包含 `NULL` 的行，因此简单的复合索引即可处理。
- Postgres 针对包含 `NULL` 的唯一索引的处理与 MySQL 类似，但我们在 PG 中使用部分索引 `WHERE deleted_at IS NULL` 进一步提高语义准确性（在 AutoMigrate 时可能不使用这个语义，但手动创建时推荐使用）。
