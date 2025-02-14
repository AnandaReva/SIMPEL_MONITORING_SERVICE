package handlers

import (
	"database/sql"
	"monitoring_service/crypto"
	"monitoring_service/db"
	"monitoring_service/logger"
	pubsub "monitoring_service/pubsub"
	"monitoring_service/utils"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

/* // Inisialisasi hub WebSocket
var hub *pubsub.WebSocketHub

func init() {
	var err error
	hub, err = pubsub.NewWebSocketHub("INIT")
	if err != nil {
		logger.Error("INIT", "ERROR - Failed to initialize WebSocketHub:", err)
	}
}
*/
// Upgrader WebSocket
var upgrader = websocket.Upgrader{
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
		logger.Debug(referenceID, "DEBUG - Execution completed in:", duration)
	}()

	deviceName := r.Header.Get("name")
	password := r.Header.Get("password")

	logger.Info(referenceID, "INFO - Incoming WebSocket connection - Device:", deviceName)

	if deviceName == "" || password == "" {
		logger.Error(referenceID, "ERROR - Missing credentials")
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400000",
			ErrorMessage: "Bad Request",
		})
		return
	}

	// Mendapatkan koneksi database
	conn, err := db.GetConnection()
	if err != nil {
		logger.Error(referenceID, "ERROR - Failed to get database connection:", err)
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
			logger.Error(referenceID, "ERROR - Invalid device ID")
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "401000",
				ErrorMessage: "Unauthorized",
			})
		} else {
			logger.Error(referenceID, "ERROR - Database query failed:", err)
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
		logger.Error(referenceID, "ERROR - Failed to generate salted password:", errSaltedPass)
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

		logger.Error(referenceID, "ERROR - Failed to initialize WebSocketHub:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500003",
			ErrorMessage: "Internal Server error",
		})
		return
	}

	// Upgrade ke WebSocket
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(referenceID, "ERROR - WebSocket upgrade failed:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500003",
			ErrorMessage: "Failed to upgrade to WebSocket",
		})
		return
	}

	// Tambahkan perangkat ke hub WebSocket
	hub.AddDevice(referenceID, wsConn, deviceData.DeviceID, deviceData.DeviceName)

	go func() {
		defer func() {
			hub.RemoveDevice(referenceID, wsConn)
			logger.Info(referenceID, "INFO - WebSocket connection closed")
		}()

		for {
			messageType, message, err := wsConn.ReadMessage()
			if err != nil {
				logger.Error(referenceID, "ERROR - WebSocket read error:", err)
				break
			}

			msgString := strconv.Itoa(messageType) + string(message)

			// Log pesan yang diterima
			logger.Info(referenceID, "INFO - Received WebSocket message: ", msgString)

			// Kirim data ke Redis
			err = hub.DevicePublishToChannel(referenceID, deviceData.DeviceID, msgString)
			if err != nil {
				logger.Error(referenceID, "ERROR - Failed to publish data to Redis:", err)
			}
		}
	}()

	logger.Info(referenceID, "INFO - WebSocket connection established for device:", deviceData.DeviceName)
}
