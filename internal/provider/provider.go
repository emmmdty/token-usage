package provider

import (
	"time"
)

// QuotaWindow 表示一个配额窗口
type QuotaWindow struct {
	Status  string    `json:"status"`
	Percent int       `json:"percent"`
	ResetAt time.Time `json:"resetAt"`
}

// StatusUnknown marks a window whose usage cannot be determined (e.g. the
// provider only exposes a validity probe). Rendered as "n/a".
const StatusUnknown = "unknown"

// Plan flavors for providers that carry multiple subscription types.
const (
	PlanCoding = "coding"
	PlanAgent  = "agent"
)

// Usage 表示配额使用情况
type Usage struct {
	Provider string      `json:"provider"`
	PlanType string      `json:"planType"`
	Rolling  QuotaWindow `json:"rolling"`
	Weekly   QuotaWindow `json:"weekly"`
	Monthly  QuotaWindow `json:"monthly"`
	// Note carries human-readable context when windows cannot be fully
	// resolved (e.g. "balance: $12.34", "install arkcli for full quota").
	Note string `json:"note,omitempty"`
	// Account is the real account identity the provider could read at
	// query time (e.g. arkcli whoami name, key tail from a probe), used
	// for display; empty when nothing readable is available.
	Account string      `json:"account,omitempty"`
	RawData interface{} `json:"rawData,omitempty"`
}

// Provider 定义了获取用量数据的接口
type Provider interface {
	// Name 返回 provider 名称
	Name() string
	// GetUsage 获取当前配额使用情况
	GetUsage() (*Usage, error)
	// IsAvailable 检查认证信息是否可用
	IsAvailable() bool
}
