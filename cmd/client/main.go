package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"splay/client"
	"sync"
	"sync/atomic"
	"time"
)

var (
	serverURL   = flag.String("server", "http://localhost:8080", "服务器地址")
	duration    = flag.Int("duration", 30, "测试持续时间（秒）")
	qps         = flag.Int("qps", 100, "目标QPS（每秒请求数）")
	readRatio   = flag.Float64("read-ratio", 0.5, "读操作比例")
	updateRatio = flag.Float64("update-ratio", 0.3, "更新操作比例")
	keyRange    = flag.Int("key-range", 1000, "键值范围")
)

// 延迟桶定义（毫秒）
var latencyBuckets = []float64{1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000}

type LatencyStats struct {
	buckets    []int64 // 每个桶的计数
	totalCount int64   // 总请求数
	totalTime  int64   // 总延迟时间（纳秒）
	maxLatency int64   // 最大延迟（纳秒）
	minLatency int64   // 最小延迟（纳秒）
}

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
		fmt.Println("延迟统计: 无数据")
		return
	}

	fmt.Printf("延迟统计: 平均=%.2fms, 最小=%.2fms, 最大=%.2fms\n", avgLatency, minLatency, maxLatency)
	fmt.Println("延迟分布:")

	for i, bucket := range latencyBuckets {
		count := buckets[i]
		percentage := float64(count) * 100 / float64(totalCount)
		fmt.Printf("  ≤%.0fms: %d (%.1f%%)\n", bucket, count, percentage)
	}

	// 最后一个桶（>5000ms）
	count := buckets[len(buckets)-1]
	percentage := float64(count) * 100 / float64(totalCount)
	fmt.Printf("  >5000ms: %d (%.1f%%)\n", count, percentage)
}

type Stats struct {
	// 发送统计
	readSent   int64
	insertSent int64
	updateSent int64

	// 完成统计
	readOps      int64
	insertOps    int64
	updateOps    int64
	readErrors   int64
	insertErrors int64
	updateErrors int64

	// 延迟统计
	readLatency   *LatencyStats
	insertLatency *LatencyStats
	updateLatency *LatencyStats

	startTime     time.Time
	lastPrintTime time.Time

	// 上次统计的操作数（用于计算瞬时 QPS）
	lastReadSent   int64
	lastInsertSent int64
	lastUpdateSent int64
	lastReadOps    int64
	lastInsertOps  int64
	lastUpdateOps  int64
}

func NewStats() *Stats {
	now := time.Now()
	return &Stats{
		readLatency:   NewLatencyStats(),
		insertLatency: NewLatencyStats(),
		updateLatency: NewLatencyStats(),
		startTime:     now,
		lastPrintTime: now,
	}
}

func (s *Stats) GetCurrentTotals() (int64, int64, int64, int64, int64, int64, int64) {
	return atomic.LoadInt64(&s.readSent) + atomic.LoadInt64(&s.insertSent) + atomic.LoadInt64(&s.updateSent),
		atomic.LoadInt64(&s.readOps),
		atomic.LoadInt64(&s.insertOps),
		atomic.LoadInt64(&s.updateOps),
		atomic.LoadInt64(&s.readErrors) + atomic.LoadInt64(&s.insertErrors) + atomic.LoadInt64(&s.updateErrors),
		atomic.LoadInt64(&s.readSent) + atomic.LoadInt64(&s.insertSent) + atomic.LoadInt64(&s.updateSent) -
			(atomic.LoadInt64(&s.readOps) + atomic.LoadInt64(&s.insertOps) + atomic.LoadInt64(&s.updateOps) +
				atomic.LoadInt64(&s.readErrors) + atomic.LoadInt64(&s.insertErrors) + atomic.LoadInt64(&s.updateErrors)),
		atomic.LoadInt64(&s.readSent) + atomic.LoadInt64(&s.insertSent) + atomic.LoadInt64(&s.updateSent)
}

