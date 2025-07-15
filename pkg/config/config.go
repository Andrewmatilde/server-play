// Package config 提供压测工具的配置管理功能
//
// 需求和预设:
// 1. 配置驱动: 使用JSON配置文件替代复杂的命令行参数，提高易用性
// 2. 灵活的流量控制: 支持QPS模式(固定请求速率)和并发模式(固定协程数)
// 3. 操作比例配置: 支持传感器数据上报、读写操作、批量操作、查询操作的比例设置
// 4. 数据特征配置: 支持设备ID范围、数据大小范围等时序数据特征配置
// 5. 验证机制: 配置加载后进行完整性和合理性验证
// 6. 默认配置: 提供合理的默认值，确保开箱即用
// 7. 类型安全: 使用强类型配置，避免运行时错误
//
// 设计原则:
// - 配置文件优先，命令行参数作为覆盖选项
// - 操作比例总和不超过1.0，支持部分操作测试
// - 内部字段自动计算，简化配置文件结构
// - 支持配置的保存和加载，便于配置管理
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Config struct {
	// 服务器配置
	ServerURL string `json:"server_url"`
	Duration  int    `json:"duration_seconds"` // 使用秒数，方便配置文件

	// 流量控制配置
	Mode        string `json:"mode"`        // "qps" 或 "concurrency"
	QPS         int    `json:"qps"`
	Concurrency int    `json:"concurrency"`

	// 操作比例配置
	SensorDataRatio float64 `json:"sensor_data_ratio"` // 传感器数据上报比例
	SensorRWRatio   float64 `json:"sensor_rw_ratio"`   // 传感器读写操作比例
	BatchRWRatio    float64 `json:"batch_rw_ratio"`    // 批量操作比例
	QueryRatio      float64 `json:"query_ratio"`       // 查询操作比例

	// 数据配置
	KeyRange       int `json:"key_range"`        // 设备ID范围
	DataSizeMin    int `json:"data_size_min"`    // 最小数据大小（字节）
	DataSizeMax    int `json:"data_size_max"`    // 最大数据大小（字节）
	ReportInterval int `json:"report_interval"`  // 报告间隔（秒）

	// 内部计算字段
	totalRatio         float64       `json:"-"`
	durationTime       time.Duration `json:"-"`
	reportIntervalTime time.Duration `json:"-"`
}

func New() *Config {
	c := &Config{
		ServerURL:       "http://localhost:8080",
		Duration:        30,
		Mode:            "qps",
		QPS:             100,
		Concurrency:     10,
		SensorDataRatio: 0.7,
		SensorRWRatio:   0.2,
		BatchRWRatio:    0.05,
		QueryRatio:      0.05,
		KeyRange:        1000,
		DataSizeMin:     512,
		DataSizeMax:     2048,
		ReportInterval:  1,
	}
	c.calculateDerivedFields()
	return c
}

func (c *Config) LoadFromFile(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %v", err)
	}

	if err := json.Unmarshal(data, c); err != nil {
		return fmt.Errorf("解析配置文件失败: %v", err)
	}

	c.calculateDerivedFields()
	return nil
}

func (c *Config) SaveToFile(filename string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %v", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %v", err)
	}

	return nil
}

func (c *Config) calculateDerivedFields() {
	c.totalRatio = c.SensorDataRatio + c.SensorRWRatio + c.BatchRWRatio + c.QueryRatio
	c.durationTime = time.Duration(c.Duration) * time.Second
	c.reportIntervalTime = time.Duration(c.ReportInterval) * time.Second
}

func (c *Config) Validate() error {
	// 验证模式
	if c.Mode != "qps" && c.Mode != "concurrency" {
		return fmt.Errorf("无效的模式: %s, 必须是 'qps' 或 'concurrency'", c.Mode)
	}

	// 验证QPS
	if c.Mode == "qps" && c.QPS <= 0 {
		return fmt.Errorf("QPS必须大于0")
	}

	// 验证并发数
	if c.Mode == "concurrency" && c.Concurrency <= 0 {
		return fmt.Errorf("并发数必须大于0")
	}

	// 验证比例总和
	if c.totalRatio <= 0 || c.totalRatio > 1.0 {
		return fmt.Errorf("操作比例总和必须在(0,1]范围内，当前为: %.3f", c.totalRatio)
	}

	// 验证各个比例
	if c.SensorDataRatio < 0 || c.SensorRWRatio < 0 || c.BatchRWRatio < 0 || c.QueryRatio < 0 {
		return fmt.Errorf("所有操作比例必须大于等于0")
	}

	// 验证数据大小
	if c.DataSizeMin <= 0 || c.DataSizeMax <= 0 || c.DataSizeMin > c.DataSizeMax {
		return fmt.Errorf("数据大小配置无效: min=%d, max=%d", c.DataSizeMin, c.DataSizeMax)
	}

	// 验证键值范围
	if c.KeyRange <= 0 {
		return fmt.Errorf("设备ID范围必须大于0")
	}

	return nil
}

func (c *Config) Print() {
	fmt.Printf("=== 压测配置 ===\n")
	fmt.Printf("服务器地址: %s\n", c.ServerURL)
	fmt.Printf("测试持续时间: %d 秒\n", c.Duration)
	fmt.Printf("流量控制模式: %s\n", c.Mode)
	if c.Mode == "qps" {
		fmt.Printf("目标QPS: %d\n", c.QPS)
	} else {
		fmt.Printf("并发协程数: %d\n", c.Concurrency)
	}
	fmt.Printf("操作比例:\n")
	fmt.Printf("  传感器数据上报: %.2f\n", c.SensorDataRatio)
	fmt.Printf("  传感器读写操作: %.2f\n", c.SensorRWRatio)
	fmt.Printf("  批量操作: %.2f\n", c.BatchRWRatio)
	fmt.Printf("  查询操作: %.2f\n", c.QueryRatio)
	fmt.Printf("  总比例: %.2f\n", c.totalRatio)
	fmt.Printf("设备ID范围: %d\n", c.KeyRange)
	fmt.Printf("数据大小范围: %d - %d 字节\n", c.DataSizeMin, c.DataSizeMax)
	fmt.Printf("报告间隔: %d 秒\n", c.ReportInterval)
	fmt.Printf("================\n")
}

// GetDuration 获取持续时间
func (c *Config) GetDuration() time.Duration {
	return c.durationTime
}

// GetReportInterval 获取报告间隔
func (c *Config) GetReportInterval() time.Duration {
	return c.reportIntervalTime
}

// GetOperationType 根据随机数和比例返回操作类型
func (c *Config) GetOperationType(rand float64) string {
	// 将随机数映射到实际比例范围
	rand = rand * c.totalRatio

	if rand < c.SensorDataRatio {
		return "sensor-data"
	}
	rand -= c.SensorDataRatio

	if rand < c.SensorRWRatio {
		return "sensor-rw"
	}
	rand -= c.SensorRWRatio

	if rand < c.BatchRWRatio {
		return "batch-rw"
	}
	rand -= c.BatchRWRatio

	return "query"
}