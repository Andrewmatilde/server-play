# Header 配置使用说明

## 概述

压测工具支持在统计数据上报时添加自定义 HTTP header，用于身份验证和标识。

## 当前支持的 Header

### 1. Content-Type
- 自动设置为 `application/json; charset=utf-8`

### 2. X-Team-ID
- 从配置文件中的 `report_key` 字段读取
- 用于团队身份标识

### 3. X-Team-Name  
- 从配置文件中的 `report_key` 字段读取
- 用于团队名称标识

## 配置方法

### 1. 在配置文件中设置

```json
{
  "server_url": "http://localhost:8080",
  "duration_seconds": 60,
  "mode": "qps",
  "qps": 100,
  "sensor_data_ratio": 0.4,
  "sensor_rw_ratio": 0.3,
  "batch_rw_ratio": 0.2,
  "query_ratio": 0.1,
  "key_range": 1000,
  "report_interval": 5,
  "mysql_dsn": "",
  "report_url": "http://monitoring-server/api/stats",
  "report_key": "team-alpha-2024"
}
```

### 2. 查看配置结构

```bash
./client -help-config
```

## 实际发送的 Header

当 `report_key` 设置为 `"team-alpha-2024"` 时，实际发送的 HTTP 请求将包含：

```
Content-Type: application/json; charset=utf-8
X-Team-ID: team-alpha-2024
X-Team-Name: team-alpha-2024
```

## 扩展自定义 Header

如果需要添加更多自定义 header，可以修改 `cmd/client/main.go` 文件中的相关代码：

```go
// 在创建请求后添加更多 header
req.Header.Set("X-Custom-Header", "custom-value")
req.Header.Set("Authorization", "Bearer " + token)
req.Header.Set("X-API-Version", "v1.0")
```

## 注意事项

1. **安全性**: `report_key` 会直接作为 header 值发送，请确保使用安全的密钥
2. **唯一性**: 建议使用团队特定的唯一标识符作为 `report_key`
3. **格式**: header 值不应包含特殊字符，建议使用字母、数字和连字符
4. **长度**: header 值不应过长，建议控制在 100 字符以内

## 示例使用场景

### 场景 1: 团队标识
```json
{
  "report_key": "team-alpha-2024"
}
```

### 场景 2: 环境标识
```json
{
  "report_key": "prod-env-team-beta"
}
```

### 场景 3: 测试标识
```json
{
  "report_key": "load-test-2024-01-15"
}
```

## 验证 Header

可以通过以下方式验证 header 是否正确发送：

1. **使用 curl 测试**:
```bash
curl -X POST http://your-server/api/stats \
  -H "Content-Type: application/json; charset=utf-8" \
  -H "X-Team-ID: your-team-key" \
  -H "X-Team-Name: your-team-key" \
  -d '{"test": "data"}'
```

2. **查看服务器日志**: 检查服务器是否接收到正确的 header

3. **使用网络抓包工具**: 如 Wireshark 或浏览器开发者工具 