package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"splay/client"
	"splay/pkg/config"
	"splay/pkg/ratecontroller"
	"splay/pkg/stats"
	"time"
)

func main() {
	var configFile string
	flag.StringVar(&configFile, "config", "config.json", "配置文件路径")
	flag.Parse()

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
}
