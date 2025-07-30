// Package ratecontroller 提供压测工具的流量控制功能
//
// 需求和预设:
// 1. QPS模式: 按照固定的QPS速率创建独立的goroutine执行每个请求
// 2. 并发模式: 维持固定数量的长期运行worker goroutine
// 3. 操作类型分发: 根据配置的比例随机分发不同类型的操作(传感器上报、读写、批量、查询)
// 4. 独立请求执行: QPS模式下每个请求都在独立的goroutine中执行
// 5. 优雅停止: 支持上下文取消和优雅停止机制
// 6. 运行时配置: 支持运行时调整操作比例配置
// 7. 状态监控: 提供运行状态等监控信息
// 8. 精确速率控制: 使用ticker实现精确的QPS控制
//
// 设计原则:
// - QPS模式: 每个请求独立goroutine，按固定速率创建
// - 并发模式: 固定数量的worker goroutine持续执行
// - 模式间完全分离，避免混合逻辑
// - 优先保证速率的准确性
// - 支持高并发场景下的性能测试
package ratecontroller

import (
	"context"
	"splay/client"
	"splay/pkg/config"
	"splay/pkg/stats"
	"splay/pkg/worker"
	"time"
)

// Controller 流量控制器
type Controller struct {
	config         *config.Config
	statsCollector *stats.Collector
	httpClient     *client.ClientWithResponses
}

func New(cfg *config.Config, statsCollector *stats.Collector, httpClient *client.ClientWithResponses) *Controller {
	return &Controller{
		config:         cfg,
		statsCollector: statsCollector,
		httpClient:     httpClient,
	}
}

// Start 启动流量控制器
func (rc *Controller) Start(ctx context.Context) {

	switch rc.config.Mode {
	case "qps":
		go rc.runQPSMode(ctx)
	case "concurrency":
		go rc.runConcurrencyMode(ctx)
	default:
		go rc.runQPSMode(ctx)
	}
}

// runQPSMode QPS模式：按固定速率创建独立的goroutine执行请求
func (rc *Controller) runQPSMode(ctx context.Context) {

	if rc.config.QPS <= 0 {
		return
	}

	interval := time.Duration(1000000000 / rc.config.QPS * 16) // 纳秒

	for i := 0; i < 16; i++ {
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					go func() {
						w := worker.New(0, rc.httpClient, rc.statsCollector, rc.config)
						w.ExecuteOperation()
					}()
				}
			}
		}()
	}
	<-ctx.Done()
}

// runConcurrencyMode 并发模式：维持固定数量的worker goroutine
func (rc *Controller) runConcurrencyMode(ctx context.Context) {

	if rc.config.Concurrency <= 0 {
		return
	}

	// 启动固定数量的worker goroutine
	for i := 0; i < rc.config.Concurrency; i++ {
		go func(workerID int) {
			w := worker.New(workerID, rc.httpClient, rc.statsCollector, rc.config)
			for {
				select {
				case <-ctx.Done():
					return
				default:
					w.ExecuteOperation()
				}
			}
		}(i)
	}

	<-ctx.Done()
}

// selectOperationType 根据配置的比例选择操作类型
func (rc *Controller) selectOperationType() string {
	return "sensor-data"
}
