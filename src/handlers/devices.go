/*
simpel=> \d device.device_activity

	                           Table "device.device_activity"
	Column  |  Type  | Collation | Nullable |                   Default

----------+--------+-----------+----------+---------------------------------------------

	id       | bigint |           | not null | nextval('device.activity_id_seq'::regclass)
	unit_id  | bigint |           | not null |
	actor    | bigint |           |          |
	activity | text   |           | not null |
	timestamp   | bigint |           | not null | EXTRACT(epoch FROM now())::bigint

Indexes:

	"activity_pkey" PRIMARY KEY, btree (id)

Foreign-key constraints:

	"fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
	"fk_user" FOREIGN KEY (actor) REFERENCES sysuser."user"(id) ON DELETE SET NULL
*/

package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"monitoring_service/configs"
	"monitoring_service/crypto"
	"monitoring_service/db"
	"monitoring_service/logger"
	pubsub "monitoring_service/pubsub"
	"monitoring_service/utils"

	"net/http"
	"os"

	//"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"
)

// Upgrader WebSocket
var deviceUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type DeviceClientData struct {
	DeviceID       int64  `db:"id"`
	DeviceName     string `db:"name"`
	Salt           string `db:"salt"`
	SaltedPassword string `db:"salted_password"`
}

var ActionAvailable = map[string]bool{
	"update":     true,
	"deep_sleep": true,
	"restart":    true,
}

/*
reference :
package pubsub
type DeviceClient struct {
	DeviceID         int64
	DeviceName       string
	Conn             *websocket.Conn
	ChannelToPublish string
	PubSub           *redis.PubSub
	Action  		 string
} */

