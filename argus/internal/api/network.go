package api

import (
	"github.com/gin-gonic/gin"

	"argus/app/internal/middleware"
	"argus/app/internal/pkg/errno"
	"argus/app/internal/pkg/netconfig"
	"argus/app/internal/pkg/response"
	"argus/app/internal/service"
)

// NetworkHandler 网络配置相关 HTTP 控制器。
type NetworkHandler struct {
	srv service.NetworkService
}

// NewNetworkHandler 创建网络配置控制器。
func NewNetworkHandler(srv service.NetworkService) *NetworkHandler {
	return &NetworkHandler{srv: srv}
}

// GetOverview 获取网络概览及所有接口状态。
func (h *NetworkHandler) GetOverview(c *gin.Context) {
	overview, err := h.srv.GetOverview(c.Request.Context())
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Success(c, overview)
}

// GetTransaction 查询当前候选事务详情。
func (h *NetworkHandler) GetTransaction(c *gin.Context) {
	txnID := c.Param("transactionId")
	if txnID == "" {
		_ = c.Error(errno.New(errno.CodeInvalidParam))
		return
	}

	txn, err := h.srv.GetTransaction(c.Request.Context(), txnID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Success(c, txn)
}

// ApplyInterfaceRequest 接口应用请求参数体。
type ApplyInterfaceRequest struct {
	Mode       netconfig.IPMode `json:"mode" binding:"required"`
	Primary    bool             `json:"primary"`
	Address    *string          `json:"address"`
	Prefix     *int             `json:"prefix"`
	Gateway    *string          `json:"gateway"`
	DNSServers []string         `json:"dnsServers"`
}

// ApplyInterface 应用指定网卡配置为候选事务。
func (h *NetworkHandler) ApplyInterface(c *gin.Context) {
	ifaceID := c.Param("interfaceId")
	if ifaceID == "" {
		_ = c.Error(errno.New(errno.CodeInvalidParam))
		return
	}

	var req ApplyInterfaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errno.New(errno.CodeInvalidParam))
		return
	}

	var actorID uint64
	var actorUsername string
	if identity, ok := middleware.IdentityFromContext(c); ok {
		actorID = identity.UserID
		actorUsername = identity.Username
	}

	res, err := h.srv.ApplyInterface(c.Request.Context(), ifaceID, service.ApplyInterfaceInput{
		Mode:          req.Mode,
		Primary:       req.Primary,
		Address:       req.Address,
		Prefix:        req.Prefix,
		Gateway:       req.Gateway,
		DNSServers:    req.DNSServers,
		ActorID:       actorID,
		ActorUsername: actorUsername,
		ClientIP:      c.ClientIP(),
	})
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Success(c, res)
}

// ConfirmTransaction 确认候选事务。
func (h *NetworkHandler) ConfirmTransaction(c *gin.Context) {
	txnID := c.Param("transactionId")
	if txnID == "" {
		_ = c.Error(errno.New(errno.CodeInvalidParam))
		return
	}

	var actorID uint64
	var actorUsername string
	if identity, ok := middleware.IdentityFromContext(c); ok {
		actorID = identity.UserID
		actorUsername = identity.Username
	}

	res, err := h.srv.ConfirmTransaction(c.Request.Context(), txnID, actorID, actorUsername, c.ClientIP())
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Success(c, res)
}

// CancelTransaction 取消候选事务。
func (h *NetworkHandler) CancelTransaction(c *gin.Context) {
	txnID := c.Param("transactionId")
	if txnID == "" {
		_ = c.Error(errno.New(errno.CodeInvalidParam))
		return
	}

	var actorID uint64
	var actorUsername string
	if identity, ok := middleware.IdentityFromContext(c); ok {
		actorID = identity.UserID
		actorUsername = identity.Username
	}

	res, err := h.srv.CancelTransaction(c.Request.Context(), txnID, actorID, actorUsername, c.ClientIP())
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Success(c, res)
}

// FactoryReset 恢复出厂基线设置。
func (h *NetworkHandler) FactoryReset(c *gin.Context) {
	ifaceID := c.Param("interfaceId")
	if ifaceID == "" {
		_ = c.Error(errno.New(errno.CodeInvalidParam))
		return
	}

	var actorID uint64
	var actorUsername string
	if identity, ok := middleware.IdentityFromContext(c); ok {
		actorID = identity.UserID
		actorUsername = identity.Username
	}

	res, err := h.srv.FactoryReset(c.Request.Context(), ifaceID, actorID, actorUsername, c.ClientIP())
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Success(c, res)
}

