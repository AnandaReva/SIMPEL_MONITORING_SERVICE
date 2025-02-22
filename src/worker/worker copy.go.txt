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

/*
simple=> \d device.unit ;
Table "device.unit"
Column      |          Type          | Collation | Nullable |              Default

-----------------+------------------------+-----------+----------+-----------------------------------

id              | bigint                 |           | not null | nextval('device_id_sq'::regclass)
name            | character varying(255) |           | not null |
status          | integer                |           | not null |
salt            | character varying(64)  |           | not null |
salted_password | character varying(128) |           | not null |
data            | jsonb                  |           | not null |
create_tstamp   | bigint                 |           |          | EXTRACT(epoch FROM now())::bigint

Indexes:

"unit_pkey" PRIMARY KEY, btree (id)

Referenced by:

TABLE "device.data" CONSTRAINT "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE

\d: extra argument ";" ignored
simple=> \d device.data ;

	Table "device.data"
									   Column    |       Type       | Collation | Nullable |                 Default

									   --------------+------------------+-----------+----------+-----------------------------------------

									   id           | bigint           |           | not null | nextval('device.data_id_seq'::regclass)
									   unit_id      | bigint           |           | not null |
									   tstamp       | bigint           |           | not null | EXTRACT(epoch FROM now())::bigint
									   voltage      | double precision |           | not null |
									   current      | double precision |           | not null |
	power        | double precision |           | not null |
	energy       | double precision |           | not null |
	frequency    | double precision |           | not null |
	power_factor | double precision |           | not null |

	Indexes:

	"data_new_pkey1" PRIMARY KEY, btree (id)

	Foreign-key constraints:

	"fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE

	\d: extra argument ";" ignored
	simple=>
*/
// StartRedisToDBWorker menjalankan worker untuk memindahkan data dari Redis ke PostgreSQL secara periodik

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

func StartRedisToDBWorker(interval time.Duration, memoryLimit int64, stopChan <-chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Info("WORKER", fmt.Sprintf("Worker started with interval: %v and memory limit: %d bytes", interval, memoryLimit))

	for {
		select {
		case <-ticker.C:
			logger.Debug("WORKER", "Checking Redis memory usage...")

			redisFull, err := isRedisMemoryFull(memoryLimit)
			if err != nil {
				logger.Error("WORKER", "Failed to check Redis memory:", err)
				continue
			}

			if redisFull {
				logger.Warning("WORKER", "Redis memory limit exceeded! Forcing immediate data flush to database.")
			}

			data, err := fetchRedisBuffer()
			if err != nil {
				logger.Error("WORKER", "Failed to get buffer data from Redis.")
				continue
			}

			if len(data) > 0 {
				if err := saveDataToDB(data); err != nil {
					logger.Error("WORKER", "ERROR - Failed to save data to DB: ", err)
					filteredData, err := validateDeviceData(data)
					if err == nil {
						if err := saveDataToDB(filteredData); err != nil {
							logger.Error("WORKER", "Retry failed, clearing Redis buffer")
							clearRedisBuffer()
						}
					} else {

						logger.Error("WORKER", "Failed validateDeviceData: ", err)
					}
				}
			}

		case <-stopChan:
			logger.Info("WORKER", "Worker stopped")
			return
		}
	}
}

func fetchRedisBuffer() ([]DeviceData, error) {
	ctx := context.Background()
	redisClient := pubsub.GetRedisClient()
	if redisClient == nil {
		return nil, fmt.Errorf("redis client is not initialized")
	}
	data, err := redisClient.LRange(ctx, "buffer:device_data", 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve data from Redis: %w", err)
	}

	var parsedData []DeviceData
	for _, item := range data {
		var deviceData DeviceData
		if err := json.Unmarshal([]byte(item), &deviceData); err != nil {
			logger.Error("WORKER", fmt.Sprintf("ERROR - Invalid JSON format: %v", err))
			continue
		}
		parsedData = append(parsedData, deviceData)
	}

	return parsedData, nil
}

func validateDeviceData(data []DeviceData) ([]DeviceData, error) {
	if len(data) == 0 {
		return nil, nil
	}

	dbConn, err := db.GetConnection()
	if err != nil {
		return nil, fmt.Errorf("failed to get DB connection: %w", err)
	}

	validUnitIDs := make(map[int64]bool)
	rows, err := dbConn.Query("SELECT id FROM device.unit")
	if err != nil {
		return nil, fmt.Errorf("failed to get valid unit IDs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var unitID int64
		if err := rows.Scan(&unitID); err != nil {
			return nil, fmt.Errorf("failed to scan unit ID: %w", err)
		}
		validUnitIDs[unitID] = true
	}

	var filteredData []DeviceData
	for _, deviceData := range data {
		if validUnitIDs[deviceData.Device_Id] {
			filteredData = append(filteredData, deviceData)
		}
	}

	return filteredData, nil
}

func saveDataToDB(data []DeviceData) error {
	if len(data) == 0 {
		clearRedisBuffer()
		return nil
	}

	dbConn, err := db.GetConnection()
	if err != nil {
		return fmt.Errorf("failed to get DB connection: %w", err)
	}

	query := `INSERT INTO device.data (unit_id, tstamp, voltage, current, power, energy, frequency, power_factor) VALUES `
	values := []interface{}{}

	for i, d := range data {
		if i > 0 {
			query += ", "
		}
		query += fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)", i*8+1, i*8+2, i*8+3, i*8+4, i*8+5, i*8+6, i*8+7, i*8+8)
		values = append(values, d.Device_Id, d.Tstamp, d.Voltage, d.Current, d.Power, d.Energy, d.Frequency, d.Power_factor)
	}

	_, err = dbConn.Exec(query, values...)
	if err != nil {
		return fmt.Errorf("failed to execute batch insert: %w", err)
	}

	clearRedisBuffer()
	logger.Info("WORKER", fmt.Sprintf("Successfully inserted %d records and cleaned Redis buffer", len(data)))
	return nil
}

func clearRedisBuffer() {
	ctx := context.Background()
	redisClient := pubsub.GetRedisClient()
	redisClient.Del(ctx, "buffer:device_data")
}
