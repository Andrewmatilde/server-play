// Package stats 提供压测工具的统计收集和分析功能
//
// 需求和预设:
// 1. 实时统计收集: Worker通过Push模式主动推送操作结果，避免阻塞
// 2. 延迟分布分析: 提供详细的延迟桶统计，支持P50、P99等百分位数分析
// 3. QPS计算: 实时计算瞬时QPS和平均QPS，便于性能监控
// 4. 多操作类型支持: 分别统计传感器数据上报、读写操作、批量操作、查询操作
// 5. 错误率统计: 记录各类操作的成功率和错误率
// 6. 非阻塞设计: 统计收集不影响Worker的执行性能
// 7. 并发安全: 支持多个Worker并发推送统计数据
// 8. 最终报告: 提供详细的测试总结报告
//
// 设计原则:
// - 使用缓冲channel避免Worker阻塞
// - 原子操作保证并发安全
// - 分离统计收集和处理逻辑
// - 延迟桶设计覆盖常见的响应时间范围
// - 实时输出和最终报告分离
package stats

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// LatencyStats 延迟统计结构
type LatencyStats struct {
	buckets    []int64 // 每个桶的计数
	totalCount int64   // 总请求数
	totalTime  int64   // 总延迟时间（纳秒）
	maxLatency int64   // 最大延迟（纳秒）
	minLatency int64   // 最小延迟（纳秒）
	mu         sync.RWMutex
}

// 延迟桶定义（毫秒）
var latencyBuckets = []float64{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000}

func NewLatencyStats() *LatencyStats {
	return &LatencyStats{
		buckets:    make([]int64, len(latencyBuckets)+1), // +1 for >5000ms
		minLatency: int64(^uint64(0) >> 1),               // 初始化为最大值
	}
}

func (ls *LatencyStats) Record(latency time.Duration) {
	latencyMs := float64(latency.Nanoseconds()) / 1e6

	atomic.AddInt64(&ls.totalCount, 1)
	atomic.AddInt64(&ls.totalTime, latency.Nanoseconds())

	// 更新最大最小延迟
	for {
		current := atomic.LoadInt64(&ls.maxLatency)
		if latency.Nanoseconds() <= current {
			break
		}
		if atomic.CompareAndSwapInt64(&ls.maxLatency, current, latency.Nanoseconds()) {
			break
		}
	}

	for {
		current := atomic.LoadInt64(&ls.minLatency)
		if latency.Nanoseconds() >= current {
			break
		}
		if atomic.CompareAndSwapInt64(&ls.minLatency, current, latency.Nanoseconds()) {
			break
		}
	}

	// 找到对应的桶
	bucketIndex := len(latencyBuckets) // 默认最后一个桶（>5000ms）
	for i, bucket := range latencyBuckets {
		if latencyMs <= bucket {
			bucketIndex = i
			break
		}
	}

	atomic.AddInt64(&ls.buckets[bucketIndex], 1)
}

func (ls *LatencyStats) GetStats() (float64, float64, float64, []int64) {
	totalCount := atomic.LoadInt64(&ls.totalCount)
	if totalCount == 0 {
		return 0, 0, 0, make([]int64, len(ls.buckets))
	}

	totalTime := atomic.LoadInt64(&ls.totalTime)
	maxLatency := atomic.LoadInt64(&ls.maxLatency)
	minLatency := atomic.LoadInt64(&ls.minLatency)

	avgLatency := float64(totalTime) / float64(totalCount) / 1e6 // 转换为毫秒
	maxLatencyMs := float64(maxLatency) / 1e6
	minLatencyMs := float64(minLatency) / 1e6

	buckets := make([]int64, len(ls.buckets))
	for i := range buckets {
		buckets[i] = atomic.LoadInt64(&ls.buckets[i])
	}

	return avgLatency, maxLatencyMs, minLatencyMs, buckets
}

func (ls *LatencyStats) PrintDistribution() {
	avgLatency, maxLatency, minLatency, buckets := ls.GetStats()
	totalCount := atomic.LoadInt64(&ls.totalCount)

	if totalCount == 0 {
		fmt.Println("  无数据")
		return
	}

	fmt.Printf("  平均=%.2fms, 最小=%.2fms, 最大=%.2fms\n", avgLatency, minLatency, maxLatency)
	fmt.Printf("  延迟分布:\n")

	for i, bucket := range latencyBuckets {
		count := buckets[i]
		percentage := float64(count) * 100 / float64(totalCount)
		fmt.Printf("    ≤%.0fms: %d (%.1f%%)\n", bucket, count, percentage)
	}

	// 最后一个桶（>5000ms）
	count := buckets[len(buckets)-1]
	percentage := float64(count) * 100 / float64(totalCount)
	fmt.Printf("    >5000ms: %d (%.1f%%)\n", count, percentage)
}

// Result 单个操作的统计结果
type Result struct {
	Operation string
	Latency   time.Duration
	Success   bool
	Timestamp time.Time
}

