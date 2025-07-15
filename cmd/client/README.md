# 时序数据存储系统压测工具

这是一个专门为时序数据存储系统设计的压力测试工具，支持对传感器数据 API 进行全面的性能测试。

## 功能特性

- **多种操作支持**: 传感器数据上报、读写操作、批量操作、数据查询
- **精确的流量控制**: 
  - **QPS 模式**: 按固定速率创建独立的 goroutine 执行每个请求
  - **并发模式**: 维持固定数量的长期运行 worker goroutine
- **实时统计监控**: 延迟分布、QPS、错误率等详细指标
- **配置文件驱动**: 使用 JSON 配置文件，避免复杂的命令行参数
- **模块化设计**: 清晰的包结构，易于维护和扩展

## 目录结构

```
pkg/
├── config/       # 配置管理模块
├── stats/        # 统计收集模块  
├── worker/       # 工作协程模块
└── ratecontroller/ # 流量控制模块

cmd/client/
├── main.go       # 主程序入口
├── config.json   # 配置文件
└── bench-client  # 编译后的可执行文件
```

## 编译

```bash
cd cmd/client
go build -o bench-client
```

## 使用方法

### 1. 配置文件使用

```bash
# 使用默认配置文件 config.json
./bench-client

# 使用自定义配置文件
./bench-client -config my-config.json
```

### 2. 配置文件格式

```json
{
  "server_url": "http://localhost:8080",
  "duration_seconds": 60,
  "mode": "qps",
  "qps": 1000,
  "concurrency": 50,
  "sensor_data_ratio": 0.6,
  "sensor_rw_ratio": 0.25,
  "batch_rw_ratio": 0.1,
  "query_ratio": 0.05,
  "key_range": 10000,
  "data_size_min": 512,
  "data_size_max": 4096,
  "report_interval": 1
}
```

### 3. 配置参数说明

| 参数 | 说明 | 默认值 |
|------|------|---------|
| server_url | 目标服务器地址 | http://localhost:8080 |
| duration_seconds | 测试持续时间(秒) | 30 |
| mode | 流量控制模式 (qps/concurrency) | qps |
| qps | 目标QPS值 (QPS模式) | 100 |
| concurrency | 并发协程数 (并发模式) | 10 |
| sensor_data_ratio | 传感器数据上报比例 | 0.7 |
| sensor_rw_ratio | 传感器读写操作比例 | 0.2 |
| batch_rw_ratio | 批量操作比例 | 0.05 |
| query_ratio | 查询操作比例 | 0.05 |
| key_range | 设备ID范围 | 1000 |
| data_size_min | 最小数据大小(字节) | 512 |
| data_size_max | 最大数据大小(字节) | 2048 |
| report_interval | 报告间隔(秒) | 1 |

## 流量控制模式详解

### QPS 模式 (mode: "qps")
- **原理**: 按照固定的 QPS 速率创建独立的 goroutine 执行每个请求
- **特点**: 
  - 每个请求都在独立的 goroutine 中执行
  - 请求之间完全独立，不会相互影响
  - 适合测试服务器的最大吞吐能力
  - 可以模拟真实的高并发场景
- **配置**: 设置 `qps` 参数控制请求发送速率

### 并发模式 (mode: "concurrency")  
- **原理**: 维持固定数量的长期运行 worker goroutine
- **特点**:
  - 固定数量的 worker goroutine 持续执行请求
  - Worker 之间共享工作队列
  - 适合测试固定并发数下的系统表现
  - 更节省系统资源
- **配置**: 设置 `concurrency` 参数控制并发协程数量

## 测试 API

工具会测试以下 API 端点：

1. **POST /api/sensor-data** - 传感器数据上报
2. **POST /api/sensor-rw** - 传感器读写操作 
3. **POST /api/batch-sensor-rw** - 批量操作
4. **POST /api/get-sensor-data** - 数据查询

## 输出示例

### 实时输出
```
[5.1s] 发送QPS: 998.2 | 完成QPS: 995.1 | 平均发送: 1000.0 | 平均完成: 997.3 | 待处理: 15 | 错误: 2
       延迟(ms): 上报12.3 读写15.6 批量45.2 查询8.9
```

### 最终报告
```
=== 最终统计报告 ===
总运行时间: 60.02 秒
发送请求数: 60000
完成请求数: 59850
  传感器数据上报: 35910 (错误: 5)
  传感器读写操作: 14963 (错误: 8)
  批量操作: 5985 (错误: 2)
  查询操作: 2992 (错误: 1)
待处理请求: 134
总错误数: 16
平均发送 QPS: 999.67
平均完成 QPS: 997.50
错误率: 0.03%

=== 延迟分析 ===
传感器数据上报:
  平均=12.34ms, 最小=2.10ms, 最大=156.78ms
  延迟分布:
    ≤10ms: 25430 (70.8%)
    ≤20ms: 8973 (25.0%)
    ≤50ms: 1398 (3.9%)
    ≤100ms: 109 (0.3%)
    >100ms: 0 (0.0%)
```

## 性能目标

根据需求文档，系统应达到以下性能指标：

- **QPS**: ≥ 10,000 requests/second
- **延迟**: P99 ≤ 500ms  
- **错误率**: ≤ 1%
- **持久化延迟**: ≤ 1s
- **数据丢失率**: ≤ 0.5%

## 注意事项

1. 确保目标服务器已启动并可访问
2. 根据服务器性能调整 QPS 和并发数
3. 监控服务器资源使用情况
4. 操作比例总和应为 1.0 或更小
5. 数据大小范围会影响网络带宽使用

## 扩展说明

该工具采用模块化设计，易于扩展：

- **config**: 配置管理，支持 JSON 格式
- **stats**: 统计收集，支持实时和最终报告
- **worker**: 工作协程，执行具体的 API 调用
- **ratecontroller**: 流量控制，支持多种模式

如需添加新的 API 测试或统计指标，只需修改对应的模块即可。