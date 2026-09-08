// Package manager coordinates multi-provider queries and aggregation.
package manager

import (
	"github.com/emmmdty/token-usage/internal/provider"
)

// Manager 管理多个 provider
type Manager struct {
	providers []provider.Provider
}

// NewManager 创建一个新的 manager
func NewManager() *Manager {
	return &Manager{
		providers: make([]provider.Provider, 0),
	}
}

// Add 添加一个 provider
func (m *Manager) Add(p provider.Provider) {
	if p.IsAvailable() {
		m.providers = append(m.providers, p)
	}
}

// GetAll 获取所有可用 provider 的用量
func (m *Manager) GetAll() ([]*provider.Usage, error) {
	var results []*provider.Usage
	for _, p := range m.providers {
		usage, err := p.GetUsage()
		if err != nil {
			continue // 跳过失败的 provider
		}
		results = append(results, usage)
	}
	return results, nil
}

// GetProviders 返回所有已注册的 provider 名称
func (m *Manager) GetProviders() []string {
	var names []string
	for _, p := range m.providers {
		names = append(names, p.Name())
	}
	return names
}