func (s *Stats) PrintRealtime() {
	now := time.Now()
	elapsed := now.Sub(s.lastPrintTime).Seconds()
	totalElapsed := now.Sub(s.startTime).Seconds()

	totalSent, readDone, insertDone, updateDone, totalErrors, pending, _ := s.GetCurrentTotals()
	totalDone := readDone + insertDone + updateDone

	// 计算发送速率（瞬时）
	currentReadSent := atomic.LoadInt64(&s.readSent)
	currentInsertSent := atomic.LoadInt64(&s.insertSent)
	currentUpdateSent := atomic.LoadInt64(&s.updateSent)

	instantSendQPS := float64(currentReadSent+currentInsertSent+currentUpdateSent-s.lastReadSent-s.lastInsertSent-s.lastUpdateSent) / elapsed

	// 计算完成速率（瞬时）
	instantDoneQPS := float64(readDone+insertDone+updateDone-s.lastReadOps-s.lastInsertOps-s.lastUpdateOps) / elapsed

	// 计算平均速率
	avgSendQPS := float64(totalSent) / totalElapsed
	avgDoneQPS := float64(totalDone) / totalElapsed

	// 获取延迟统计
	readAvgLatency, _, _, _ := s.readLatency.GetStats()
	insertAvgLatency, _, _, _ := s.insertLatency.GetStats()
	updateAvgLatency, _, _, _ := s.updateLatency.GetStats()

	fmt.Printf("[%.1fs] 发送QPS: %.1f | 完成QPS: %.1f | 平均发送: %.1f | 平均完成: %.1f | 待处理: %d | 错误: %d | 延迟(ms): 读%.1f 插%.1f 更%.1f\n",
		totalElapsed, instantSendQPS, instantDoneQPS, avgSendQPS, avgDoneQPS, pending, totalErrors,
		readAvgLatency, insertAvgLatency, updateAvgLatency)

	// 更新上次统计
	s.lastReadSent = currentReadSent
	s.lastInsertSent = currentInsertSent
	s.lastUpdateSent = currentUpdateSent
	s.lastReadOps = readDone
	s.lastInsertOps = insertDone
	s.lastUpdateOps = updateDone
	s.lastPrintTime = now
}

func (s *Stats) PrintFinal() {
	totalElapsed := time.Since(s.startTime).Seconds()
	totalSent, readDone, insertDone, updateDone, totalErrors, pending, _ := s.GetCurrentTotals()
	totalDone := readDone + insertDone + updateDone

	fmt.Printf("\n=== 最终统计 ===\n")
	fmt.Printf("总运行时间: %.2f 秒\n", totalElapsed)
	fmt.Printf("发送请求数: %d\n", totalSent)
	fmt.Printf("完成请求数: %d\n", totalDone)
	fmt.Printf("  读操作: %d (错误: %d)\n", readDone, atomic.LoadInt64(&s.readErrors))
	fmt.Printf("  插入操作: %d (错误: %d)\n", insertDone, atomic.LoadInt64(&s.insertErrors))
	fmt.Printf("  更新操作: %d (错误: %d)\n", updateDone, atomic.LoadInt64(&s.updateErrors))
	fmt.Printf("待处理请求: %d\n", pending)
	fmt.Printf("总错误数: %d\n", totalErrors)

	if totalElapsed > 0 {
		fmt.Printf("平均发送 QPS: %.2f\n", float64(totalSent)/totalElapsed)
		fmt.Printf("平均完成 QPS: %.2f\n", float64(totalDone)/totalElapsed)
		if totalDone > 0 {
			fmt.Printf("错误率: %.2f%%\n", float64(totalErrors)*100/float64(totalDone+totalErrors))
		}
	}

	fmt.Println("\n=== 延迟分析 ===")
	fmt.Println("读操作:")
	s.readLatency.PrintDistribution()
	fmt.Println("\n插入操作:")
	s.insertLatency.PrintDistribution()
	fmt.Println("\n更新操作:")
	s.updateLatency.PrintDistribution()
}

