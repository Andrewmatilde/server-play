package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"splay/client"
	"splay/pkg/config"
	"splay/pkg/ratecontroller"
	"splay/pkg/stats"
	"time"
)

func main() {
	var configFile string
	var helpConfig bool
	flag.StringVar(&configFile, "config", "config.json", "配置文件路径")
	flag.BoolVar(&helpConfig, "help-config", false, "显示配置结构说明")
	flag.Parse()

	// 如果请求显示配置帮助，则显示配置结构并退出
	if helpConfig {
		printConfigHelp()
		return
	}

	// 1. 加载配置
	cfg := config.New()
	if configFile != "" {
		if err := cfg.LoadFromFile(configFile); err != nil {
			fmt.Printf("加载配置文件失败: %v, 使用默认配置\n", err)
		}
	}

	// 验证配置
	if err := cfg.Validate(); err != nil {
		log.Fatalf("配置验证失败: %v", err)
	}

	// 打印配置信息
	cfg.Print()

	// 2. 创建HTTP客户端
	httpClient, err := client.NewClientWithResponses(cfg.ServerURL)
	if err != nil {
		log.Fatalf("创建HTTP客户端失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.GetDuration())
	defer cancel()

	// 3. 创建统计收集器
	statsCollector := stats.NewCollector(ctx)

	// 4. 创建流量控制器
	controller := ratecontroller.New(cfg, statsCollector, httpClient)

	// 6. 启动实时统计输出
	go func() {
		ticker := time.NewTicker(cfg.GetReportInterval())
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				statsCollector.PrintRealtime()
			}
		}
	}()

	fmt.Printf("开始压测，持续时间: %d 秒\n", cfg.Duration)
	fmt.Printf("流量控制模式: %s\n", cfg.Mode)
	if cfg.Mode == "qps" {
		fmt.Printf("目标QPS: %d (每个请求独立goroutine)\n", cfg.QPS)
	} else {
		fmt.Printf("并发数: %d (固定worker协程)\n", cfg.Concurrency)
	}

	// 7. 启动流量控制器
	controller.Start(ctx)

	// 8. 等待测试完成或上下文取消
	<-ctx.Done()

	fmt.Println("\n测试时间到，正在停止...")

	// 10. 等待一段时间让剩余的goroutine完成
	fmt.Println("等待剩余请求完成...")
	time.Sleep(2 * time.Second)

	// 11. 打印最终统计报告
	fmt.Println("\n生成最终统计报告...")
	statsCollector.PrintFinalReport()

	// 12. 生成并上报统计数据
	fmt.Println("\n准备上报统计数据...")
	statsReport := statsCollector.GetStatsReport()

	s, err := json.Marshal(statsReport)
	if err != nil {
		log.Fatalf("Failed to marshal stats report: %v", err)
	}

	fmt.Println("==========上报数据==========\n", string(s))
	fmt.Println("==========上报数据==========")

	// 创建请求
	req, err := http.NewRequest("POST", cfg.ReportURL, bytes.NewBuffer(s))
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}

	// 设置 Content-Type
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	req.Header.Set("X-Team-ID", cfg.ReportKey)
	req.Header.Set("X-Team-Name", cfg.ReportKey)

	// 发送请求
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Failed to report stats: %v", err)
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Server returned error status: %d", resp.StatusCode)
	}

	fmt.Println("上报统计数据成功")

}

// printConfigHelp 显示配置结构说明
func printConfigHelp() {
	fmt.Println("=== 配置结构说明 ===")
	fmt.Println()
	fmt.Println("配置文件使用 JSON 格式，包含以下字段：")
	fmt.Println()
	fmt.Println("服务器配置：")
	fmt.Println("  server_url          string   服务器地址 (默认: http://localhost:8080)")
	fmt.Println("  duration_seconds    int      测试持续时间（秒）(默认: 60)")
	fmt.Println()
	fmt.Println("流量控制配置：")
	fmt.Println("  mode                string   流量控制模式: \"qps\" 或 \"concurrency\" (默认: qps)")
	fmt.Println("  qps                 int      目标QPS（mode=qps时使用）(默认: 100)")
	fmt.Println("  concurrency         int      并发数（mode=concurrency时使用）(默认: 10)")
	fmt.Println()
	fmt.Println("操作比例配置（总和应≤1.0）：")
	fmt.Println("  sensor_data_ratio   float64  传感器数据上报比例 (默认: 0.4)")
	fmt.Println("  sensor_rw_ratio     float64  传感器读写操作比例 (默认: 0.3)")
	fmt.Println("  batch_rw_ratio      float64  批量操作比例 (默认: 0.2)")
	fmt.Println("  query_ratio         float64  查询操作比例 (默认: 0.1)")
	fmt.Println()
	fmt.Println("数据配置：")
	fmt.Println("  key_range           int      设备ID范围 (默认: 1000)")
	fmt.Println("  report_interval     int      实时报告间隔（秒）(默认: 5)")
	fmt.Println()
	fmt.Println("MySQL配置：")
	fmt.Println("  mysql_dsn           string   MySQL数据源名称 (默认: \"\")")
	fmt.Println()
	fmt.Println("上报配置：")
	fmt.Println("  report_url          string   统计数据上报URL (默认: \"\")")
	fmt.Println("  report_key          string   上报认证密钥，用于设置 X-Team-ID 和 X-Team-Name header (默认: \"\")")
	fmt.Println()
	fmt.Println("示例配置文件 (config.json)：")
	fmt.Println(`{
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
  "report_key": "your-team-key"  // 将同时设置 X-Team-ID 和 X-Team-Name header
}`)
	fmt.Println()
	fmt.Println("使用方法：")
	fmt.Println("  ./client -config config.json")
	fmt.Println("  ./client -help-config")
}
