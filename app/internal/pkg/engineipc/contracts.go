package engineipc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/status"

	aivisionv1 "niko-vue-admin/app/internal/proto/aivision/v1"
)

// 稳定业务响应 code（response.message 的 code 字段，空串表示成功）。
// 调用方只判断 Code，不得解析 ErrorMessage 的文本。
const (
	// CodeOK 表示业务成功。
	CodeOK = ""
	// CodeIPCUNAVAILABLE 表示未注入业务适配器时 fail closed 的稳定码。
	CodeIPCUNAVAILABLE = "IPC_UNAVAILABLE"
	// CodeInternalError 表示 adapter 返回普通内部错误时归一化的稳定码。
	CodeInternalError = "INTERNAL_ERROR"
)

// AdapterError 是业务 adapter 返回的稳定业务错误。Code 原样进入响应；
// ErrorMessage 仅供诊断，调用方不得解析其文本。
type AdapterError struct {
	Code         string
	ErrorMessage string
}

// Error 实现 error 接口；业务调用方应使用 Code 判断稳定错误，不解析文本。
func (e *AdapterError) Error() string {
	if e == nil {
		return "internal error"
	}
	return fmt.Sprintf("%s: %s", e.Code, e.ErrorMessage)
}

// NewAdapterError 构造带稳定 code 的业务错误。空 code 不代表成功，统一转换为内部错误。
func NewAdapterError(code, message string) *AdapterError {
	if strings.TrimSpace(code) == "" {
		return &AdapterError{Code: CodeInternalError, ErrorMessage: "internal error"}
	}
	return &AdapterError{Code: code, ErrorMessage: message}
}

// OrphanDisposition 是孤儿图片对账结果：Go 告知 Engine 需要保留/可删除的图片 ID。
type OrphanDisposition struct {
	RetainImageIDs []string
	DeleteImageIDs []string
}

// DesiredStateAdapter 提供 Go 权威的 DesiredState 与 revision。
// DesiredState 返回 (nil, nil) 视为内部实现错误，由 service 归一化为 INTERNAL_ERROR。
type DesiredStateAdapter interface {
	DesiredState(ctx context.Context, currentRevision uint64) (*aivisionv1.DesiredState, error)
}

// ReportAdapter 承接 Engine 的上报。只有 adapter 真正接受了数据，service 才返回空 code；
// 未注入适配器时统一 fail closed 返回 IPC_UNAVAILABLE。
type ReportAdapter interface {
	AcceptAlarm(ctx context.Context, alarm *aivisionv1.AlarmEvent) error
	AcceptTaskState(ctx context.Context, state *aivisionv1.TaskState) error
	AcceptInstanceState(ctx context.Context, state *aivisionv1.InstanceState) error
	AcceptMetrics(ctx context.Context, telemetry *aivisionv1.DeviceTelemetry) error
	ReconcileOrphanImages(ctx context.Context, orphans []*aivisionv1.OrphanImageEntry) (OrphanDisposition, error)
}

// isTransportError 判断 adapter 返回的错误是否必须继续作为 gRPC transport status 传播。
// context 取消/超时和显式 status.Error 都不能降级为响应体中的业务错误。
func isTransportError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	_, ok := status.FromError(err)
	return ok
}

// normalizeAdapterError 把业务 adapter 返回的 error 归一化为稳定响应 code + 受控诊断文本：
//   - *AdapterError：Code 原样返回，ErrorMessage 仅诊断；
//   - 空 code 或其他普通 error：统一返回 INTERNAL_ERROR 与受控文本；
//   - nil：返回成功空 code。
//
// transport error 必须由 service 先用 isTransportError 检查并原样返回，不能调用本函数生成 ACK。
// 返回的 second 为受控诊断文本（可写进 error_message，不含内部 cause）。
func normalizeAdapterError(err error) (code, message string) {
	if err == nil {
		return CodeOK, ""
	}
	var ae *AdapterError
	if errors.As(err, &ae) && ae != nil {
		if strings.TrimSpace(ae.Code) == "" {
			return CodeInternalError, "internal error"
		}
		return ae.Code, ae.ErrorMessage
	}
	return CodeInternalError, "internal error"
}

// unavailableDesiredStateAdapter 是生产默认 adapter：未配置持久化实现时 fail closed，
// 不让 Engine 得到成功 ACK。
type unavailableDesiredStateAdapter struct{}

func (unavailableDesiredStateAdapter) DesiredState(context.Context, uint64) (*aivisionv1.DesiredState, error) {
	return nil, NewAdapterError(CodeIPCUNAVAILABLE, "desired state service unavailable")
}

// unavailableReportAdapter 是生产默认上报 adapter：未配置持久化实现时 fail closed，
// 禁止对未持久化的告警、状态、遥测或孤儿图片上报返回成功 ACK。
type unavailableReportAdapter struct{}

func (unavailableReportAdapter) AcceptAlarm(context.Context, *aivisionv1.AlarmEvent) error {
	return NewAdapterError(CodeIPCUNAVAILABLE, "report service unavailable")
}

func (unavailableReportAdapter) AcceptTaskState(context.Context, *aivisionv1.TaskState) error {
	return NewAdapterError(CodeIPCUNAVAILABLE, "report service unavailable")
}

func (unavailableReportAdapter) AcceptInstanceState(context.Context, *aivisionv1.InstanceState) error {
	return NewAdapterError(CodeIPCUNAVAILABLE, "report service unavailable")
}

func (unavailableReportAdapter) AcceptMetrics(context.Context, *aivisionv1.DeviceTelemetry) error {
	return NewAdapterError(CodeIPCUNAVAILABLE, "report service unavailable")
}

func (unavailableReportAdapter) ReconcileOrphanImages(context.Context, []*aivisionv1.OrphanImageEntry) (OrphanDisposition, error) {
	return OrphanDisposition{}, NewAdapterError(CodeIPCUNAVAILABLE, "report service unavailable")
}