func main() {
	flag.Parse()

	// 创建客户端
	c, err := client.NewClientWithResponses(*serverURL)
	if err != nil {
		log.Fatalf("创建客户端失败: %v", err)
	}

	fmt.Printf("开始压测，服务器: %s\n", *serverURL)
	fmt.Printf("持续时间: %d秒，目标QPS: %d\n", *duration, *qps)
	fmt.Printf("读比例: %.2f，更新比例: %.2f，插入比例: %.2f\n",
		*readRatio, *updateRatio, 1.0-*readRatio-*updateRatio)

	stats := NewStats()

	// 启动时间控制
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*duration)*time.Second)
	defer cancel()

	// 预先插入一些数据
	fmt.Println("预先插入数据...")
	preInsertData(c, *keyRange/2)

	// 启动实时统计输出
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stats.PrintRealtime()
			}
		}
	}()

	fmt.Println("开始压测...")
	stats.startTime = time.Now() // 重置开始时间
	stats.lastPrintTime = time.Now()

	// 计算请求间隔
	interval := time.Duration(1000000000 / *qps) // 纳秒
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var wg sync.WaitGroup

	// 主发送循环
	for {
		select {
		case <-ctx.Done():
			goto done
		case <-ticker.C:
			// 不再限制并发数，直接发送请求
			wg.Add(1)
			go func() {
				defer wg.Done()

				// 根据比例选择操作类型
				r := rand.Float64()
				if r < *readRatio {
					doRead(c, stats)
				} else if r < *readRatio+*updateRatio {
					doUpdate(c, stats)
				} else {
					doInsert(c, stats)
				}
			}()
		}
	}

done:
	fmt.Println("等待所有请求完成...")

	// 等待所有请求完成，最多等待10秒
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("所有请求已完成")
	case <-time.After(10 * time.Second):
		fmt.Println("等待超时，强制退出")
	}

	// 打印最终统计信息
	stats.PrintFinal()
}

func preInsertData(c *client.ClientWithResponses, count int) {
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("user%08d", i)
		value := generateRandomValue()

		_, err := c.InsertWithResponse(context.Background(), client.InsertJSONRequestBody{
			Key:   key,
			Value: &value,
		})
		if err != nil {
			log.Printf("预插入数据失败 key=%s: %v", key, err)
		}
	}
	fmt.Printf("预插入 %d 条数据完成\n", count)
}

func doRead(c *client.ClientWithResponses, stats *Stats) {
	key := fmt.Sprintf("user%08d", rand.Intn(*keyRange))
	atomic.AddInt64(&stats.readSent, 1)

	startTime := time.Now()
	resp, err := c.ReadWithResponse(context.Background(), &client.ReadParams{
		Key: key,
	})
	latency := time.Since(startTime)

	if err != nil {
		atomic.AddInt64(&stats.readErrors, 1)
		return
	}

	stats.readLatency.Record(latency)

	if resp.StatusCode() == 200 {
		atomic.AddInt64(&stats.readOps, 1)
	} else {
		atomic.AddInt64(&stats.readErrors, 1)
	}
}

func doUpdate(c *client.ClientWithResponses, stats *Stats) {
	key := fmt.Sprintf("user%08d", rand.Intn(*keyRange))
	value := generateRandomValue()
	atomic.AddInt64(&stats.updateSent, 1)

	startTime := time.Now()
	resp, err := c.UpdateWithResponse(context.Background(), client.UpdateJSONRequestBody{
		Key:   key,
		Value: &value,
	})
	latency := time.Since(startTime)

	if err != nil {
		atomic.AddInt64(&stats.updateErrors, 1)
		fmt.Printf("更新失败 key=%s: %v\n", key, err)
		return
	}

	stats.updateLatency.Record(latency)

	if resp.StatusCode() == 204 {
		atomic.AddInt64(&stats.updateOps, 1)
	} else {
		atomic.AddInt64(&stats.updateErrors, 1)
	}
}

func doInsert(c *client.ClientWithResponses, stats *Stats) {
	key := fmt.Sprintf("user%08d", rand.Intn(*keyRange*2)) // 扩大范围避免冲突
	value := generateRandomValue()
	atomic.AddInt64(&stats.insertSent, 1)

	startTime := time.Now()
	resp, err := c.InsertWithResponse(context.Background(), client.InsertJSONRequestBody{
		Key:   key,
		Value: &value,
	})
	latency := time.Since(startTime)

	if err != nil {
		atomic.AddInt64(&stats.insertErrors, 1)
		fmt.Printf("插入失败 key=%s: %v\n", key, err)
		return
	}

	stats.insertLatency.Record(latency)

	if resp.StatusCode() == 204 {
		atomic.AddInt64(&stats.insertOps, 1)
	} else {
		atomic.AddInt64(&stats.insertErrors, 1)
	}
}

func generateRandomValue() string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 100)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
