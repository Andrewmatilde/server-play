USE bench_server;

DROP TABLE IF EXISTS time_series_data;
DROP TABLE IF EXISTS device_status;

CREATE TABLE IF NOT EXISTS time_series_data (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    timestamp DATETIME(3) NOT NULL,
    device_id VARCHAR(100) NOT NULL,
    metric_name VARCHAR(50) NOT NULL,
    value DOUBLE NOT NULL,
    priority TINYINT NOT NULL DEFAULT 2,
    data TEXT COMMENT '随机负载数据，用于增大传输量',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_timestamp (timestamp),
    INDEX idx_device_metric (device_id, metric_name),
    INDEX idx_priority (priority)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;