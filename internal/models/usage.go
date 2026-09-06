package models

import (
	"time"

	"github.com/emmmdty/token-usage/internal/provider"
)

// QuotaWindow 表示一个配额窗口
type QuotaWindow struct {
	Status   string    `json:"status"`
	Percent  int       `json:"percent"`
	ResetsAt time.Time `json:"resetsAt"`
}

// Usage 表示配额使用情况
type Usage struct {
	Rolling QuotaWindow `json:"rolling"`
	Weekly  QuotaWindow `json:"weekly"`
	Monthly QuotaWindow `json:"monthly"`
	Note    string      `json:"note,omitempty"`
	// Account is the provider-readable account identity (display only).
	Account string `json:"account,omitempty"`
}

// Model 表示一个可用模型
type Model struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Pricing struct {
		Input  float64 `json:"input"`
		Output float64 `json:"output"`
	} `json:"pricing"`
}

// FromProviderUsage 将 provider.Usage 转换为 models.Usage
func FromProviderUsage(p *provider.Usage) *Usage {
	return &Usage{
		Rolling: QuotaWindow{
			Status:   p.Rolling.Status,
			Percent:  p.Rolling.Percent,
			ResetsAt: p.Rolling.ResetAt,
		},
		Weekly: QuotaWindow{
			Status:   p.Weekly.Status,
			Percent:  p.Weekly.Percent,
			ResetsAt: p.Weekly.ResetAt,
		},
		Monthly: QuotaWindow{
			Status:   p.Monthly.Status,
			Percent:  p.Monthly.Percent,
			ResetsAt: p.Monthly.ResetAt,
		},
		Note:    p.Note,
		Account: p.Account,
	}
}