// Collector 统计收集器
type Collector struct {
	// 各操作的延迟统计
	sensorDataStats *LatencyStats
	sensorRWStats   *LatencyStats
	batchRWStats    *LatencyStats
	queryStats      *LatencyStats

	// 操作计数
	sensorDataSent   int64
	sensorRWSent     int64
	batchRWSent      int64
	querySent        int64
	sensorDataOps    int64
	sensorRWOps      int64
	batchRWOps       int64
	queryOps         int64
	sensorDataErrors int64
	sensorRWErrors   int64
	batchRWErrors    int64
	queryErrors      int64

	// 时间统计
	startTime     time.Time
	lastPrintTime time.Time

	// 上次统计的操作数（用于计算瞬时 QPS）
	lastSensorDataSent int64
	lastSensorRWSent   int64
	lastBatchRWSent    int64
	lastQuerySent      int64
	lastSensorDataOps  int64
	lastSensorRWOps    int64
	lastBatchRWOps     int64
	lastQueryOps       int64

	// 用于推送统计结果的通道
	resultChan chan Result
	done       chan struct{}
	mu         sync.RWMutex
}

func NewCollector() *Collector {
	now := time.Now()
	sc := &Collector{
		sensorDataStats: NewLatencyStats(),
		sensorRWStats:   NewLatencyStats(),
		batchRWStats:    NewLatencyStats(),
		queryStats:      NewLatencyStats(),
		startTime:       now,
		lastPrintTime:   now,
		resultChan:      make(chan Result, 10000), // 缓冲通道
		done:            make(chan struct{}),
	}

	// 启动统计处理协程
	go sc.processResults()

	return sc
}

// PushResult 推送操作结果
func (sc *Collector) PushResult(operation string, latency time.Duration, success bool) {
	select {
	case sc.resultChan <- Result{
		Operation: operation,
		Latency:   latency,
		Success:   success,
		Timestamp: time.Now(),
	}:
	default:
		// 如果通道满了，丢弃该统计结果
		// 这样可以避免阻塞 Worker
	}
}

// processResults 处理统计结果
func (sc *Collector) processResults() {
	for {
		select {
		case result := <-sc.resultChan:
			sc.processResult(result)
		case <-sc.done:
			// 处理剩余的结果
			for {
				select {
				case result := <-sc.resultChan:
					sc.processResult(result)
				default:
					return
				}
			}
		}
	}
}

func (sc *Collector) processResult(result Result) {
	switch result.Operation {
	case "sensor-data":
		atomic.AddInt64(&sc.sensorDataSent, 1)
		if result.Success {
			atomic.AddInt64(&sc.sensorDataOps, 1)
			sc.sensorDataStats.Record(result.Latency)
		} else {
			atomic.AddInt64(&sc.sensorDataErrors, 1)
		}
	case "sensor-rw":
		atomic.AddInt64(&sc.sensorRWSent, 1)
		if result.Success {
			atomic.AddInt64(&sc.sensorRWOps, 1)
			sc.sensorRWStats.Record(result.Latency)
		} else {
			atomic.AddInt64(&sc.sensorRWErrors, 1)
		}
	case "batch-rw":
		atomic.AddInt64(&sc.batchRWSent, 1)
		if result.Success {
			atomic.AddInt64(&sc.batchRWOps, 1)
			sc.batchRWStats.Record(result.Latency)
		} else {
			atomic.AddInt64(&sc.batchRWErrors, 1)
		}
	case "query":
		atomic.AddInt64(&sc.querySent, 1)
		if result.Success {
			atomic.AddInt64(&sc.queryOps, 1)
			sc.queryStats.Record(result.Latency)
		} else {
			atomic.AddInt64(&sc.queryErrors, 1)
		}
	}
}

func (sc *Collector) GetCurrentTotals() (int64, int64, int64, int64) {
	totalSent := atomic.LoadInt64(&sc.sensorDataSent) + atomic.LoadInt64(&sc.sensorRWSent) +
		atomic.LoadInt64(&sc.batchRWSent) + atomic.LoadInt64(&sc.querySent)
	totalOps := atomic.LoadInt64(&sc.sensorDataOps) + atomic.LoadInt64(&sc.sensorRWOps) +
		atomic.LoadInt64(&sc.batchRWOps) + atomic.LoadInt64(&sc.queryOps)
	totalErrors := atomic.LoadInt64(&sc.sensorDataErrors) + atomic.LoadInt64(&sc.sensorRWErrors) +
		atomic.LoadInt64(&sc.batchRWErrors) + atomic.LoadInt64(&sc.queryErrors)
	pending := totalSent - totalOps - totalErrors

	return totalSent, totalOps, totalErrors, pending
}

