package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"monitoring_service/crypto"
	"monitoring_service/db"
	"monitoring_service/logger"
	pubsub "monitoring_service/pubsub"
	"monitoring_service/utils"
	"net/http"
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

	if deviceName == "" || password == "" {
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

	logger.Debug(referenceID, "DEBUG - Device_Create_Conn - DISINI 1")

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

	logger.Debug(referenceID, "DEBUG - Device_Create_Conn - DISINI 2")

	// Tambahkan perangkat ke hub WebSocket
	hub.AddDeviceToWebSocket(referenceID, wsConn, deviceData.DeviceID, deviceData.DeviceName)

	logger.Debug(referenceID, "DEBUG - Device_Create_Conn - DISINI 3")
	go func() {

		// Contoh data dari device yang diterima:
		// {
		//   "Tstamp": 1707900015,
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
		//   "Tstamp": 1707900015,
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
			logger.Info(referenceID, "INFO - Device_Create_Conn - WebSocket connection closed")
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

			logger.Info(referenceID, "INFO - data is valid, adding device_id")

			// Tambahkan Device_ID
			msgString, err := addDeviceIdField(referenceID, message, deviceData.DeviceID)
			if err != nil {
				logger.Error(referenceID, "ERROR - Failed to process message:", err)
				continue
			}

			logger.Info(referenceID, "INFO - msgString: ", msgString)

			// Push data ke buffer Redis
			err = pubsub.PushDataToBuffer(context.Background(), msgString, referenceID)
			if err != nil {
				logger.Error(referenceID, "ERROR - Failed to push data to Redis Buffer:", err)
			}
			// Kirim data ke Redis
			err = hub.DevicePublishToChannel(referenceID, deviceData.DeviceID, msgString)
			if err != nil {
				logger.Error(referenceID, "ERROR - Failed to publish data to Redis:", err)
			}

		}
	}()

	logger.Info(referenceID, "INFO - WebSocket connection established for device:", deviceData.DeviceName)
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
			logger.Error(referenceID, "ERROR - validateDeviceData - Data not valid: missing field", field)
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

/* // Tambahkan perangkat ke hub WebSocket
hub.AddDevice(referenceID, wsConn, deviceData.DeviceID, deviceData.DeviceName)

go func() {
	defer func() {
		hub.RemoveDevice(referenceID, wsConn)
		logger.Info(referenceID, "INFO - Device_Create_Conn - WebSocket connection closed")
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

		// Decode JSON
		var messageData map[string]interface{}
		err = json.Unmarshal(message, &messageData)
		if err != nil {
			logger.Error(referenceID, "ERROR - Invalid JSON format:", err)
			continue
		}

		// Validasi data
		if !validateDeviceData(messageData) {
			logger.Error(referenceID, "ERROR - Device_Create_Conn - Received invalid data")
			continue
		}

		// Tambahkan Device_ID
		messageData["Device_Id"] = deviceData.DeviceID

		// Konversi kembali ke JSON
		processedMessage, err := json.Marshal(messageData)
		if err != nil {
			logger.Error(referenceID, "ERROR - Failed to marshal processed data:", err)
			continue
		}

		// Push ke Redis
		err = pubsub.PushDataToBuffer(context.Background(), string(processedMessage), referenceID)
		if err != nil {
			logger.Error(referenceID, "ERROR - Failed to push data to Redis Buffer:", err)
		}

		// Kirim ke Redis Channel
		err = hub.DevicePublishToChannel(referenceID, deviceData.DeviceID, string(processedMessage))
		if err != nil {
			logger.Error(referenceID, "ERROR - Failed to publish data to Redis:", err)
		}
	}
}() */
