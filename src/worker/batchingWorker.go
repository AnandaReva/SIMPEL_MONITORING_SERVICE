package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/pubsub"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// DeviceData sesuai dengan tabel device.data
type DeviceData struct {
	Device_Id    int64 // unit_id
	Timestamp    time.Time
	Voltage      float64
	Current      float64
	Power        float64
	Energy       float64
	Frequency    float64
	Power_factor float64
}

/*
simpel=> \d device.unit
                                         Table "device.unit"
     Column      |          Type          | Collation | Nullable |              Default
-----------------+------------------------+-----------+----------+-----------------------------------
 id              | bigint                 |           | not null | nextval('device_id_sq'::regclass)
 name            | character varying(255) |           | not null |
 st              | integer                |           | not null |
 data            | jsonb                  |           | not null |
 create_tstamp   | bigint                 |           |          | EXTRACT(epoch FROM now())::bigint
 last_tstamp     | bigint                 |           |          | EXTRACT(epoch FROM now())::bigint
 image           | bigint                 |           |          |
 read_interval   | integer                |           | not null |
 salted_password | character varying(128) |           | not null |
 salt            | character varying(32)  |           | not null |
Indexes:
    "unit_pkey" PRIMARY KEY, btree (id)
    "idx_device_name" btree (name)
Foreign-key constraints:
    "fk_attachment" FOREIGN KEY (image) REFERENCES sysfile.file(id) ON DELETE SET NULL
Referenced by:
    TABLE "_timescaledb_internal._hyper_5_38_chunk" CONSTRAINT "38_38_fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
    TABLE "device.data" CONSTRAINT "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
    TABLE "device.device_activity" CONSTRAINT "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE


simpel=> \d device.data
                                             Table "device.data"
    Column    |            Type             | Collation | Nullable |                 Default
--------------+-----------------------------+-----------+----------+------------------------------------------
 id           | bigint                      |           | not null | nextval('device.data2_id_seq'::regclass)
 unit_id      | bigint                      |           | not null |
 timestamp    | timestamp without time zone |           | not null | now()
 voltage      | double precision            |           | not null |
 current      | double precision            |           | not null |
 power        | double precision            |           | not null |
 energy       | double precision            |           | not null |
 frequency    | double precision            |           | not null |
 power_factor | double precision            |           | not null |
Indexes:
    "data_tstamp_idx" btree ("timestamp" DESC)
    "data_unique_idx" UNIQUE, btree (id, "timestamp")
Foreign-key constraints:
    "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
Triggers:
    ts_insert_blocker BEFORE INSERT ON device.data FOR EACH ROW EXECUTE FUNCTION _timescaledb_functions.insert_blocker()

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

func StartRedisToDBWorker(interval time.Duration, memoryLimit int64, stopChan <-chan struct{}, dbConn *sqlx.DB) {
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

			data, err := fetchRedisBufferDeviceData()
			if err != nil {
				logger.Error("WORKER", "Failed to get buffer data from Redis.")
				continue
			}

			if len(data) > 0 {
				if err := dumpDataToDB(data, dbConn); err != nil {
					logger.Error("WORKER", "ERROR - Failed to save data to DB: ", err)
					filteredData, err := validateDeviceData(data, dbConn)
					if err == nil {
						if err := dumpDataToDB(filteredData, dbConn); err != nil {
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

func fetchRedisBufferDeviceData() ([]DeviceData, error) {
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
	var invalidItems []string // Menyimpan item invalid untuk dihapus sekaligus

	for _, item := range data {
		var deviceData struct {
			Device_Id    int64   `json:"unit_id"`
			Timestamp    string  `json:"timestamp"`
			Voltage      float64 `json:"voltage"`
			Current      float64 `json:"current"`
			Power        float64 `json:"power"`
			Energy       float64 `json:"energy"`
			Frequency    float64 `json:"frequency"`
			Power_factor float64 `json:"power_factor"`
		}

		// Coba decode JSON
		if err := json.Unmarshal([]byte(item), &deviceData); err != nil {
			logger.Error("WORKER", fmt.Sprintf("ERROR - Invalid JSON format: %v - DATA: %s", err, item))
			invalidItems = append(invalidItems, item)
			continue
		}

		// Coba parse timestamp
		parsedTime, err := time.Parse("2006-01-02 15:04:05", deviceData.Timestamp)
		if err != nil {
			logger.Error("WORKER", fmt.Sprintf("ERROR - Invalid timestamp format: %v - TIMESTAMP: %s", err, deviceData.Timestamp))
			invalidItems = append(invalidItems, item)
			continue
		}

		// Data valid, tambahkan ke parsedData
		parsedData = append(parsedData, DeviceData{
			Device_Id:    deviceData.Device_Id,
			Timestamp:    parsedTime.UTC(),
			Voltage:      deviceData.Voltage,
			Current:      deviceData.Current,
			Power:        deviceData.Power,
			Energy:       deviceData.Energy,
			Frequency:    deviceData.Frequency,
			Power_factor: deviceData.Power_factor,
		})
	}

	// Hapus semua data invalid dari Redis dalam satu pipeline
	if len(invalidItems) > 0 {
		pipe := redisClient.Pipeline()
		for _, item := range invalidItems {
			pipe.LRem(ctx, "buffer:device_data", 1, item)
		}
		_, err := pipe.Exec(ctx)
		if err != nil {
			logger.Error("WORKER", fmt.Sprintf("ERROR - Failed to remove invalid items from Redis: %v", err))
		}
	}

	return parsedData, nil
}

func validateDeviceData(data []DeviceData, dbConn *sqlx.DB) ([]DeviceData, error) {
	if len(data) == 0 {
		return nil, nil
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
	logger.Debug("WORKER", fmt.Sprintf("INFO - Valid unit IDs retrieved: %d", len(validUnitIDs)))

	var filteredData []DeviceData
	for _, deviceData := range data {
		if validUnitIDs[deviceData.Device_Id] {
			filteredData = append(filteredData, deviceData)
		}
	}

	return filteredData, nil
}

func dumpDataToDB(data []DeviceData, dbConn *sqlx.DB) error {
	if len(data) == 0 {
		clearRedisBuffer()
		return nil
	}

	// Gunakan batch insert untuk efisiensi
	tx, err := dbConn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	stmt, err := tx.Prepare(`INSERT INTO device.data (unit_id, timestamp, voltage, current, power, energy, frequency, power_factor) 
							VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()
	for _, d := range data {
		_, err := stmt.Exec(d.Device_Id, d.Timestamp, d.Voltage, d.Current, d.Power, d.Energy, d.Frequency, d.Power_factor)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to execute insert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
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
