package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"monitoring_service/crypto"
	"monitoring_service/db"
	"monitoring_service/logger"
	pubsub "monitoring_service/pubsub"
	"monitoring_service/utils"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"
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
	referenceID, ok := r.Context().Value(ctxKey).(string)
	if !ok {
		referenceID = "unknown"
	}

	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		logger.Debug(referenceID, "DEBUG - Device_Create_Conn - Execution completed in:", duration)
	}()

	// Ambil token, session_id dan device_id dari query parameter
	deviceName := r.URL.Query().Get("name")
	password := r.URL.Query().Get("password")

	logger.Info(referenceID, "INFO - Device_Create_Conn - Incoming WebSocket connection - Device:", deviceName)

	if strings.TrimSpace(deviceName) == "" || strings.TrimSpace(password) == "" {

		logger.Error(referenceID, "ERROR - Device_Create_Conn - Missing credentials")
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400000",
			ErrorMessage: "Bad Request",
		})
		return
	}

	// Mendapatkan koneksi database
	conn, err := db.GetConnection()
	if err != nil {
		logger.Error(referenceID, "ERROR - Device_Create_Conn - Failed to get database connection:", err)
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
			logger.Error(referenceID, "ERROR - Device_Create_Conn -  Invalid device ID")
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "401000",
				ErrorMessage: "Unauthorized",
			})
		} else {
			logger.Error(referenceID, "ERROR - Device_Create_Conn - Database query failed:", err)
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "500002",
				ErrorMessage: "Internal Server Error",
			})
		}
		return
	}

	// Generate salted password
	saltedPassword, errSaltedPass := crypto.GeneratePBKDF2(password, deviceData.Salt, 32, 1000)
	if errSaltedPass != "" {
		logger.Error(referenceID, "ERROR - Device_Create_Conn - Failed to generate salted password:", errSaltedPass)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500001",
			ErrorMessage: "Internal Server Error",
		})
		return
	}

	// Verifikasi password
	if saltedPassword != deviceData.SaltedPassword {
		logger.Error(referenceID, "ERROR - Invalid password")
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "401001",
			ErrorMessage: "Unauthorized",
		})
		return
	}

	//////////////////////////////// 	//////////////////////////////////

	hub, err := pubsub.GetWebSocketHub(referenceID)
	if err != nil {

		logger.Error(referenceID, "ERROR - Device_Create_Conn - Failed to initialize WebSocketHub:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500003",
			ErrorMessage: "Internal Server error",
		})
		return
	}

	// Upgrade ke WebSocket
	wsConn, err := deviceUpgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(referenceID, "ERROR - Device_Create_Conn - WebSocket upgrade failed:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500003",
			ErrorMessage: "Internal server",
		})
		return
	}

	// Tambahkan perangkat ke hub WebSocket
	hub.AddDeviceToWebSocket(referenceID, wsConn, deviceData.DeviceID, deviceData.DeviceName)

	logger.Debug(referenceID, "DEBUG - Device_Create_Conn - DISINI 3")
	errChangeStatus := Change_Device_Status(referenceID, deviceData.DeviceID, 1, conn)
	if errChangeStatus != nil {
		logger.Error(referenceID, "ERROR - Device_Create_Conn - Failed to change device status", errChangeStatus)
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
			hub.RemoveDeviceFromWebSocket(referenceID, wsConn)
			err := Change_Device_Status(referenceID, deviceData.DeviceID, 0, conn)
			if err != nil {
				logger.Error(referenceID, "ERROR - Device_Create_Conn - Failed to change device status", err)
			}
			logger.Info(referenceID, "INFO - Device_Create_Conn - WebSocket connection closed and status changed")
		}()

		for {
			messageType, message, err := wsConn.ReadMessage()
			if err != nil {
				logger.Error(referenceID, "ERROR - Device_Create_Conn - WebSocket read error:", err)
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
			logger.Info(referenceID, "INFO - Received WebSocket: ", logStr)

			// Validasi data
			if !validateDeviceData(referenceID, message) {
				continue
			}

			logger.Info(referenceID, "INFO - Device_Create_Conn - data is valid, adding device_id")

			// Tambahkan Device_ID
			msgString, err := addDeviceIdField(referenceID, message, deviceData.DeviceID)
			if err != nil {
				logger.Error(referenceID, "ERROR - Device_Create_Conn - Failed to process message:", err)
				continue
			}

			logger.Info(referenceID, "INFO - msgString: ", msgString)

			// Push data ke buffer Redis
			err = pubsub.PushDataToBuffer(context.Background(), msgString, referenceID)
			if err != nil {
				logger.Error(referenceID, "ERROR - Device_Create_Conn-  Failed to push data to Redis Buffer: ", err)
			}
			// Kirim data ke Redis
			err = hub.DevicePublishToChannel(referenceID, deviceData.DeviceID, msgString)
			if err != nil {
				logger.Error(referenceID, "ERROR - Device_Create_Conn - Failed to publish data to Redis : ", err)
			}

		}
	}()

	logger.Info(referenceID, "INFO - Device_Create_Conn - WebSocket connection established for device:", deviceData.DeviceName)
}

