package handlers

import (
	"database/sql"
	"monitoring_service/crypto"
	"monitoring_service/db"
	"monitoring_service/logger"
	"monitoring_service/utils"
	ws "monitoring_service/ws"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

// Inisialisasi hub WebSocket
var hub = ws.NewWebSocketHub()

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

func Device_Conn_WS(w http.ResponseWriter, r *http.Request) {
	var ctxKey HTTPContextKey = "requestID"
	reference_id, ok := r.Context().Value(ctxKey).(string)
	if !ok {
		reference_id = "unknown"
	}

	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		logger.Debug(reference_id, "DEBUG - Execution completed in:", duration)
	}()

	deviceName := r.Header.Get("name")
	password := r.Header.Get("password")

	logger.Info(reference_id, "INFO - Header name:", deviceName)
	logger.Info(reference_id, "INFO - Header password:", password)

	if deviceName == "" || password == "" {
		logger.Error(reference_id, "ERROR - Missing credentials")
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400000",
			ErrorMessage: "Bad Request",
		})
		return
	}

	// Mendapatkan koneksi database
	conn, err := db.GetConnection()
	if err != nil {
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
			logger.Error(reference_id, "ERROR - Invalid device ID")
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "401000",
				ErrorMessage: "Unauthorized",
			})
		} else {
			logger.Error(reference_id, "ERROR - Database query failed", err)
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
		logger.Error(reference_id, "ERROR - Failed to generate salted password:", errSaltedPass)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500001",
			ErrorMessage: "Internal Server Error",
		})
		return
	}

	logger.Info(reference_id, "INFO - Generated salted password:", saltedPassword)
	logger.Info(reference_id, "INFO - Salted password from DB:", deviceData.SaltedPassword)

	// Verifikasi password
	if saltedPassword != deviceData.SaltedPassword {
		logger.Error(reference_id, "ERROR - Invalid password")
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "401001",
			ErrorMessage: "Unauthorized",
		})
		return
	}

	// Upgrade ke WebSocket
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(reference_id, "ERROR - WebSocket upgrade failed", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500003",
			ErrorMessage: "Failed to upgrade to WebSocket",
		})
		return
	}

	hub.AddDevice(reference_id, wsConn, deviceData.DeviceID, deviceData.DeviceName)

	go func() {
		defer func() {
			hub.RemoveDevice(reference_id, wsConn)
			logger.Info(reference_id, "INFO - WebSocket connection closed")
		}()

		for {
			messageType, message, err := wsConn.ReadMessage()
			if err != nil {
				logger.Error(reference_id, "ERROR - WebSocket read error:", err)
				break
			}

			msgTypeString := strconv.Itoa(messageType)
			messageString := string(message)

			msgString := msgTypeString + messageString

			// Log pesan yang diterima
			logger.Info(reference_id, "INFO - Received WebSocket message:", msgString)
		}
	}()

	logger.Info(reference_id, "INFO - WebSocket connection established")
}

/* func Device_Conn_WS(w http.ResponseWriter, r *http.Request) {
	var ctxKey HTTPContextKey = "requestID"
	reference_id, ok := r.Context().Value(ctxKey).(string)
	if !ok {
		reference_id = "unknown"
	}

	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		logger.Debug(reference_id, "DEBUG - Execution completed in:", duration)
	}()

	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	body, err := utils.Request(r)
	if err != nil {
		logger.Error(reference_id, "ERROR - Invalid request body: ", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400003",
			ErrorMessage: "Invalid request",
		})
		return
	}

	// Validasi device_id dan password
	deviceName, ok := body["name"].(string)
	if !ok || deviceName == "" {
		logger.Error(reference_id, "ERROR - Missing / invalid device name")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		utils.Response(w, result)
		return
	}

	password, ok := body["password"].(string)
	if !ok || password == "" {
		logger.Error(reference_id, "ERROR - Missing password")
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request"
		utils.Response(w, result)
		return
	}

	// Mendapatkan koneksi database
	conn, err := db.GetConnection()
	if err != nil {
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500000",
			ErrorMessage: "Internal Server Error",
		})
		return
	}
	defer db.ReleaseConnection()

	// Ambil credential dari database
	var deviceSalt, deviceSaltedPasswordDb string
	query := `SELECT salt, salted_password FROM device.unit WHERE name = $1`
	err = conn.QueryRow(query, deviceName).Scan(&deviceSalt, &deviceSaltedPasswordDb)
	if err != nil {
		logger.Error(reference_id, "ERROR - Invalid device ID", err)
		result.ErrorCode = "401000"
		result.ErrorMessage = "Unauthorized"
		utils.Response(w, result)
		return
	}

	// Generate salted password
	saltedPassword, errSaltedPass := crypto.GeneratePBKDF2(password, deviceSalt, 32, 1000)
	if errSaltedPass != "" {
		logger.Error(reference_id, "ERROR - Failed to generate salted password:", errSaltedPass)
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		utils.Response(w, result)
		return
	}

	logger.Info(reference_id, "INFO - Generated salted password: ", saltedPassword)
	logger.Info(reference_id, "INFO - Salted password from db: ", deviceSaltedPasswordDb)

	// Verifikasi password
	if saltedPassword != deviceSaltedPasswordDb {
		logger.Error(reference_id, "ERROR - Invalid password")
		result.ErrorCode = "401001"
		result.ErrorMessage = "Unauthorized"
		utils.Response(w, result)
		return
	}

	// Upgrade ke WebSocket
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(reference_id, "ERROR - WebSocket upgrade failed: ", err)
		result.ErrorCode = "500002"
		result.ErrorMessage = "Failed to upgrade to WebSocket"
		utils.Response(w, result)
		return
	}

	hub.AddClient(wsConn)

	go func() {
		message := []byte(`{"status": "connected", "device": "` + deviceName + `"}`)
		if err := wsConn.WriteMessage(websocket.TextMessage, message); err != nil {
			logger.Error(reference_id, "ERROR - Failed to send WebSocket message:", err)
		}
	}()

	logger.Info(reference_id, "SUCCESS - Device connected via WebSocket:", deviceName)
	utils.Response(w, result)
}
*/
