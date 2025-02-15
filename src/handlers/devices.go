package handlers

import (
	"context"
	"database/sql"
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

	deviceName := r.Header.Get("name")
	password := r.Header.Get("password")

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

			msgString := string(message)

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