func Device_Create_Conn(w http.ResponseWriter, r *http.Request) {
	// Ambil referenceId dari context
	var ctxKey HTTPContextKey = "requestID"
	referenceId, ok := r.Context().Value(ctxKey).(string)
	if !ok {
		referenceId = "unknown"
	}
	startTime := time.Now()
	defer func() {
		logger.Debug(referenceId, "DEBUG - Device_Create_Conn - Execution completed in:", time.Since(startTime))
		db.ReleaseConnection()
	}()

	// Validasi query params
	params := r.URL.Query()
	deviceName := params.Get("name")
	devicePassword := params.Get("password")
	if strings.TrimSpace(deviceName) == "" || strings.TrimSpace(devicePassword) == "" {
		utils.Response(w, utils.ResultFormat{ErrorCode: "400000", ErrorMessage: "Invalid Request"})
		return
	}

	// Ambil connection DB
	conn, err := db.GetConnection()
	if err != nil {
		utils.Response(w, utils.ResultFormat{ErrorCode: "500000", ErrorMessage: "Internal Server Error"})
		return
	}
	defer db.ReleaseConnection()

	// Ambil data device dari tabel unit
	var deviceData DeviceClientData
	query := `SELECT id, name, salt, salted_password FROM device.unit WHERE name = $1`
	if err := conn.QueryRow(query, deviceName).Scan(
		&deviceData.DeviceID,
		&deviceData.DeviceName,
		&deviceData.Salt,
		&deviceData.SaltedPassword,
	); err != nil {
		if err == sql.ErrNoRows {
			utils.Response(w, utils.ResultFormat{ErrorCode: "401000", ErrorMessage: "Unauthorized"})
		} else {
			utils.Response(w, utils.ResultFormat{ErrorCode: "500002", ErrorMessage: "Internal Server Error"})
		}
		return
	}

	// Decrypt password
	key := os.Getenv("KEY")
	if key == "" {
		logger.Error(referenceId, "ERROR - Device_Create_Conn - KEY is not set")
		utils.Response(w, utils.ResultFormat{ErrorCode: "500001", ErrorMessage: "Internal server error"})
		return
	}
	plainPwd, err := crypto.DecryptAES256(deviceData.SaltedPassword, deviceData.Salt, key)
	if err != nil || plainPwd != devicePassword {
		utils.Response(w, utils.ResultFormat{ErrorCode: "401001", ErrorMessage: "Unauthorized"})
		return
	}

	// Upgrade ke WebSocket
	wsConn, err := deviceUpgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(referenceId, "ERROR - Device_Create_Conn - Failed to upgrade connection:", err)
		utils.Response(w, utils.ResultFormat{ErrorCode: "500003", ErrorMessage: "Internal Server Error"})
		return
	}

	// Tambahkan ke hub
	hub, err := pubsub.GetWebSocketHub(referenceId)
	if err != nil {
		wsConn.Close()
		utils.Response(w, utils.ResultFormat{ErrorCode: "500003", ErrorMessage: "Internal Server Error"})
		return
	}
	if err := hub.AddDeviceToWebSocket(referenceId, wsConn, deviceData.DeviceID, deviceData.DeviceName); err != nil {
		wsConn.Close()
		utils.Response(w, utils.ResultFormat{ErrorCode: "500004", ErrorMessage: "Internal Server Error"})
		return
	}

	// Perbarui status di DB: connected
	tx, _ := conn.Beginx()
	tx.Exec(`UPDATE device.unit SET st = 1 WHERE id = $1`, deviceData.DeviceID)
	tx.Exec(`INSERT INTO device.device_activity (unit_id, activity) VALUES ($1, 'connect')`, deviceData.DeviceID)
	tx.Commit()

	lastPing := time.Now()
	missedPings := 0
	pingTicker := time.NewTicker(time.Duration(configs.GetDeviceWsPingInterval()) * time.Second)
	done := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				hub.Mu.Lock()
				deviceClient := hub.Devices[wsConn]
				hub.Mu.Unlock()

				currAction, err := hub.GetDeviceAction(referenceId, deviceData.DeviceID)
				if err != nil {
					logger.Error(referenceId, fmt.Sprintf("ERROR - Device_Create_Conn - Failed to get device action: %v", err))
					continue
				}

				actionType, ok := currAction["type"].(string)
				if !ok || strings.TrimSpace(actionType) == "" {
					continue
				}

				if ActionAvailable[actionType] {
					logger.Debug(referenceId, "DEBUG - Device_Create_Conn - Current action for device:", currAction)

					messageToDeviceBytes, err := json.Marshal(currAction)
					if err != nil {
						logger.Error(referenceId, fmt.Sprintf("ERROR - Device_Create_Conn - Failed to marshal message: %v", err))
						continue
					}

					if deviceClient == nil {
						logger.Error(referenceId, "ERROR - Device_Create_Conn - Device client not found for connection")
						return
					}

					if err := deviceClient.SafeWriteJSON(currAction); err != nil {
						logger.Error(referenceId, fmt.Sprintf("ERROR - Device_Create_Conn - SafeWriteJSON failed: %v", err))
						return
					}

					logger.Debug(referenceId, "DEBUG - Device_Create_Conn - Sent message to device:", string(messageToDeviceBytes))

					err = hub.SetDeviceAction(referenceId, deviceData.DeviceID, map[string]any{})
					if err != nil {
						logger.Error(referenceId, fmt.Sprintf("ERROR - Device_Create_Conn - Failed to reset device action: %v", err))
						return
					}
				} else {
					logger.Warning(referenceId, fmt.Sprintf("WARNING - Action type %s not available for device %d", actionType, deviceData.DeviceID))
				}

			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		hub.Mu.Lock()
		_, ok := hub.Devices[wsConn]
		if !ok {
			logger.Error(referenceId, "ERROR - WebSocket handler - Device client not found for connection")
			hub.Mu.Unlock() // Pastikan di-unlock sebelum return
			return
		}
		hub.Mu.Unlock()

		defer pingTicker.Stop()
		for {
			select {
			case <-pingTicker.C:
				if time.Since(lastPing) > 5*time.Second {
					missedPings++
					logger.Warning(referenceId,
						fmt.Sprintf("WARN - Device_Create_Conn - Missed ping #%d for device %d", missedPings, deviceData.DeviceID))
				} else {
					missedPings = 0
				}
				if missedPings >= 3 {
					logger.Error(referenceId,
						fmt.Sprintf("ERROR - Device_Create_Conn - No ping for 3 intervals, disconnecting device %d", deviceData.DeviceID))
					handleDeviceDisconnect(referenceId, conn, hub, wsConn, deviceData.DeviceID)
					return // Cukup keluar goroutine
				}
			case <-done:
				return
			}
		}
	}()

	// Loop utama membaca pesan WebSocket
	for {
		messageType, messageFromDevice, err := wsConn.ReadMessage()
		if err != nil {
			logger.Warning(referenceId, "WARN - Device_Create_Conn - WebSocket disconnected:", err)
			break
		}

		if messageType != websocket.TextMessage {
			continue
		}

		var msg map[string]any
		if err := json.Unmarshal(messageFromDevice, &msg); err != nil {
			logger.Error(referenceId, "ERROR - Device_Create_Conn - Invalid JSON:", err)
			continue
		}

		rawT, exists := msg["type"]
		if !exists {
			logger.Warning(referenceId, "WARN - Device_Create_Conn - Missing 'type' field, skipping message")
			continue
		}
		t, ok := rawT.(string)
		if !ok {
			logger.Warning(referenceId, "WARN - Device_Create_Conn - 'type' is not string, skipping message")
			continue
		}

		switch t {
		case "ping":
			lastPing = time.Now()
			logger.Debug(referenceId, "DEBUG - Device_Create_Conn - Received ping from device:", deviceData.DeviceID)

		case "sensor_data":
			if !validateDeviceData(referenceId, messageFromDevice) {
				continue
			}
			msgString := string(messageFromDevice)
			if err := pubsub.PushDataToBuffer(context.Background(), msgString, referenceId); err == nil {
				hub.DevicePublishToChannel(referenceId, deviceData.DeviceID, msgString)
			}

		default:
			logger.Warning(referenceId, "WARNING - Device_Create_Conn - Unknown message type:", t)
		}
	}

	// Bersihkan ticker & disconnect

	close(done)
	handleDeviceDisconnect(referenceId, conn, hub, wsConn, deviceData.DeviceID)
}

