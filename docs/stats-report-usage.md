# 统计数据上报使用指南

## 概述

本文档说明如何使用 `statsCollector.GetStatsReport()` 方法来获取压测统计数据并进行上报。

## 数据结构说明

`model.StatsReport` 包含以下主要部分：

### 1. 基本统计信息
- `TotalElapsed`: 总运行时间（秒）
- `TotalSent`: 发送的请求总数
- `TotalOps`: 完成的请求总数  
- `TotalErrors`: 错误总数
- `Pending`: 待处理的请求数
- `TotalSaveDelayErrors`: 因落盘超时产生的错误数

### 2. 性能指标 (PerformanceMetrics)
- `AvgSentQPS`: 平均发送 QPS
- `AvgCompletedQPS`: 平均完成 QPS
- `ErrorRate`: 错误率（百分比）

### 3. 操作统计 (Operations)
每种操作类型都包含：
- `Operations`: 成功完成的操作数
- `Errors`: 错误数

支持的操作类型：
- `SensorData`: 传感器数据上报
- `SensorRW`: 传感器读写操作
- `BatchRW`: 批量操作
- `Query`: 查询操作

### 4. 延迟分析 (LatencyAnalysis)
每种操作类型都有延迟分布统计：
- `Avg`, `Min`, `Max`: 平均、最小、最大延迟（毫秒）
- `Buckets`: 延迟分布桶，对应 [1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000]ms 及 >5000ms
- 高优先级请求的对应统计（如果有）

### 5. 高优先级请求统计 (HighPriorityStats)
当存在优先级≥3的请求时，会包含：
- `TotalCount`: 高优先级请求总数
- `Percentage`: 占总请求的百分比
- 各操作类型的高优先级请求数

## 使用方法

### 1. 获取统计报告

```go
// 在压测结束后获取统计报告
statsReport := statsCollector.GetStatsReport()
```

### 2. 将报告转换为 JSON

```go
jsonData, err := json.Marshal(statsReport)
if err != nil {
    log.Printf("序列化失败: %v", err)
    return
}
```

### 3. 上报到监控服务器

```go
func reportToMonitoringServer(report *model.StatsReport) error {
    jsonData, err := json.Marshal(report)
    if err != nil {
        return fmt.Errorf("序列化失败: %w", err)
    }
    
    resp, err := http.Post(
        "http://monitoring-server/api/stats", 
        "application/json", 
        bytes.NewBuffer(jsonData),
    )
    if err != nil {
        return fmt.Errorf("发送请求失败: %w", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("服务器返回错误状态码: %d", resp.StatusCode)
    }
    
    return nil
}
```

## 完整示例

```go
package main

import (
    "bytes"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "splay/model"
    "splay/pkg/stats"
)

func uploadStatsReport(statsCollector *stats.Collector, serverURL string) error {
    // 1. 获取统计报告
    report := statsCollector.GetStatsReport()
    
    // 2. 验证数据（可选）
    if report.TotalOps == 0 {
        return fmt.Errorf("没有完成的操作，跳过上报")
    }
    
    // 3. 序列化为 JSON
    jsonData, err := json.Marshal(report)
    if err != nil {
        return fmt.Errorf("序列化失败: %w", err)
    }
    
    // 4. 创建 HTTP 请求
    req, err := http.NewRequest("POST", serverURL, bytes.NewBuffer(jsonData))
    if err != nil {
        return fmt.Errorf("创建请求失败: %w", err)
    }
    
    req.Header.Set("Content-Type", "application/json")
    
    // 5. 发送请求
    client := &http.Client{
        Timeout: 30 * time.Second,
    }
    
    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("发送请求失败: %w", err)
    }
    defer resp.Body.Close()
    
    // 6. 检查响应
    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("服务器返回错误: %d, %s", resp.StatusCode, string(body))
    }
    
    log.Println("统计数据上报成功")
    return nil
}
```

## 数据示例

以下是一个实际的统计报告 JSON 示例：

```json
{
  "highPriorityStats": {
    "batchRWCount": 150,
    "percentage": 15.5,
    "queryCount": 200,
    "sensorDataCount": 100,
    "sensorRWCount": 50,
    "totalCount": 500
  },
  "latencyAnalysis": {
    "sensorData": {
      "avg": 15.5,
      "buckets": [10, 20, 50, 100, 150, 80, 40, 20, 10, 5, 2, 1, 12],
      "highPriorityAvg": 12.3,
      "highPriorityBuckets": [5, 10, 25, 30, 20, 10, 0, 0, 0, 0, 0, 0, 0],
      "highPriorityCount": 100,
      "highPriorityMax": 45.2,
      "highPriorityMin": 2.1,
      "max": 6523.5,
      "min": 1.2
    }
  },
  "operations": {
    "batchRW": {
      "errors": 5,
      "operations": 995
    },
    "query": {
      "errors": 10,
      "operations": 1990
    },
    "sensorData": {
      "errors": 2,
      "operations": 498
    },
    "sensorRW": {
      "errors": 3,
      "operations": 297
    }
  },
  "pending": 15,
  "performanceMetrics": {
    "avgCompletedQPS": 2500.5,
    "avgSentQPS": 2550.8,
    "errorRate": 0.5
  },
  "totalElapsed": 60.5,
  "totalErrors": 20,
  "totalOps": 3780,
  "totalSaveDelayErrors": 0,
  "totalSent": 3815
}
```

## 注意事项

1. **调用时机**: 应在压测完成后、统计收集器停止前调用 `GetStatsReport()`
2. **线程安全**: `GetStatsReport()` 方法是线程安全的，可以在任何时候调用
3. **性能影响**: 调用该方法的开销很小，不会影响正在进行的压测
4. **错误处理**: 始终检查上报过程中的错误，避免数据丢失
5. **重试机制**: 建议实现重试机制，以应对网络故障等临时问题

## 扩展建议

1. **批量上报**: 如果需要定期上报，可以收集多个时间点的报告进行批量上报
2. **压缩传输**: 对于大量数据，可以考虑使用 gzip 压缩
3. **认证机制**: 在生产环境中，应添加适当的认证机制（如 API Key）
4. **监控告警**: 基于上报的数据设置监控告警阈值 