package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"monitoring_service/db"
	"monitoring_service/logger"
	"monitoring_service/pubsub"
	"strings"
	"time"
)

// DeviceData sesuai dengan tabel device.data
type DeviceData struct {
	Device_Id    int64 // unit_id
	Tstamp       int64 // epoch time
	Voltage      float64
	Current      float64
	Power        float64
	Energy       float64
	Frequency    float64
	Power_factor float64
}

/* simple=> \d device.data
                                    Table "device.data"
    Column    |       Type       | Collation | Nullable |              Default
--------------+------------------+-----------+----------+-----------------------------------
 id           | bigint           |           | not null |
 unit_id      | bigint           |           | not null |
 timestamp    | bigint           |           | not null | EXTRACT(epoch FROM now())::bigint
 voltage      | double precision |           | not null |
 current      | double precision |           | not null |
 power        | double precision |           | not null |
 energy       | double precision |           | not null |
 frequency    | double precision |           | not null |
 power_factor | double precision |           | not null |
Indexes:
    "data_new_pkey1" PRIMARY KEY, btree (id)
Foreign-key constraints:
    "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE */

// StartRedisToDBWorker menjalankan worker untuk memindahkan data dari Redis ke PostgreSQL secara periodik

func StartRedisToDBWorker(interval time.Duration, memoryLimit int64, stopChan <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Ambil memory limit hanya sekali di awal agar tidak terlalu banyak log

	logger.Info("WORKER", fmt.Sprintf("INFO - Redis Memory Limit: %d bytes", memoryLimit))
	logger.Debug("WORKER", fmt.Sprintf("DEBUG - Worker started with interval: %v", interval))

	for {
		select {
		case <-ticker.C:
			logger.Debug("WORKER", "DEBUG - Checking Redis memory usage...")

			// Cek penggunaan memori Redis
			redisFull, err := isRedisMemoryFull(memoryLimit)
			if err != nil {
				logger.Error("WORKER", "ERROR - Failed to check Redis memory:", err)
				continue
			}

			// Jika Redis penuh, flush ke database segera
			if redisFull {
				logger.Warning("WORKER", "WARNING - Redis memory limit exceeded! Forcing immediate data flush to database.")
			}

			// Jalankan proses penyimpanan data ke database (baik normal maupun jika Redis penuh)
			if err := scanAndSaveRedisBuffer(); err != nil {
				logger.Error("WORKER", "ERROR - scanAndSaveRedisBuffer:", err)
			}

		case <-stopChan:
			logger.Info("WORKER", "INFO - Worker stopped")
			return
		}
	}
}

func isRedisMemoryFull(memoryLimit int64) (bool, error) {
	ctx := context.Background()
	redisClient := pubsub.GetRedisClient()
	if redisClient == nil {
		return false, fmt.Errorf("redis client is not initialized")
	}

	// Ambil informasi penggunaan memori dari Redis
	memInfo, err := redisClient.Info(ctx, "memory").Result()
	if err != nil {
		return false, fmt.Errorf("failed to get Redis memory info: %w", err)
	}

	// Parsing `used_memory` dan `used_memory_human`
	var usedMemory int64
	var usedMemoryHuman string

	for _, line := range strings.Split(memInfo, "\n") {
		if strings.HasPrefix(line, "used_memory:") {
			fmt.Sscanf(line, "used_memory:%d", &usedMemory)
		}
		if strings.HasPrefix(line, "used_memory_human:") {
			usedMemoryHuman = strings.TrimPrefix(line, "used_memory_human:")
		}
	}

	// Log hanya `used_memory_human`
	logger.Info("WORKER", "Info - MEMORY USED N REDIS: "+strings.TrimSpace(usedMemoryHuman))

	return usedMemory > memoryLimit, nil
}

func scanAndSaveRedisBuffer() error {
	ctx := context.Background()
	redisClient := pubsub.GetRedisClient()
	if redisClient == nil {
		return fmt.Errorf("redis client is not initialized")
	}

	data, err := redisClient.LRange(ctx, "buffer:device_data", 0, -1).Result()
	if err != nil {
		return fmt.Errorf("failed to retrieve data from Redis: %w", err)
	}

	if len(data) == 0 {
		logger.Info("WORKER", "INFO - No data found in Redis buffer")
		return nil
	}

	var deviceDataList []DeviceData
	for _, item := range data {
		logger.Debug("WORKER", fmt.Sprintf("DEBUG - Raw JSON: %s", item))
		var deviceData DeviceData
		if err := json.Unmarshal([]byte(item), &deviceData); err != nil {
			logger.Error("WORKER", fmt.Sprintf("ERROR - Failed to parse JSON: %v", err))
			continue
		}
		deviceDataList = append(deviceDataList, deviceData)
	}

	if len(deviceDataList) > 0 {
		dbConn, err := db.GetConnection()
		if err != nil {
			return fmt.Errorf("failed to get DB connection: %w", err)
		}
		defer db.ReleaseConnection()

		query := `INSERT INTO device.data (unit_id, timestamp, voltage, current, power, energy, frequency, power_factor) VALUES `
		values := []interface{}{}

		for i, data := range deviceDataList {
			if i > 0 {
				query += ", "
			}
			query += fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)", i*8+1, i*8+2, i*8+3, i*8+4, i*8+5, i*8+6, i*8+7, i*8+8)
			values = append(values, data.Device_Id, data.Tstamp, data.Voltage, data.Current, data.Power, data.Energy, data.Frequency, data.Power_factor)
		}

		_, err = dbConn.Exec(query, values...)
		if err != nil {
			return fmt.Errorf("failed to execute batch insert: %w", err)
		}
	}

	// Hapus data dari buffer setelah berhasil diproses
	_, err = redisClient.Del(ctx, "buffer:device_data").Result()
	if err != nil {
		logger.Error("WORKER", fmt.Sprintf("ERROR - Failed to delete buffer: %v", err))
		return err
	}

	logger.Info("WORKER", "INFO - Successfully processed and deleted Redis buffer")
	return nil
}
