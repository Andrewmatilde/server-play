// Package worker 提供压测工具的工作协程实现
//
// 需求和预设:
// 1. 时序数据API支持: 实现对传感器数据上报、读写操作、批量操作、查询操作的测试
// 2. 真实数据模拟: 模拟3000个工厂的传感器数据，包含设备ID、指标类型、数值、优先级等
// 3. 业务逻辑测试: 支持阈值监控测试(数值>100触发高优先级告警)
// 4. 可配置负载: 根据配置生成不同大小的随机负载数据(512B-20KB)
// 5. 工作队列模式: 从RateController接收工作项，按需执行操作
// 6. 统计推送: 将操作结果推送给StatsCollector进行统计
// 7. 上下文支持: 支持优雅的取消和超时控制
// 8. 错误处理: 区分不同类型的错误，提供详细的错误统计
//
// 设计原则:
// - 每个Worker独立运行，互不影响
// - 模拟真实的工厂传感器数据特征
// - 支持多种时序数据操作类型
// - 异步统计推送，避免影响测试性能
// - 使用真实的API调用，测试完整链路
package worker

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"splay/client"
	"splay/pkg/config"
	"splay/pkg/stats"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// 常量定义
const (
	charset              = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	dataSize             = 64  // 固定数据大小
	queryTriggerInterval = 100 // 每100个读请求触发一次查询验证
)

// 查询计数器，用于每100个读请求触发一次验证
var queryCounter int64

// 对象池
var (
	// 字节切片池
	byteSlicePool = sync.Pool{
		New: func() any {
			return make([]byte, dataSize)
		},
	}

	// 传感器数据上报请求池
	sensorDataRequestPool = sync.Pool{
		New: func() any {
			return &client.UploadSensorDataJSONRequestBody{}
		},
	}

	// 传感器读写请求池
	sensorRWRequestPool = sync.Pool{
		New: func() any {
			return &client.SensorReadWriteJSONRequestBody{}
		},
	}

	// 查询请求池
	queryRequestPool = sync.Pool{
		New: func() any {
			return &client.GetSensorDataJSONRequestBody{}
		},
	}

	// 批量请求切片池
	batchRequestPool = sync.Pool{
		New: func() any {
			return make([]client.SensorReadWriteRequest, 0, 10) // 预分配10个容量
		},
	}
)

// Worker 工作协程
type Worker struct {
	id             int
	client         *client.ClientWithResponses
	statsCollector *stats.Collector
	config         *config.Config
}

func New(id int, client *client.ClientWithResponses, statsCollector *stats.Collector, cfg *config.Config) *Worker {
	return &Worker{
		id:             id,
		client:         client,
		statsCollector: statsCollector,
		config:         cfg,
	}
}

// ExecuteOperation 执行单个操作（用于QPS模式的独立goroutine）
func (w *Worker) ExecuteOperation() {
	w.doSensorDataUpload()
}

// doSensorDataUpload 传感器数据上报
func (w *Worker) doSensorDataUpload() {
	deviceID := w.generateDeviceID()
	metricName := w.generateMetricName()
	value := w.generateValue()
	priority := w.generatePriority()
	data := w.generateRandomData()

	// 从池中获取请求对象
	request := sensorDataRequestPool.Get().(*client.UploadSensorDataJSONRequestBody)
	defer sensorDataRequestPool.Put(request)

	// 立即记录发送事件
	w.statsCollector.PushSentEvent("sensor-data")

	startTime := time.Now()
	// 重用request对象
	request.DeviceId = deviceID
	request.MetricName = client.SensorDataMetricName(metricName)
	request.Value = value
	request.Timestamp = startTime
	request.Priority = &priority
	request.Data = &data

	resp, err := w.client.UploadSensorDataWithResponse(context.Background(), *request)
	latency := time.Since(startTime)

	success := err == nil && resp.StatusCode() == 200
	// 记录完成事件
	w.statsCollector.PushCompletedResult("sensor-data", latency, priority, success)

	// 每100个写入请求后启动goroutine进行查询验证
	if atomic.AddInt64(&queryCounter, 1)%queryTriggerInterval == 0 {
		go w.verifyDataInMySQL(deviceID, metricName, priority)
	}
}

// verifyDataInMySQL 验证MySQL中的数据写入
func (w *Worker) verifyDataInMySQL(deviceID, metricName string, priority int) {
	// 等待3秒让数据写入MySQL
	time.Sleep(3 * time.Second)

	// 连接MySQL数据库
	db, err := sql.Open("mysql", w.config.MySQLDSN)
	if err != nil {
		log.Println("Failed to connect to MySQL:", err)
		w.statsCollector.PushCompletedResult("verify-query", 0, priority, false)
		return
	}
	defer db.Close()

	// 查询刚写入的数据
	queryStart := time.Now()
	query := `SELECT COUNT(*) FROM time_series_data 
		WHERE device_id = ? AND metric_name = ?`

	var count int
	err = db.QueryRow(query, deviceID, metricName).Scan(&count)
	queryLatency := time.Since(queryStart)

	// 记录验证结果
	success := err == nil && count > 0
	w.statsCollector.PushCompletedResult("verify-query", queryLatency, priority, success)
}

// generateDeviceID 生成设备ID
func (w *Worker) generateDeviceID() string {
	factoryID := rand.Intn(3000) + 1 // 工厂ID 1-3000
	deviceID := rand.Intn(w.config.KeyRange) + 1
	return fmt.Sprintf("factory_%03d_device_%08d", factoryID, deviceID)
}

// generateMetricName 生成指标名称
func (w *Worker) generateMetricName() string {
	metrics := []string{
		"temperature", "pressure", "humidity", "vibration",
		"voltage", "current", "power", "flow_rate",
	}
	return metrics[rand.Intn(len(metrics))]
}

// generateValue 生成传感器数值
func (w *Worker) generateValue() float64 {
	// 99% 的概率生成正常值 (0-100)
	// 1% 的概率生成异常值 (100-200)，触发告警
	if rand.Float64() < 0.99 {
		return rand.Float64() * 100
	} else {
		return 100 + rand.Float64()*100
	}
}

// generatePriority 生成优先级
func (w *Worker) generatePriority() int {
	// 根据业务逻辑，值>100时系统会自动提升为高优先级
	// 这里随机生成，让系统自己判断
	priorities := []int{1, 2, 3}
	weights := []float64{0.2, 0.6, 0.2} // 高、中、低优先级的权重

	r := rand.Float64()
	cumulative := 0.0
	for i, weight := range weights {
		cumulative += weight
		if r < cumulative {
			return priorities[i]
		}
	}
	return 2 // 默认中优先级
}

// generateRandomData 生成固定64字节的负载数据
func (w *Worker) generateRandomData() string {
	// 从池中获取字节切片
	b := byteSlicePool.Get().([]byte)
	defer byteSlicePool.Put(b)

	// 生成随机数据
	charId := rand.Intn(len(charset))
	for i := range dataSize {
		b[i] = charset[charId]
		charId = ((charId + 3) / 7 >> 2) % len(charset)
	}
	return string(b)
}