// **Fungsi untuk menangani disconnect dengan aman**
func handleDeviceDisconnect(referenceId string, conn *sqlx.DB, hub *pubsub.WebSocketHub, wsConn *websocket.Conn, deviceID int64) {
	logger.Info(referenceId, "INFO - Device_Create_Conn - handleDeviceDisconnect - Device disconnecting:", deviceID)

	tx, err := conn.Beginx()
	if err != nil {
		logger.Error(referenceId, "ERROR - Device_Create_Conn - handleDeviceDisconnect - Failed to begin transaction:", err)
		wsConn.Close()
		return
	}

	_, err = tx.Exec(`INSERT INTO device.device_activity (unit_id, activity) VALUES ($1, 'disconnect')`, deviceID)
	if err != nil {
		logger.Error(referenceId, "ERROR - handleDeviceDisconnect - Failed to insert disconnect activity")
		tx.Rollback()
		wsConn.Close()
		return
	}

	_, err = tx.Exec(`UPDATE device.unit SET st = 0 WHERE id = $1`, deviceID)
	if err != nil {
		logger.Error(referenceId, "ERROR - Device_Create_Conn - handleDeviceDisconnect - Failed to update device status on disconnect")
		tx.Rollback()
		wsConn.Close()
		return
	}

	if err = tx.Commit(); err != nil {
		logger.Error(referenceId, "ERROR - Device_Create_Conn - handleDeviceDisconnect - Failed to commit transaction")
		wsConn.Close()
		return
	}

	// Hapus device dari WebSocket
	hub.RemoveDeviceFromWebSocket(referenceId, wsConn)
	wsConn.Close()
	logger.Info(referenceId, "INFO - Device_Create_Conn - handleDeviceDisconnect - Device successfully disconnected:", deviceID)
}

func validateDeviceData(referenceId string, data []byte) bool {
	// Decode JSON dari []byte ke map[string]interface{}
	var jsonData map[string]any
	err := json.Unmarshal(data, &jsonData)
	if err != nil {
		logStr := fmt.Sprintf("ERROR - Device_Create_Conn - Invalid JSON format: %s, Message: %s", err, string(data))
		logger.Error(referenceId, logStr)
		return false
	}

	// Daftar field yang wajib ada
	requiredFields := []string{"unit_id", "timestamp", "voltage", "current", "power", "energy", "frequency", "power_factor"}

	// Periksa apakah setiap field yang diperlukan ada dalam data
	for _, field := range requiredFields {
		if _, exists := jsonData[field]; !exists {
			logger.Error(referenceId, "ERROR - Device_Create_Conn - validateDeviceData - Data not valid: missing fields : ", field)
			return false
		}
	}

	return true
}
