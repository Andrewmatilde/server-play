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
	"fmt"
	"math/rand"
	"splay/client"
	"splay/pkg/config"
	"splay/pkg/stats"
	"time"
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
func (w *Worker) ExecuteOperation(operation string) {
	switch operation {
	case "sensor-data":
		w.doSensorDataUpload()
	case "sensor-rw":
		w.doSensorReadWrite()
	case "batch-rw":
		w.doBatchSensorRW()
	case "query":
		w.doSensorQuery()
	default:
		// 未知操作类型，记录错误
		w.statsCollector.PushResult(operation, 0, 0, false)
	}
}

// doSensorDataUpload 传感器数据上报
func (w *Worker) doSensorDataUpload() {
	deviceID := w.generateDeviceID()
	metricName := w.generateMetricName()
	value := w.generateValue()
	priority := w.generatePriority()
	data := w.generateRandomData()

	// 立即记录发送事件
	w.statsCollector.PushSentEvent("sensor-data")

	startTime := time.Now()
	resp, err := w.client.UploadSensorDataWithResponse(context.Background(), client.UploadSensorDataJSONRequestBody{
		DeviceId:   deviceID,
		MetricName: client.SensorDataMetricName(metricName),
		Value:      value,
		Timestamp:  startTime,
		Priority:   &priority,
		Data:       &data,
	})
	latency := time.Since(startTime)

	success := err == nil && resp.StatusCode() == 200
	// 记录完成事件
	w.statsCollector.PushCompletedResult("sensor-data", latency, priority, success)
}

// doSensorReadWrite 传感器读写操作
func (w *Worker) doSensorReadWrite() {
	deviceID := w.generateDeviceID()
	metricName := w.generateMetricName()
	value := w.generateValue()
	priority := w.generatePriority()
	data := w.generateRandomData()

	// 立即记录发送事件
	w.statsCollector.PushSentEvent("sensor-rw")

	startTime := time.Now()
	resp, err := w.client.SensorReadWriteWithResponse(context.Background(), client.SensorReadWriteJSONRequestBody{
		DeviceId:   deviceID,
		MetricName: metricName,
		NewValue:   value,
		Timestamp:  startTime,
		Priority:   &priority,
		Data:       &data,
	})
	latency := time.Since(startTime)

	success := err == nil && resp.StatusCode() == 200
	// 记录完成事件
	w.statsCollector.PushCompletedResult("sensor-rw", latency, priority, success)
}

// doBatchSensorRW 批量传感器读写操作
func (w *Worker) doBatchSensorRW() {
	batchSize := rand.Intn(10) + 1 // 批量大小 1-10
	data := make([]client.SensorReadWriteRequest, batchSize)

	for i := 0; i < batchSize; i++ {
		deviceID := w.generateDeviceID()
		metricName := w.generateMetricName()
		value := w.generateValue()
		priority := w.generatePriority()
		randomData := w.generateRandomData()

		data[i] = client.SensorReadWriteRequest{
			DeviceId:   deviceID,
			MetricName: metricName,
			NewValue:   value,
			Timestamp:  time.Now(),
			Priority:   &priority,
			Data:       &randomData,
		}
	}

	// 立即记录发送事件
	w.statsCollector.PushSentEvent("batch-rw")

	startTime := time.Now()
	resp, err := w.client.BatchSensorReadWriteWithResponse(context.Background(), client.BatchSensorReadWriteJSONRequestBody{
		Data: data,
	})
	latency := time.Since(startTime)

	success := err == nil && resp.StatusCode() == 200
	// 记录完成事件
	w.statsCollector.PushCompletedResult("batch-rw", latency, 0, success)
}

// doSensorQuery 传感器数据查询
func (w *Worker) doSensorQuery() {
	deviceID := w.generateDeviceID()
	endTime := time.Now()
	startTime := endTime.Add(-time.Hour) // 查询最近1小时的数据
	limit := rand.Intn(100) + 1          // 限制 1-100 条记录

	request := client.GetSensorDataJSONRequestBody{
		DeviceId:  deviceID,
		StartTime: startTime,
		EndTime:   endTime,
		Limit:     &limit,
	}

	// 随机选择是否指定特定指标
	if rand.Float64() < 0.5 {
		metricName := client.GetSensorDataRequestMetricName(w.generateMetricName())
		request.MetricName = &metricName
	}

	// 立即记录发送事件
	w.statsCollector.PushSentEvent("query")

	reqStartTime := time.Now()
	resp, err := w.client.GetSensorDataWithResponse(context.Background(), request)
	latency := time.Since(reqStartTime)

	success := err == nil && resp.StatusCode() == 200
	// 记录完成事件
	w.statsCollector.PushCompletedResult("query", latency, 0, success)
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

// generateRandomData 生成随机负载数据
func (w *Worker) generateRandomData() string {
	// 根据配置生成指定大小范围的随机数据
	size := rand.Intn(w.config.DataSizeMax-w.config.DataSizeMin+1) + w.config.DataSizeMin

	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, size)
	charId := rand.Intn(len(charset))
	for i := range b {
		b[i] = charset[charId]
		charId = (charId + 3) >> 4 % len(charset)
	}
	return string(b)
}
