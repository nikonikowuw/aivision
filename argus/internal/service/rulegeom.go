package service

import (
	"math"

	"argus/app/internal/model"
	"argus/app/internal/pkg/errno"
)

// 规则几何校验的浮点容差：归一化坐标（[0,1]）下，1e-9 足以区分
// 「共线/相触」与「明确交叉」，同时容忍 UI 绘制与 JSON 往返的浮点误差。
const geometryEpsilon = 1e-9

// ValidateRules 对任务级检测规则做字段契约与几何校验（纯函数，不依赖 HTTP/DB）。
// 按 design §6 逐规则校验五项：
//  1. 角色合法性：role 必须为 ROI / MASK / LINE（CodeInvalidParam）；
//  2. 坐标范围：所有点 x,y ∈ [0,1]（CodeRuleOutOfBounds）；
//  3. 顶点数量：ROI/MASK ≥ 3、LINE ≥ 2（CodeRuleTooFewPoints）；
//  4. 多边形自交：ROI/MASK 非相邻边两两不相交，相邻边共享端点除外（CodeRuleSelfIntersect）；
//  5. 方向字段：非 LINE 规则不得携带非零 line_direction（CodeInvalidParam）。
//
// 校验顺序与实现细节（自交判定的叉积与共线处理）为后端权威实现，
// 前端不得自行放行未通过本函数的规则。
func ValidateRules(rules []model.DetectionRule) error {
	for index := range rules {
		rule := &rules[index]
		switch rule.Role {
		case model.DetectionRuleRoleROI, model.DetectionRuleRoleMask:
			if len(rule.Points) < 3 {
				return errno.New(errno.CodeRuleTooFewPoints)
			}
		case model.DetectionRuleRoleLine:
			if len(rule.Points) < 2 {
				return errno.New(errno.CodeRuleTooFewPoints)
			}
		default:
			return errno.New(errno.CodeInvalidParam)
		}
		if rule.Role != model.DetectionRuleRoleLine && rule.LineDirection != model.DetectionLineDirectionBoth {
			return errno.New(errno.CodeInvalidParam)
		}
		for _, point := range rule.Points {
			if point.X < 0 || point.X > 1 || point.Y < 0 || point.Y > 1 {
				return errno.New(errno.CodeRuleOutOfBounds)
			}
		}

		segmentCount := len(rule.Points)
		if rule.Role == model.DetectionRuleRoleLine {
			segmentCount = len(rule.Points) - 1
		}
		for i := 0; i < segmentCount; i++ {
			p1 := rule.Points[i]
			p2 := rule.Points[(i+1)%len(rule.Points)]
			if math.Abs(p1.X-p2.X) <= 1e-6 && math.Abs(p1.Y-p2.Y) <= 1e-6 {
				return errno.New(errno.CodeRuleSelfIntersect)
			}
		}

		if rule.Role == model.DetectionRuleRoleROI || rule.Role == model.DetectionRuleRoleMask {
			if math.Abs(polygonSignedArea(rule.Points)) <= 1e-6 {
				return errno.New(errno.CodeRuleSelfIntersect)
			}
			if polygonSelfIntersects(rule.Points) {
				return errno.New(errno.CodeRuleSelfIntersect)
			}
		}
	}
	return nil
}

// polygonSignedArea 计算多边形有向面积（顶点需为顺时针或逆时针排列）。
func polygonSignedArea(points []model.DetectionPoint) float64 {
	n := len(points)
	area := 0.0
	for i := 0; i < n; i++ {
		cur := points[i]
		next := points[(i+1)%n]
		area += cur.X*next.Y - next.X*cur.Y
	}
	return area * 0.5
}

// polygonSelfIntersects 判定闭合多边形是否存在自交：对每对非相邻边做线段相交
// 检测。顶点数受场景约束（通常 <20），O(n²) 可接受。
func polygonSelfIntersects(points []model.DetectionPoint) bool {
	n := len(points)
	for i := 0; i < n; i++ {
		// 边 i：points[i] → points[(i+1)%n]
		for j := i + 1; j < n; j++ {
			// 跳过相邻边（共享端点）：边 i 与 i+1，以及首尾闭合边 0 与 n-1。
			if j == i+1 || (i == 0 && j == n-1) {
				continue
			}
			if segmentsIntersect(points[i], points[(i+1)%n], points[j], points[(j+1)%n]) {
				return true
			}
		}
	}
	return false
}

// cross 二维叉积：向量 oa 与 ob 的有向面积（正=逆时针，负=顺时针，0=共线）。
func cross(o, a, b model.DetectionPoint) float64 {
	return (a.X-o.X)*(b.Y-o.Y) - (a.Y-o.Y)*(b.X-o.X)
}

// onSegment 判定 q 是否位于线段 pr 的包围盒内（调用方已确认三点共线）。
func onSegment(p, q, r model.DetectionPoint) bool {
	return math.Min(p.X, r.X)-geometryEpsilon <= q.X && q.X <= math.Max(p.X, r.X)+geometryEpsilon &&
		math.Min(p.Y, r.Y)-geometryEpsilon <= q.Y && q.Y <= math.Max(p.Y, r.Y)+geometryEpsilon
}

// segmentsIntersect 标准线段相交判定：叉积符号测试 + 共线时的区间重叠。
// p1-p2 与 q1-q2 为两条线段的端点。
func segmentsIntersect(p1, p2, q1, q2 model.DetectionPoint) bool {
	d1 := cross(q1, q2, p1)
	d2 := cross(q1, q2, p2)
	d3 := cross(p1, p2, q1)
	d4 := cross(p1, p2, q2)

	// 严格跨立：两条线段各自把另一条的端点分居两侧。
	if ((d1 > geometryEpsilon && d2 < -geometryEpsilon) || (d1 < -geometryEpsilon && d2 > geometryEpsilon)) &&
		((d3 > geometryEpsilon && d4 < -geometryEpsilon) || (d3 < -geometryEpsilon && d4 > geometryEpsilon)) {
		return true
	}
	// 共线退化为区间重叠测试：任一端点落在另一条线段的包围盒内即相交。
	if math.Abs(d1) <= geometryEpsilon && onSegment(q1, p1, q2) {
		return true
	}
	if math.Abs(d2) <= geometryEpsilon && onSegment(q1, p2, q2) {
		return true
	}
	if math.Abs(d3) <= geometryEpsilon && onSegment(p1, q1, p2) {
		return true
	}
	if math.Abs(d4) <= geometryEpsilon && onSegment(p1, q2, p2) {
		return true
	}
	return false
}
