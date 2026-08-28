# 检测规则编辑器

## Goal

在实时视频画面上提供 ROI（检测区域）、Mask（屏蔽区域）与分界线的可视化绘制能力，输出符合 PRD §7.6.1 的归一化坐标，供算法实例配置使用。

父任务：`08-28-task-configuration`（后端 `rules_json` 存储与几何校验已在父任务完成，本任务只做绘制界面）。

## Background / 已确认事实

- PRD §7.6.1：原点位于**视频有效画面**左上角；横纵坐标统一为 `[0,1]` 归一化；平台负责预览坐标与归一化坐标转换；多边形至少 3 个顶点，不允许越界或自交。
- `engine.proto` 已定义契约，本任务不改动：
  - `DetectionRuleRole`：`ROI = 1`（只在此区域内检测）、`MASK = 2`（此区域内目标忽略）、`LINE = 3`（跨线触发）
  - `DetectionLineDirection`：`BOTH = 0`、`A_TO_B = 1`、`B_TO_A = 2`，仅 LINE 有效
  - `DetectionPoint`：`float x`、`float y`
  - 区域 ≥3 顶点首尾相连；线 ≥2 顶点折线
- `manifest-schema.md:173`、`188`：检测规则是**任务级配置**，由 Engine 校验后经 `av_algo_instance_args.rules` 下发，判定语义（锚点/跨线）归算法；**不进入算法 `config.schema.json`**。
- `ui/apps/web-antd/src/components/video/VideoPlayer.vue`：标准 `<video>` + flv.js（`attachMediaElement`），可作为绘制底图载体。
- 父任务已实现服务端几何校验 `rulegeom.ValidateRules`，前端校验是体验优化，**不是安全边界**。

### 底图方案决策

采用**实时视频流 + 透明 canvas 叠加层**，不使用冻结帧：

- 产品上：用户能看到人/车实际行进路线，才能准确判断绊线位置与 ROI 范围；静态图是盲画。
- 实现上：无需 `drawImage` 捕获帧，少一层逻辑。
- 避坑：flv.js 拉流若跨域，`drawImage` 会污染 canvas（tainted），后续 `toDataURL` / `getImageData` 直接抛异常；叠加层完全不接触视频像素，无此风险。

## Requirements

### R1 坐标换算

`<video>` 在 `object-fit: contain` 下会产生黑边，归一化必须基于**实际画面区域**而非元素尺寸：

```
videoAR = videoWidth / videoHeight
elemAR  = clientWidth / clientHeight

若 videoAR > elemAR：画面满宽，上下黑边
    renderW = clientWidth
    renderH = clientWidth / videoAR
    offsetX = 0
    offsetY = (clientHeight - renderH) / 2
否则：画面满高，左右黑边
    renderH = clientHeight
    renderW = clientHeight * videoAR
    offsetX = (clientWidth - renderW) / 2
    offsetY = 0

归一化 = ((clickX - offsetX) / renderW, (clickY - offsetY) / renderH)
反算   = (nx * renderW + offsetX, ny * renderH + offsetY)
```

- 窗口尺寸变化、视频分辨率变化时必须重算并重绘。
- 落在黑边上的点击应被忽略或钳制到 `[0,1]`，不得产生越界坐标。

### R2 绘制交互

- 支持三种规则角色：ROI、Mask、分界线；一个实例可配置多条规则。
- 区域：逐点点击添加顶点，双击或点击起点闭合；闭合前实时预览连线。
- 分界线：逐点点击添加折线顶点，双击结束。
- 顶点可拖拽调整；支持删除单个顶点（区域降至 2 点或线降至 1 点时阻止删除）。
- 支持删除整条规则、清空全部规则。
- 不同角色用不同颜色与填充样式区分，图例明确。
- 分界线需可视化方向（箭头），并可切换 `BOTH` / `A_TO_B` / `B_TO_A`。

### R3 前端校验

在提交前给出即时反馈（服务端仍会复校）：

- 顶点数不足（区域 <3、线 <2）
- 坐标越界
- 多边形自交——绘制过程中实时提示，不等到提交

### R4 集成

- 嵌入父任务的 `InstanceFormModal.vue`，与 `analysis_fps`、`params_json` 一起**整份提交**（PRD §7.6 原子热更新）。
- 编辑既有实例时，从 `rules_json` 还原绘制状态。
- 视频源复用摄像头预览通道，退出编辑器时释放播放会话，不得残留连接。

## Acceptance Criteria

- [ ] `pnpm check` 与 `pnpm test:unit` 通过。
- [ ] 坐标换算函数有独立单测，覆盖：视频比容器宽（上下黑边）、比容器窄（左右黑边）、比例相同（无黑边）三种情形，以及正反变换互逆性。
- [ ] 在 16:9 视频 + 4:3 容器下，点击画面正中得到的归一化坐标为 `(0.5, 0.5) ± 0.01`。
- [ ] 绘制 ROI/Mask/分界线三种规则并保存，刷新页面后能正确还原形状与位置。
- [ ] 多边形自交时在绘制过程中即给出提示，且无法提交。
- [ ] 顶点数不足时无法闭合/提交。
- [ ] 窗口缩放后已绘制图形位置随画面正确跟随，归一化坐标不变。
- [ ] 规则与 FPS、参数一起整份提交，任一校验失败则整份拒绝，旧配置保持不变。
- [ ] 关闭编辑器后播放会话已释放（无残留 flv.js 连接）。

## Non-Goals

- 不实现算法侧的规则判定语义（锚点判定、跨线方向判定归算法包，见 `manifest-schema.md:188`）。
- 不实现规则模板库、规则复制到其他摄像头。
- 不在预览画面上叠加检测框、事件类型或识别结果（PRD §7.3 明确禁止）。
- 不修改后端 `rules_json` 存储结构与几何校验（父任务已完成）。
- 不改动 `engine.proto` 的 `DetectionRule` / `DetectionPoint` 契约。
