# 人员管理 MVP

## 目标
为 AI 视频分析边缘平台提供可用的人员基础信息管理和业务系统同步能力。管理员可以在 Web 页面维护人员，外部业务系统可以通过受 IP 白名单保护的单人员 REST API 逐条同步人员数据。

## 背景与范围边界
当前仓库只有预留的人员 gRPC 协议，没有人员管理的 Go 或 Vue 实现。本任务只实现人员基础文本信息闭环，不改变预留的 C++ 人员 gRPC 协议。

## 功能范围
- 新增人员、分页查询、编辑姓名、单个删除和页面批量删除。
- 页面 API 使用现有 JWT 认证和权限系统，路径为 `/api/person/...`。
- 外部同步 API 使用 `/api/v1/open/person/...` 前缀，只提供单人员幂等新增/更新和删除；业务系统自行逐条调用，不提供开放批量接口。
- 外部同步 API 使用固定启动 IP 白名单保护，白名单默认为空，支持 IPv4/IPv6 单地址和 CIDR。
- 新增资源管理菜单、人员页面权限、操作权限和三语文案。

## 人员模型
- 数据库内部主键为 `id`，只供数据库内部使用，不出现在任何 API、前端模型、日志业务载荷或 URL 中。
- 对外标识为 `person_id`，创建后不可修改，大小写敏感并按原值保存。
- 手工填写的 `personId` 去除首尾空格后必须为 1～64 个 ASCII 字符；首字符为字母或数字，后续只允许字母、数字、`_`、`-`、`.`、`:`。
- 页面新增未填写 `personId` 时生成去划线 UUIDv4；开放同步 API 必须从路径提供非空 `personId`。
- `name` 允许重复，去除首尾空格后不能为空，最大 64 个 Unicode 字符，并拒绝控制字符。
- `createdAt`、`updatedAt`、`deletedAt` 使用现有 `BaseModel`；`person_id` 在包含软删除记录的所有人员中保持唯一。

## 删除、恢复与并发
- 删除使用软删除；默认列表只返回未软删除人员。
- 页面删除对不存在或已删除人员返回 `CodeNotFound`；开放删除对不存在或已删除人员返回成功。
- 页面新增填写已软删除的 `personId` 时恢复原记录、更新姓名并保留内部 `id`。
- 开放 `PUT` 对不存在、有效和已软删除标识均执行幂等 upsert；已删除记录恢复，不创建新记录。
- 页面新增填写已存在的有效 `personId` 时返回 `CodePersonIDTaken = 1018`，不覆盖原姓名。
- 页面批量删除每次接受 1～100 个 `personIds`，仅处理当前页选择；不存在或已删除项忽略，其他项继续删除，整体成功并返回 `data: null`。
- 不引入版本号、`If-Match`、乐观锁或应用层显式锁；并发写入按普通事务提交顺序处理，最后成功提交的请求覆盖前一个请求，数据库唯一约束负责防止重复记录。

## HTTP API
### 页面 API
- `GET /api/person/page`：支持 `page`、`pageSize`、`personId` 精确筛选和 `name` 模糊筛选；默认排序为 `created_at DESC, id DESC`。
- `POST /api/person`：`personId` 可选，返回公开人员 DTO。
- `PUT /api/person/:personId`：只更新 `name`，不支持更新 `personId`；请求体中的 `personId` 不作为更新字段。
- `DELETE /api/person/:personId`：软删除单个人员。
- `DELETE /api/person/batch`：请求体为 `{"personIds":[...]}`，最多 100 个，仅页面认证 API 使用。

### 外部同步 API
- `PUT /api/v1/open/person/:personId`：请求体只包含必填 `name`，幂等 upsert；成功返回公开人员 DTO。
- `DELETE /api/v1/open/person/:personId`：幂等软删除；成功返回 `data: null`。
- 不提供开放批量接口、开放查询接口或 JWT 认证；调用必须通过 IP 白名单。
- 所有请求和响应使用统一 `{code,data,message}`；JSON 字段使用 camelCase（`personId`、`personIds`、`createdAt`、`updatedAt`），不返回内部 `id`。
- IP 不在白名单时返回 HTTP 403 和 `CodeForbidden`；参数错误使用 HTTP 400 和 `CodeInvalidParam`；业务错误沿用现有统一错误处理中间件。

