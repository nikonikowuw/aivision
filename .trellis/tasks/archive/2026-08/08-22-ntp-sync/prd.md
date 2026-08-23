# 实现对时服务

## Goal

为 AI 视频分析边缘平台提供 NTP 对时配置与同步状态监控能力，让管理员通过 Web 界面配置 NTP 服务器、查看同步状态，无需 SSH 登录设备。确保业务事件时间准确可靠，同步异常时事件带 `time_synced=false` 标记。

## Background

- 产品 PRD（`docs/prd/ai-video-analytics-edge-platform-v1.0.md`）明确要求：支持 NTP 服务器配置和同步状态展示；系统时间未同步时继续推理但记录和 Webhook 带 `time_synced=false`。
- C++ 引擎设计已预留 `wall_time_ns` 和 `time_synced` 字段。
- 网络配置任务（`08-22-network-configuration`）已明确排除 NTP，本任务独立实现。
- 当前代码库无任何 NTP/时间同步相关实现。

## Requirements

### 后端（Go）

- R1: 读取当前 NTP 同步状态（同步源、是否已同步、最后同步时间、时钟偏移量）
- R2: 读取当前配置的 NTP 服务器列表
- R3: 配置 NTP 服务器列表（增删改）
- R4: 支持两种对时模式切换：NTP 自动对时 / 手动设置时间
- R5: NTP 模式：触发立即同步
- R6: 手动模式：管理员填写日期时间直接设置系统时钟，同时禁用 NTP 服务避免被覆盖
- R7: 提供 `time_synced` 状态查询接口，供 C++ 引擎和 Webhook 使用
- R8: RBAC 权限控制（读/写分离）
- R9: 写操作记录操作日志
- R10: 使用测试替身，不直接操作测试环境系统时钟

### 前端（Vue）

- R11: 运维管理下新增"时间管理"页面
- R12: 对时模式切换（NTP 自动 / 手动）
- R13: NTP 模式：展示同步状态（源、状态、偏移、最后同步时间）+ 服务器列表管理 + 手动触发同步按钮
- R14: 手动模式：日期时间输入 + 应用按钮
- R15: 三语国际化（zh-CN、en-US、zh-TW）

## Technical Constraints

- 首版目标系统：Linux（RK3576 部署）+ macOS（开发环境），两端均支持真实操作
- NTP 后端通过统一接口适配多种工具（见 D2）
- Go 服务以 root 运行，直接执行系统命令（见 D3）

## Out of Scope

- 摄像头或其他远程设备的批量校时
- 多设备集中下发时间配置
- 设备时区配置（保留现有前端用户级显示时区）
- PTP（精密时间协议）支持

## Acceptance Criteria

- [ ] 管理员可在 Web 界面查看 NTP 同步状态
- [ ] 管理员可配置 NTP 服务器列表
- [ ] 管理员可触发手动同步（NTP 模式）
- [ ] 管理员可手动填写日期时间设置系统时钟（手动模式）
- [ ] 切换到手动模式时 NTP 服务被禁用，切换到 NTP 模式时 NTP 服务被启用
- [ ] 非授权用户无法修改 NTP 配置
- [ ] 写操作（配置变更、手动同步）记录到操作日志
- [ ] `time_synced` 接口返回正确的同步状态
- [ ] 单元测试使用 mock，不操作真实系统时钟

## Decisions

- D1: 首版同时支持 Linux 和 macOS 真实操作，通过平台适配层抽象差异
- D2: NTP 后端不绑定具体工具，定义统一接口（NTPManager interface），通过适配器支持 chrony、systemd-timesyncd、macOS sntp 等，运行时按平台/可用性自动选择或配置指定
- D3: Go 服务以 root 运行，无需额外提权机制
- D4: 支持 NTP 自动对时和手动设置时间两种模式，互斥切换；手动模式下禁用 NTP 服务防止时间被覆盖