// BondRequest 切到 active-backup 或 lacp-aggregation 时的 bond 参数体。
type BondRequest struct {
	SlaveIDs       []string                      `json:"slaveIds"`
	PrimarySlaveID string                        `json:"primarySlaveId,omitempty"`
	XmitHashPolicy *netconfig.BondXmitHashPolicy `json:"xmitHashPolicy,omitempty"`
	IPv4           ApplyInterfaceRequest         `json:"ipv4"`
}

// GatewayRequest 切到 gateway 时的网关参数体。
type GatewayRequest struct {
	DownstreamInterfaceID string `json:"downstreamInterfaceId"`
	PoolStart             string `json:"poolStart"`
	PoolEnd               string `json:"poolEnd"`
	Prefix                int    `json:"prefix"`
	LeaseDurationSeconds  int64  `json:"leaseDurationSeconds"`
	IPForward             bool   `json:"ipForward"`
}

// SwitchModeRequest 模式切换请求参数体。
type SwitchModeRequest struct {
	Mode    netconfig.NetworkMode `json:"mode" binding:"required"`
	Bond    *BondRequest          `json:"bond"`
	Gateway *GatewayRequest       `json:"gateway"`
}

// SwitchMode 切换整机网络工作模式（候选事务）。
func (h *NetworkHandler) SwitchMode(c *gin.Context) {
	var req SwitchModeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errno.New(errno.CodeInvalidParam))
		return
	}

	var actorID uint64
	var actorUsername string
	if identity, ok := middleware.IdentityFromContext(c); ok {
		actorID = identity.UserID
		actorUsername = identity.Username
	}

	input := service.SwitchModeInput{
		Mode:          req.Mode,
		ActorID:       actorID,
		ActorUsername: actorUsername,
		ClientIP:      c.ClientIP(),
	}
	if req.Mode == netconfig.NetworkModeActiveBackup {
		if req.Bond == nil || req.Gateway != nil {
			_ = c.Error(errno.New(errno.CodeInvalidParam))
			return
		}
		input.SlaveIDs = req.Bond.SlaveIDs
		input.PrimarySlaveID = req.Bond.PrimarySlaveID
		input.BondIPv4 = service.ApplyInterfaceInput{
			Mode:       req.Bond.IPv4.Mode,
			Primary:    req.Bond.IPv4.Primary,
			Address:    req.Bond.IPv4.Address,
			Prefix:     req.Bond.IPv4.Prefix,
			Gateway:    req.Bond.IPv4.Gateway,
			DNSServers: req.Bond.IPv4.DNSServers,
		}
	} else if req.Mode == netconfig.NetworkModeLACP {
		if req.Bond == nil || req.Gateway != nil {
			_ = c.Error(errno.New(errno.CodeInvalidParam))
			return
		}
		input.SlaveIDs = req.Bond.SlaveIDs
		input.XmitHashPolicy = req.Bond.XmitHashPolicy
		input.BondIPv4 = service.ApplyInterfaceInput{
			Mode:       req.Bond.IPv4.Mode,
			Primary:    req.Bond.IPv4.Primary,
			Address:    req.Bond.IPv4.Address,
			Prefix:     req.Bond.IPv4.Prefix,
			Gateway:    req.Bond.IPv4.Gateway,
			DNSServers: req.Bond.IPv4.DNSServers,
		}
	} else if req.Mode == netconfig.NetworkModeGateway {
		if req.Gateway == nil || req.Bond != nil {
			_ = c.Error(errno.New(errno.CodeInvalidParam))
			return
		}
		input.Gateway = &service.GatewayInput{
			DownstreamInterfaceID: req.Gateway.DownstreamInterfaceID,
			PoolStart:             req.Gateway.PoolStart,
			PoolEnd:               req.Gateway.PoolEnd,
			Prefix:                req.Gateway.Prefix,
			LeaseDurationSeconds:  req.Gateway.LeaseDurationSeconds,
			IPForward:             req.Gateway.IPForward,
		}
	} else {
		if req.Bond != nil || req.Gateway != nil {
			_ = c.Error(errno.New(errno.CodeInvalidParam))
			return
		}
	}

	res, err := h.srv.SwitchMode(c.Request.Context(), input)
	if err != nil {
		_ = c.Error(err)
		return
	}
	response.Success(c, res)
}
