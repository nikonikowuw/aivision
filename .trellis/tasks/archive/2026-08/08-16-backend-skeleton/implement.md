# 实施计划：Go 工程骨架（backend-skeleton）

> 逐条实施，每步跑完验证点再进入下一步。全部完成 → 汇报并**等用户手动 commit**（父任务约定）。

## 执行顺序与验证链路

1. **初始化 `app/` 工程**：`go mod init niko-vue-admin/app` → 引入依赖（gin、gorm、gorm mysql driver、viper、zap、x/crypto、wire、sqlite 测试驱动）
   → 验证：`go build ./...` 通过
2. **`internal/pkg/` 四件套**：config（viper+APP_* 覆盖）/ logger（zap）/ response（{code,data,message}）/ errno（父 design.md §6 错误码表全量定义）
   → 验证：`go test ./internal/pkg/...` 通过（config 覆盖规则、response JSON 结构）
3. **`internal/model/` 8 表**：BaseModel + 8 个模型 + TableName/tag/索引 + `BuildMenuTree` 纯函数
   → 验证：`go vet ./...` 无错误；sqlite 内存库 AutoMigrate 冒烟测试通过
4. **seed**：`internal/model/seed.go`，按 design.md §5 权限码契约播种（幂等判定 admin 存在）
   → 验证：sqlite 冒烟测试断言 admin/super/菜单树/按钮码数量
5. **wire 装配**：`wire.go` ProviderSet → `wire_gen.go`；`router.go` gin 空壳
   → 验证：`make wire` 再生成无 diff；`go build ./...`
6. **Makefile + config.yaml 收尾**
   → 验证：`make build`、`make test`、`make vet` 全绿
7. **AC 全量核验**（真 MySQL）
   → docker 起 mysql:8（映射 3306）→ `make dev` 启动无 panic、日志显示 AutoMigrate+seed → `docker exec` 查 8 表存在、admin 密码为 bcrypt 哈希、菜单树完整（含按钮级）
   → 验证点：skeleton prd AC 全部打勾

## 回滚点

- 每步完成即自然 checkpoint；代码未 commit 前一切在 `app/` 下，删除 `app/` 即回滚（docker 数据卷 `docker rm -f` 释放）。
- 依赖版本锁定在 go.mod/go.sum，可复现。

## 交付边界

- 不注册任何业务路由（router 空壳）；不写 repository/service/api/middleware。
- 汇报内容：AC 逐项结果 + docker mysql 验证命令输出摘要。
