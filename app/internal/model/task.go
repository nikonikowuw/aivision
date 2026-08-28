package model

// 任务配置模块模型（Phase 1 仅定义检测规则契约结构；AnalysisTask /
// AlgorithmInstance 表模型随 Phase 2 数据迁移一并补充到本文件）。

// DetectionRuleRole 检测规则角色。
// 数值与 proto DetectionRuleRole（engine/proto/aivision/v1/engine.proto）及
// C ABI av_rule_role（sdk/include/aivision/algo.h）保持一致。
type DetectionRuleRole int32

const (
	DetectionRuleRoleUnspecified DetectionRuleRole = 0 // 未指定（非法）
	DetectionRuleRoleROI         DetectionRuleRole = 1 // 布防/检测区域：只在此区域内检测
	DetectionRuleRoleMask        DetectionRuleRole = 2 // 屏蔽/遮罩区域：此区域内目标忽略
	DetectionRuleRoleLine        DetectionRuleRole = 3 // 分界线：目标跨线触发
)

// DetectionLineDirection 分界线跨越方向，仅 LINE 规则有效。
// 数值与 proto DetectionLineDirection 及 C ABI av_line_dir 保持一致。
type DetectionLineDirection int32

const (
	DetectionLineDirectionBoth DetectionLineDirection = 0 // 双向跨越
	DetectionLineDirectionAToB DetectionLineDirection = 1 // 沿折线方向跨越
	DetectionLineDirectionBToA DetectionLineDirection = 2 // 逆折线方向跨越
)

// DetectionPoint 归一化检测点。
// 坐标原点为视频有效画面左上角，x/y 均归一化到 [0,1]（见 prd §7.6.1）。
type DetectionPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// DetectionRule 任务级检测规则（ROI / Mask / 分界线）。
// 持久化形态：algorithm_instances.rules_json 中存 []DetectionRule 的 JSON。
// 契约（design §6 / R6）：
//   - ROI/MASK 为闭合多边形，至少 3 个顶点且不自交；
//   - LINE 为折线，至少 2 个顶点；
//   - line_direction 仅 LINE 规则有效，区域规则必须为 0。
type DetectionRule struct {
	Role          DetectionRuleRole      `json:"role"`
	LineDirection DetectionLineDirection `json:"lineDirection"`
	Points        []DetectionPoint       `json:"points"`
}