## IP 白名单配置
- 配置文件：`app/configs/config.yaml`。
- 配置键：
  ```yaml
  open:
    person_sync_allowed_ips: []
  ```
- 环境变量 `APP_OPEN_PERSON_SYNC_ALLOWED_IPS` 在启动时覆盖配置文件值，多个地址使用逗号分隔。
- 应用启动时校验全部配置项；任一 IP/CIDR 非法则启动失败，不能静默忽略。
- 默认空列表表示拒绝所有来源；只匹配 TCP 真实连接源地址，不读取或信任 `X-Forwarded-For`。
- 本期不提供白名单管理页面、管理 API 或运行时热更新；修改配置后重启服务生效。

## 页面与权限
- 页面路径 `/resource/person`，路由名 `ResourcePerson`，页面权限 `resource:person`，图标 `ant-design:idcard-outlined`。
- 页面保持缓存，重新激活时刷新数据并保留筛选、分页和滚动位置。
- 新增、编辑、删除按钮权限分别为 `resource:person:add`、`resource:person:edit`、`resource:person:delete`。
- 列表展示多选框、`personId`、姓名、创建时间、更新时间和操作列；新增和编辑使用弹窗，不提供独立详情页、回收站或恢复按钮。
- 新增表单显示可选 `personId` 和必填姓名；编辑显示只读 `personId`，只提交姓名。
- 批量删除需要二次确认并显示选中数量；成功后清空选择并刷新列表。
- 所有页面文案、路由标题和操作日志动作使用 `zh-CN`、`en-US`、`zh-TW` 三语 i18n。

## 非本期范围
- 人脸照片上传、质量检测、C++ 特征提取、特征持久化、索引构建和人员 gRPC `PersonService.SyncPersons` 实现。
- Webhook 下发、识别记录、回收站、单人员详情页、开放批量接口和开放查询接口。
- 白名单运行时管理、动态更新和白名单变更审计。
- 手机号、工号、部门、备注、状态等额外人员字段。

## 验收标准
- [ ] 新增版本化 PostgreSQL migration，创建人员表、唯一 `person_id` 约束和必要索引；down migration 可逆；SQLite `AutoMigrate` 同步包含 Person。
- [ ] GORM 模型显式声明表名和列名，使用 `BaseModel`，默认查询排除软删除人员。
- [ ] 页面 API 完成分页、精确/模糊筛选、增改删和 1～100 项批量软删除；内部 `id` 不出现在任何响应。
- [ ] `personId` 自动生成、格式校验、大小写敏感、创建后不可变、重复冲突和软删除恢复行为符合本文件。
- [ ] 开放 `PUT`/`DELETE` 单人员接口符合 upsert、删除幂等和公开 DTO 契约，不提供开放批量/查询接口。
- [ ] 固定白名单支持 IPv4、IPv6、CIDR、环境变量覆盖、默认拒绝、真实连接地址匹配和非法配置启动失败；不信任 `X-Forwarded-For`。
- [ ] 页面菜单、动态路由、页面权限、按钮权限、缓存刷新、多选批量删除、弹窗表单和三语文案可用。
- [ ] 写操作继续进入统一操作日志；handler 不自定义错误响应，不泄露内部错误或内部主键。
- [ ] 后端单测/API/路由测试覆盖正常、参数非法、重复标识、恢复、幂等删除、批量边界、白名单 IPv4/IPv6/CIDR 和不信任代理头。
- [ ] `cd app && make test`、`make vet`、`make wire`/生成代码检查通过；`cd ui && pnpm check` 通过。

## 已确认决策
- 自动标识：去划线 UUIDv4。
- 页面接口前缀：`/api/person`；开放接口前缀：`/api/v1/open/person`。
- 只提供开放单人员 REST 操作；业务系统自行逐条同步。
- 软删除并允许同一 `personId` 下发时恢复原记录。
- 页面支持当前页最多 100 条批量删除，成功返回 `data: null`。
- 固定启动白名单，配置在 `open.person_sync_allowed_ips`，环境变量可覆盖，非法配置启动失败。
- `personId` 大小写敏感；姓名允许重复；不提供详情页和回收站。

## 阻塞性未决问题
无。模型来源、协议实现和测试环境等属于实现阶段的技术验证，不改变本期产品行为。
