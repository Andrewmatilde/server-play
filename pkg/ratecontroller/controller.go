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
	"math/rand"
	"splay/client"
	"splay/pkg/config"
	"splay/pkg/stats"
	"splay/pkg/worker"
	"sync"
	"time"
)

// Controller 流量控制器
type Controller struct {
	config         *config.Config
	statsCollector *stats.Collector
	httpClient     *client.ClientWithResponses
	done           chan struct{}
	wg             sync.WaitGroup
	mu             sync.RWMutex
	running        bool
}

func New(cfg *config.Config, statsCollector *stats.Collector, httpClient *client.ClientWithResponses) *Controller {
	return &Controller{
		config:         cfg,
		statsCollector: statsCollector,
		httpClient:     httpClient,
		done:           make(chan struct{}),
	}
}

// Start 启动流量控制器
func (rc *Controller) Start(ctx context.Context) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	
	if rc.running {
		return
	}
	
	rc.running = true
	
	switch rc.config.Mode {
	case "qps":
		rc.wg.Add(1)
		go rc.runQPSMode(ctx)
	case "concurrency":
		rc.wg.Add(1)
		go rc.runConcurrencyMode(ctx)
	default:
		// 默认使用QPS模式
		rc.wg.Add(1)
		go rc.runQPSMode(ctx)
	}
}

// Stop 停止流量控制器
func (rc *Controller) Stop() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	
	if !rc.running {
		return
	}
	
	rc.running = false
	close(rc.done)
	rc.wg.Wait()
}

// runQPSMode QPS模式：按固定速率创建独立的goroutine执行请求
func (rc *Controller) runQPSMode(ctx context.Context) {
	defer rc.wg.Done()
	
	if rc.config.QPS <= 0 {
		return
	}
	
	// 计算请求间隔
	interval := time.Duration(1000000000 / rc.config.QPS) // 纳秒
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	// 添加并发限制，防止goroutine数量无限增长
	maxConcurrentRequests := rc.config.QPS * 2 // 允许的最大并发请求数
	if maxConcurrentRequests > 10000 {
		maxConcurrentRequests = 10000 // 硬限制
	}
	semaphore := make(chan struct{}, maxConcurrentRequests)
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-rc.done:
			return
		case <-ticker.C:
			// 根据配置的比例选择操作类型
			operation := rc.selectOperationType()
			
			// 非阻塞方式尝试获取信号量
			select {
			case semaphore <- struct{}{}:
				// 为每个请求创建独立的goroutine
				go func() {
					defer func() { <-semaphore }() // 释放信号量
					w := worker.New(0, rc.httpClient, rc.statsCollector, rc.config)
					w.ExecuteOperation(operation)
				}()
			default:
				// 如果并发数已达到限制，丢弃这个请求
				// 这样可以防止系统资源耗尽
			}
		}
	}
}

// runConcurrencyMode 并发模式：维持固定数量的worker goroutine
func (rc *Controller) runConcurrencyMode(ctx context.Context) {
	defer rc.wg.Done()
	
	if rc.config.Concurrency <= 0 {
		return
	}
	
	// 创建工作队列
	workChan := make(chan string, rc.config.Concurrency*2)
	var workerWg sync.WaitGroup
	
	// 启动固定数量的worker goroutine
	for i := 0; i < rc.config.Concurrency; i++ {
		workerWg.Add(1)
		go func(workerID int) {
			defer workerWg.Done()
			
			w := worker.New(workerID, rc.httpClient, rc.statsCollector, rc.config)
			for {
				select {
				case <-ctx.Done():
					return
				case operation := <-workChan:
					w.ExecuteOperation(operation)
				}
			}
		}(i)
	}
	
	// 持续生成工作项
	go func() {
		for {
			select {
			case <-ctx.Done():
				close(workChan)
				return
			case <-rc.done:
				close(workChan)
				return
			default:
				operation := rc.selectOperationType()
				select {
				case workChan <- operation:
				case <-ctx.Done():
					return
				case <-rc.done:
					return
				}
			}
		}
	}()
	
	// 等待所有worker完成
	workerWg.Wait()
}

// selectOperationType 根据配置的比例选择操作类型
func (rc *Controller) selectOperationType() string {
	return rc.config.GetOperationType(rand.Float64())
}

// IsRunning 检查是否正在运行
func (rc *Controller) IsRunning() bool {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.running
}

// UpdateConfig 更新配置（运行时调整）
func (rc *Controller) UpdateConfig(cfg *config.Config) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	
	// 只更新可以运行时调整的配置
	rc.config.SensorDataRatio = cfg.SensorDataRatio
	rc.config.SensorRWRatio = cfg.SensorRWRatio
	rc.config.BatchRWRatio = cfg.BatchRWRatio
	rc.config.QueryRatio = cfg.QueryRatio
}

// GetCurrentConfig 获取当前配置
func (rc *Controller) GetCurrentConfig() *config.Config {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	
	// 返回配置的副本
	configCopy := *rc.config
	return &configCopy
}