func (sc *Collector) PrintRealtime() {
	now := time.Now()
	elapsed := now.Sub(sc.lastPrintTime).Seconds()
	totalElapsed := now.Sub(sc.startTime).Seconds()

	totalSent, totalOps, totalErrors, pending := sc.GetCurrentTotals()

	// 计算瞬时发送速率
	currentSensorDataSent := atomic.LoadInt64(&sc.sensorDataSent)
	currentSensorRWSent := atomic.LoadInt64(&sc.sensorRWSent)
	currentBatchRWSent := atomic.LoadInt64(&sc.batchRWSent)
	currentQuerySent := atomic.LoadInt64(&sc.querySent)

	instantSendQPS := float64(currentSensorDataSent+currentSensorRWSent+currentBatchRWSent+currentQuerySent-
		sc.lastSensorDataSent-sc.lastSensorRWSent-sc.lastBatchRWSent-sc.lastQuerySent) / elapsed

	// 计算瞬时完成速率
	currentSensorDataOps := atomic.LoadInt64(&sc.sensorDataOps)
	currentSensorRWOps := atomic.LoadInt64(&sc.sensorRWOps)
	currentBatchRWOps := atomic.LoadInt64(&sc.batchRWOps)
	currentQueryOps := atomic.LoadInt64(&sc.queryOps)

	instantDoneQPS := float64(currentSensorDataOps+currentSensorRWOps+currentBatchRWOps+currentQueryOps-
		sc.lastSensorDataOps-sc.lastSensorRWOps-sc.lastBatchRWOps-sc.lastQueryOps) / elapsed

	// 计算平均速率
	avgSendQPS := float64(totalSent) / totalElapsed
	avgDoneQPS := float64(totalOps) / totalElapsed

	// 获取延迟统计
	sensorDataAvgLatency, _, _, _ := sc.sensorDataStats.GetStats()
	sensorRWAvgLatency, _, _, _ := sc.sensorRWStats.GetStats()
	batchRWAvgLatency, _, _, _ := sc.batchRWStats.GetStats()
	queryAvgLatency, _, _, _ := sc.queryStats.GetStats()

	fmt.Printf("[%.1fs] 发送QPS: %.1f | 完成QPS: %.1f | 平均发送: %.1f | 平均完成: %.1f | 待处理: %d | 错误: %d\n",
		totalElapsed, instantSendQPS, instantDoneQPS, avgSendQPS, avgDoneQPS, pending, totalErrors)
	fmt.Printf("       延迟(ms): 上报%.1f 读写%.1f 批量%.1f 查询%.1f\n",
		sensorDataAvgLatency, sensorRWAvgLatency, batchRWAvgLatency, queryAvgLatency)

	// 更新上次统计
	sc.lastSensorDataSent = currentSensorDataSent
	sc.lastSensorRWSent = currentSensorRWSent
	sc.lastBatchRWSent = currentBatchRWSent
	sc.lastQuerySent = currentQuerySent
	sc.lastSensorDataOps = currentSensorDataOps
	sc.lastSensorRWOps = currentSensorRWOps
	sc.lastBatchRWOps = currentBatchRWOps
	sc.lastQueryOps = currentQueryOps
	sc.lastPrintTime = now
}

func (sc *Collector) PrintFinalReport() {
	// 停止统计处理
	close(sc.done)

	// 等待一小段时间确保所有统计结果都被处理
	time.Sleep(100 * time.Millisecond)

	totalElapsed := time.Since(sc.startTime).Seconds()
	totalSent, totalOps, totalErrors, pending := sc.GetCurrentTotals()

	fmt.Printf("\n=== 最终统计报告 ===\n")
	fmt.Printf("总运行时间: %.2f 秒\n", totalElapsed)
	fmt.Printf("发送请求数: %d\n", totalSent)
	fmt.Printf("完成请求数: %d\n", totalOps)
	fmt.Printf("  传感器数据上报: %d (错误: %d)\n", atomic.LoadInt64(&sc.sensorDataOps), atomic.LoadInt64(&sc.sensorDataErrors))
	fmt.Printf("  传感器读写操作: %d (错误: %d)\n", atomic.LoadInt64(&sc.sensorRWOps), atomic.LoadInt64(&sc.sensorRWErrors))
	fmt.Printf("  批量操作: %d (错误: %d)\n", atomic.LoadInt64(&sc.batchRWOps), atomic.LoadInt64(&sc.batchRWErrors))
	fmt.Printf("  查询操作: %d (错误: %d)\n", atomic.LoadInt64(&sc.queryOps), atomic.LoadInt64(&sc.queryErrors))
	fmt.Printf("待处理请求: %d\n", pending)
	fmt.Printf("总错误数: %d\n", totalErrors)

	if totalElapsed > 0 {
		fmt.Printf("平均发送 QPS: %.2f\n", float64(totalSent)/totalElapsed)
		fmt.Printf("平均完成 QPS: %.2f\n", float64(totalOps)/totalElapsed)
		if totalOps+totalErrors > 0 {
			fmt.Printf("错误率: %.2f%%\n", float64(totalErrors)*100/float64(totalOps+totalErrors))
		}
	}

	fmt.Println("\n=== 延迟分析 ===")
	fmt.Println("传感器数据上报:")
	sc.sensorDataStats.PrintDistribution()
	fmt.Println("\n传感器读写操作:")
	sc.sensorRWStats.PrintDistribution()
	fmt.Println("\n批量操作:")
	sc.batchRWStats.PrintDistribution()
	fmt.Println("\n查询操作:")
	sc.queryStats.PrintDistribution()
}