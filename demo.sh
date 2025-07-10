#!/bin/bash

echo "=== YCSB 性能测试演示 (改进的 QPS 计算) ==="
echo "1. 启动 SQLite 服务器..."

# 清理旧的数据库文件
rm -f test.db

# 启动服务器（后台运行）
./server-test -dsn="test.db" &
SERVER_PID=$!

echo "服务器 PID: $SERVER_PID"
echo "等待服务器启动..."
sleep 2

echo "2. 运行客户端压测（15秒，实时 QPS 监控）..."
echo "   - 每秒显示瞬时 QPS 和平均 QPS"
echo "   - 最后显示详细的性能统计"
echo ""

./client-test -duration=15 -concurrency=8 -read-ratio=0.6 -update-ratio=0.3 -key-range=500

echo ""
echo "3. 停止服务器..."
kill $SERVER_PID

echo ""
echo "=== 测试完成 ==="
echo "SQLite 数据库文件信息:"
ls -la test.db 2>/dev/null || echo "数据库文件不存在" 