// Fungsi validasi data dari IoT
func validateDeviceData(referenceID string, data []byte) bool {
	// Decode JSON dari []byte ke map[string]interface{}
	var jsonData map[string]interface{}
	err := json.Unmarshal(data, &jsonData)
	if err != nil {
		logger.Error(referenceID, "ERROR - validateDeviceData - Invalid JSON format:", err)
		return false
	}

	// Daftar field yang wajib ada
	requiredFields := []string{"Tstamp", "Voltage", "Current", "Power", "Energy", "Frequency", "Power_factor"}

	// Periksa apakah setiap field yang diperlukan ada dalam data
	for _, field := range requiredFields {
		if _, exists := jsonData[field]; !exists {
			logger.Error(referenceID, "ERROR - validateDeviceData - Data not valid: missing fields : ", field)
			return false
		}
	}

	return true
}

// Fungsi untuk menambahkan Device_ID ke JSON
func addDeviceIdField(referenceID string, message []byte, deviceID int64) (string, error) {
	// Decode JSON
	var messageData map[string]interface{}
	err := json.Unmarshal(message, &messageData)
	if err != nil {
		logger.Error(referenceID, "ERROR - addDeviceIdField - Invalid JSON format:", err)
		return "", err
	}

	// Tambahkan Device_ID ke data
	messageData["Device_Id"] = deviceID

	// Encode kembali ke JSON string
	msgBytes, err := json.Marshal(messageData)
	if err != nil {
		logger.Error(referenceID, "ERROR - addDeviceIdField - Failed to encode JSON:", err)
		return "", err
	}

	return string(msgBytes), nil
}

func Change_Device_Status(referenceID string, deviceId int64, currStatus int, conn *sqlx.DB) error {
	// Validasi currStatus (hanya boleh 0 atau 1)

	logStr := fmt.Sprintf("Change_Device_Status - device_id %d, status = %d", deviceId, currStatus)
	logger.Info(referenceID, "INFO - ", logStr)

	if currStatus < 0 || currStatus > 1 {
		logger.Error(referenceID, "ERROR - Change_Device_Status - currStatus value missing or invalid:", currStatus)
		return errors.New("currStatus value missing or invalid")
	}

	var deviceIdToUpdate int64
	queryToChangeStatus := `UPDATE device.unit SET st = $1 WHERE id = $2 RETURNING id`

	errQuery := conn.Get(&deviceIdToUpdate, queryToChangeStatus, currStatus, deviceId)
	if errQuery != nil {
		logger.Error(referenceID, "ERROR - Change_Device_Status - Failed to update:", errQuery)
		return errQuery
	}

	return nil
}
