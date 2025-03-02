/*
simpel=> \d device.device_activity

	                           Table "device.device_activity"
	Column  |  Type  | Collation | Nullable |                   Default

----------+--------+-----------+----------+---------------------------------------------

	id       | bigint |           | not null | nextval('device.activity_id_seq'::regclass)
	unit_id  | bigint |           | not null |
	actor    | bigint |           |          |
	activity | text   |           | not null |
	tstamp   | bigint |           | not null | EXTRACT(epoch FROM now())::bigint

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
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Upgrader WebSocket
var deviceUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Struktur untuk menangani data device dari database
type DeviceClientData struct {
	DeviceID       int64  `db:"id"`
	DeviceName     string `db:"name"`
	Salt           string `db:"salt"`
	SaltedPassword string `db:"salted_password"`
}

/* //exp param : ws://localhost:5001/device-connect?name={device_name}&password={device_password}*/

// Device_Conn_WS menangani koneksi WebSocket dari IoT Device
func Device_Create_Conn(w http.ResponseWriter, r *http.Request) {
	var ctxKey HTTPContextKey = "requestID"
	referenceId, ok := r.Context().Value(ctxKey).(string)
	if !ok {
		referenceId = "unknown"
	}

	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		logger.Debug(referenceId, "DEBUG - Device_Create_Conn - Execution completed in:", duration)
	}()

	// Ambil token, session_id dan device_id dari query parameter
	deviceName := r.URL.Query().Get("name")
	password := r.URL.Query().Get("password")

	logger.Info(referenceId, "INFO - Device_Create_Conn - Incoming WebSocket connection - Device:", deviceName)

	if strings.TrimSpace(deviceName) == "" || strings.TrimSpace(password) == "" {

		logger.Error(referenceId, "ERROR - Device_Create_Conn - Missing credentials")
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400000",
			ErrorMessage: "Bad Request",
		})
		return
	}

	// Mendapatkan koneksi database
	conn, err := db.GetConnection()
	if err != nil {
		logger.Error(referenceId, "ERROR - Device_Create_Conn - Failed to get database connection:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500000",
			ErrorMessage: "Internal Server Error",
		})
		return
	}
	defer db.ReleaseConnection()

	// Ambil credential dari database
	var deviceData DeviceClientData
	query := `SELECT id, name, salt, salted_password FROM device.unit WHERE name = $1`
	err = conn.QueryRow(query, deviceName).Scan(
		&deviceData.DeviceID, &deviceData.DeviceName,
		&deviceData.Salt, &deviceData.SaltedPassword,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			logger.Error(referenceId, "ERROR - Device_Create_Conn -  Invalid device ID")
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "401000",
				ErrorMessage: "Unauthorized",
			})
		} else {
			logger.Error(referenceId, "ERROR - Device_Create_Conn - Database query failed:", err)
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "500002",
				ErrorMessage: "Internal Server Error",
			})
		}
		return
	}

	// Generate salted password
	saltedPassword, errSaltedPass := crypto.GeneratePBKDF2(password, deviceData.Salt, 32, configs.GetPBKDF2Iterations())
	if errSaltedPass != "" {
		logger.Error(referenceId, "ERROR - Device_Create_Conn - Failed to generate salted password:", errSaltedPass)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500001",
			ErrorMessage: "Internal Server Error",
		})
		return
	}

	// Verifikasi password
	if saltedPassword != deviceData.SaltedPassword {
		logger.Error(referenceId, "ERROR - Invalid password")
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "401001",
			ErrorMessage: "Unauthorized",
		})
		return
	}

	//////////////////////////////// 	//////////////////////////////////

	hub, err := pubsub.GetWebSocketHub(referenceId)
	if err != nil {

		logger.Error(referenceId, "ERROR - Device_Create_Conn - Failed to initialize WebSocketHub:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500003",
			ErrorMessage: "Internal Server error",
		})
		return
	}

	// Upgrade ke WebSocket
	wsConn, err := deviceUpgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(referenceId, "ERROR - Device_Create_Conn - WebSocket upgrade failed:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500003",
			ErrorMessage: "Internal server error",
		})
		return
	}

	// Tambahkan perangkat ke hub WebSocket
	errWs := hub.AddDeviceToWebSocket(referenceId, wsConn, deviceData.DeviceID, deviceData.DeviceName)
	if errWs != nil {
		logger.Error(referenceId, "ERROR - Device_Create_Conn - Failed to add device to WebSocket hub:", errWs)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500004",
			ErrorMessage: "Internal server error",
		})
	}

	tx, errTx := conn.Beginx()
	if errTx != nil {
		logger.Error(referenceId, "ERROR - Change_Device_Status - Failed to begin transaction:", errTx)
		hub.RemoveDeviceFromWebSocket(referenceId, wsConn)
		wsConn.Close() // Tutup WebSocket sebelum return

		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500005",
			ErrorMessage: "Internal server error",
		})
		return
	}
	defer tx.Rollback() // Rollback jika transaksi tidak berhasil sampai commit

	var deviceIdToUpdate int64
	queryToChangeStatus := `UPDATE device.unit SET st = $1 WHERE id = $2 RETURNING id`
	err = tx.Get(&deviceIdToUpdate, queryToChangeStatus, 1, deviceData.DeviceID)
	if err != nil {
		err := hub.RemoveDeviceFromWebSocket(referenceId, wsConn)
		if err != nil {
			logger.Warning(referenceId, "ERROR - Change_Device_Status - Failed to remove device from WebSocket hub after commit:", err)
		}
		wsConn.Close() // Tutup WebSocket sebelum return
		logger.Error(referenceId, "ERROR - Change_Device_Status - Failed to update status:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500006",
			ErrorMessage: "Internal server error",
		})
		return
	}

	queryToInsertActivity := `INSERT INTO device.device_activity (unit_id, activity) VALUES ($1, $2)`
	_, err = tx.Exec(queryToInsertActivity, deviceData.DeviceID, "Connect Device")

	if err != nil {
		err := hub.RemoveDeviceFromWebSocket(referenceId, wsConn)
		if err != nil {
			logger.Warning(referenceId, "ERROR - Change_Device_Status - Failed to remove device from WebSocket hub after commit:", err)
		}

		wsConn.Close() // Tutup WebSocket sebelum return
		logger.Error(referenceId, "ERROR - Change_Device_Status - Failed to insert activity:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500007",
			ErrorMessage: "Internal server error",
		})
		return
	}

	if err := tx.Commit(); err != nil {
		logger.Error(referenceId, "ERROR - Change_Device_Status - Failed to commit transaction:", err)

		err := hub.RemoveDeviceFromWebSocket(referenceId, wsConn)
		if err != nil {
			logger.Warning(referenceId, "ERROR - Change_Device_Status - Failed to remove device from WebSocket hub after commit:", err)
		}
		wsConn.Close() // Tutup WebSocket sebelum return

		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500008",
			ErrorMessage: "Internal server error",
		})
		return
	}

	go func() {

		// Contoh data dari device yang diterima:
		// {
		//   "Tstamp": "2025-02-22 12:02:00+00",
		//   "Voltage": 220.5,
		//   "Current": 2.3,
		//   "Power": 700.15,
		//   "Energy": 900.75,
		//   "Frequency": 50.1,
		//   "Power_factor": 0.98
		// }

		// Setelah diproses, data akan menjadi:
		// {
		//   "Device_Id": 1,
		//   "Tstamp": "2025-02-22 12:02:00+00",
		//   "Voltage": 220.5,
		//   "Current": 2.3,
		//   "Power": 700.15,
		//   "Energy": 900.75,
		//   "Frequency": 50.1,
		//   "Power_factor": 0.98
		// }

		// !! NOTE: Langkah-langkah untuk memproses data dari sensor:
		// 1. Periksa apakah semua field yang diperlukan ada dalam data yang diterima.
		//    Jika ada field yang hilang, kirim pesan error {Data not valid} dan abaikan data.
		// 2. Tambahkan "Device_Id" ke dalam pesan dengan mengambil dari DeviceClientData.DeviceId.
		// 3. Jika data valid, publish ke Redis. Jika tidak, lewati proses.

		defer func() {
			err := hub.RemoveDeviceFromWebSocket(referenceId, wsConn)
			if err != nil {
				logger.Error(referenceId, "ERROR - Change_Device_Status - Failed to remove device from WebSocket hub:", err)
			}
			// Update status perangkat setelah disconnect
			maxRetries := 3
			for i := 0; i < maxRetries; i++ {
				_, err = conn.Exec(queryToChangeStatus, 0, deviceData.DeviceID)
				if err == nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if err != nil {
				logger.Error(referenceId, "ERROR - Change_Device_Status - Final attempt to update status failed:", err)
			}

		}()

		for {
			messageType, message, err := wsConn.ReadMessage()
			if err != nil {
				logger.Error(referenceId, "ERROR - Device_Create_Conn - WebSocket read error:", err)
				break
			}

			// Format messageType (1 = Text, 2 = Binary)
			msgTypeStr := "Unknown"
			if messageType == 1 {
				msgTypeStr = "Text"
			} else if messageType == 2 {
				msgTypeStr = "Binary"
			}

			// Log pesan yang diterima
			logStr := fmt.Sprintf("Message Type: %s, Message: %s", msgTypeStr, string(message))
			logger.Info(referenceId, "INFO - Received WebSocket: ", logStr)

			// Validasi data
			if !validateDeviceData(referenceId, message) {
				continue
			}

			logger.Info(referenceId, "INFO - Device_Create_Conn - data is valid, adding device_id")

			// Tambahkan Device_ID
			msgString, err := addDeviceIdField(referenceId, message, deviceData.DeviceID)
			if err != nil {
				logger.Error(referenceId, "ERROR - Device_Create_Conn - Failed to process message:", err)
				continue
			}

			logger.Info(referenceId, "INFO - msgString: ", msgString)

			// Push data ke buffer Redis
			err = pubsub.PushDataToBuffer(context.Background(), msgString, referenceId)
			if err != nil {
				logger.Error(referenceId, "ERROR - Device_Create_Conn-  Failed to push data to Redis Buffer: ", err)
			}
			// Kirim data ke Redis
			err = hub.DevicePublishToChannel(referenceId, deviceData.DeviceID, msgString)
			if err != nil {
				logger.Error(referenceId, "ERROR - Device_Create_Conn - Failed to publish data to Redis : ", err)
			}

		}
	}()

	logger.Info(referenceId, "INFO - Device_Create_Conn - WebSocket connection established for device:", deviceData.DeviceName)
}

