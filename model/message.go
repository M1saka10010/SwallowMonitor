package model

import "encoding/json"

// Message types reported by the agent.
const (
	TypeSystemInfo  = "system_info"
	TypeSystemUsage = "system_usage"
)

// Envelope is the unified packet wrapper used by the agent.
type Envelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// SystemInfo mirrors the agent's system_info data object.
type SystemInfo struct {
	Hostname             string `json:"hostname"`
	Uptime               uint64 `json:"uptime"`
	BootTime             uint64 `json:"bootTime"`
	ModelName            string `json:"modelName"`
	Cores                int32  `json:"cores"`
	Processes            uint64 `json:"processes"`
	OS                   string `json:"os"`
	Platform             string `json:"platform"`
	PlatformFamily       string `json:"platformFamily"`
	PlatformVersion      string `json:"platformVersion"`
	KernelVersion        string `json:"kernelVersion"`
	KernelArch           string `json:"kernelArch"`
	VirtualizationSystem string `json:"virtualizationSystem"`
	VirtualizationRole   string `json:"virtualizationRole"`
	HostID               string `json:"hostId"`
}

// SystemUsage mirrors the agent's system_usage data object.
type SystemUsage struct {
	CPUUsage     float64 `json:"cpuUsage"`
	MemoryTotal  uint64  `json:"memoryTotal"`
	MemoryUsed   uint64  `json:"memoryUsed"`
	SwapTotal    uint64  `json:"swapTotal"`
	SwapUsed     uint64  `json:"swapUsed"`
	DiskTotal    uint64  `json:"diskTotal"`
	DiskUsed     uint64  `json:"diskUsed"`
	NetRecv      uint64  `json:"netRecv"`
	NetSend      uint64  `json:"netSend"`
	NetRecvSpeed float64 `json:"netRecvSpeed"`
	NetSendSpeed float64 `json:"netSendSpeed"`
	Load1        float64 `json:"load1"`
	Load5        float64 `json:"load5"`
	Load15       float64 `json:"load15"`
	Timestamp    uint64  `json:"timestamp"`
}
