package service

import "context"

// RevisionBumper 期望状态版本递增端口。供其它模块（algorithmService 等）在改变
// DesiredState 内容后触发 revision 递增（design §3.2 / PRD D11），避免
// algorithmService 直接依赖 TaskRepository。
//
// 实现方必须保证 BumpRevision 与各自的业务写入在同事务内提交：否则会出现
// 「配置已改但 revision 未增（Engine 永不感知）」或「revision 已增但配置未落」。
type RevisionBumper interface {
	BumpRevision(ctx context.Context) error
}