// Fungsi validasi data dari IoT
func validateDeviceData(referenceId string, data []byte) bool {
	// Decode JSON dari []byte ke map[string]interface{}
	var jsonData map[string]interface{}
	err := json.Unmarshal(data, &jsonData)
	if err != nil {
		logStr := fmt.Sprintf("Invalid JSON format: %s, Message: %s", err, string(data))
		logger.Error(referenceId, logStr)
		return false
	}

	// Daftar field yang wajib ada
	requiredFields := []string{"Tstamp", "Voltage", "Current", "Power", "Energy", "Frequency", "Power_factor"}

	// Periksa apakah setiap field yang diperlukan ada dalam data
	for _, field := range requiredFields {
		if _, exists := jsonData[field]; !exists {
			logger.Error(referenceId, "ERROR - validateDeviceData - Data not valid: missing fields : ", field)
			return false
		}
	}

	return true
}

// Fungsi untuk menambahkan Device_ID ke JSON
func addDeviceIdField(referenceId string, message []byte, deviceID int64) (string, error) {
	// Decode JSON
	var messageData map[string]interface{}
	err := json.Unmarshal(message, &messageData)
	if err != nil {
		logger.Error(referenceId, "ERROR - addDeviceIdField - Invalid JSON format:", err)
		return "", err
	}

	// Tambahkan Device_ID ke data
	messageData["Device_Id"] = deviceID

	// Encode kembali ke JSON string
	msgBytes, err := json.Marshal(messageData)
	if err != nil {
		logger.Error(referenceId, "ERROR - addDeviceIdField - Failed to encode JSON:", err)
		return "", err
	}

	return string(msgBytes), nil
}

/*
simpel=> \d device.device_activity

Table "device.device_activity"
	Column  |  Type  | Collation | Nullable |                   Default

----------+--------+-----------+----------+---------------------------------------------

	id       | bigint |           | not null | nextval('device.activity_id_seq'::regclass)
	unit_id  | bigint |           | not null |
	actor    | bigint |           |          |
	activity | text   |           | not null |
	tstamp   | bigint |           | not null | EXTRACT(epoch FROM now())::bigint

Indexes:

	"activity_pkey" PRIMARY KEY, btree (id)

Foreign-key constraints:

	"fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
	"fk_user" FOREIGN KEY (actor) REFERENCES sysuser."user"(id) ON DELETE SET NULL
*/
