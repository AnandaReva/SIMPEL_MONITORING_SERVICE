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

func Device_Create_Conn(w http.ResponseWriter, r *http.Request) {
	var ctxKey HTTPContextKey = "requestID"
	referenceId, ok := r.Context().Value(ctxKey).(string)
	if !ok {
		referenceId = "unknown"
	}

	startTime := time.Now()
	defer func() {
		logger.Debug(referenceId, "DEBUG - Device_Create_Conn - Execution completed in:", time.Since(startTime))
	}()

	deviceName := r.URL.Query().Get("name")
	password := r.URL.Query().Get("password")

	logger.Info(referenceId, "INFO - Device_Create_Conn Incoming WebSocket connection - Device:", deviceName)

	if strings.TrimSpace(deviceName) == "" || strings.TrimSpace(password) == "" {
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400000",
			ErrorMessage: "Invalid Request",
		})
		return
	}

	conn, err := db.GetConnection()
	if err != nil {
		utils.Response(w, utils.ResultFormat{ErrorCode: "500000", ErrorMessage: "Internal Server Error"})
		return
	}
	defer db.ReleaseConnection()

	var deviceData DeviceClientData
	query := `SELECT id, name, salt, salted_password FROM device.unit WHERE name = $1`
	err = conn.QueryRow(query, deviceName).Scan(&deviceData.DeviceID, &deviceData.DeviceName, &deviceData.Salt, &deviceData.SaltedPassword)
	if err != nil {
		if err == sql.ErrNoRows {
			utils.Response(w, utils.ResultFormat{ErrorCode: "401000", ErrorMessage: "Unauthorized"})
		} else {
			utils.Response(w, utils.ResultFormat{ErrorCode: "500002", ErrorMessage: "Internal Server Error"})
		}
		return
	}

	saltedPassword, errSaltedPass := crypto.GeneratePBKDF2(password, deviceData.Salt, 32, configs.GetPBKDF2Iterations())
	if errSaltedPass != err || saltedPassword != deviceData.SaltedPassword {
		utils.Response(w, utils.ResultFormat{ErrorCode: "401001", ErrorMessage: "Unauthorized"})
		return
	}

	hub, err := pubsub.GetWebSocketHub(referenceId)
	if err != nil {
		utils.Response(w, utils.ResultFormat{ErrorCode: "500003", ErrorMessage: "Internal Server Error"})
		return
	}

	wsConn, err := deviceUpgrader.Upgrade(w, r, nil)
	if err != nil {
		utils.Response(w, utils.ResultFormat{ErrorCode: "500003", ErrorMessage: "Internal Server Error"})
		return
	}

	err = hub.AddDeviceToWebSocket(referenceId, wsConn, deviceData.DeviceID, deviceData.DeviceName)
	if err != nil {
		utils.Response(w, utils.ResultFormat{ErrorCode: "500004", ErrorMessage: "Internal Server Error"})
		return
	}

	// Update status device sebagai "Connected"
	tx, err := conn.Beginx()
	if err != nil {
		utils.Response(w, utils.ResultFormat{ErrorCode: "500005", ErrorMessage: "Internal Server Error"})
		return
	}

	_, err = tx.Exec(`UPDATE device.unit SET st = 1 WHERE id = $1`, deviceData.DeviceID)
	if err == nil {
		_, err = tx.Exec(`INSERT INTO device.device_activity (unit_id, activity) VALUES ($1, 'Connect')`, deviceData.DeviceID)
	}

	if err != nil || tx.Commit() != nil {
		utils.Response(w, utils.ResultFormat{ErrorCode: "500006", ErrorMessage: "Internal Server Error"})
		return
	}

	// **Handle disconnect secara eksplisit**
	defer func() {
		handleDeviceDisconnect(referenceId, conn, hub, wsConn, deviceData.DeviceID)
	}()

	// **Loop untuk membaca pesan WebSocket**
	for {
		messageType, message, err := wsConn.ReadMessage()
		if err != nil {
			logger.Warning(referenceId, "WARN - Device_Create_Conn - WebSocket disconnected:", err)
			break
		}

		if messageType != websocket.TextMessage {
			continue
		}

		if !validateDeviceData(referenceId, message) {
			continue
		}

		// msgString, err := addDeviceIdField(referenceId, message, deviceData.DeviceID)
		// if err != nil {
		// 	continue
		// }

		msgString := string(message)

		err = pubsub.PushDataToBuffer(context.Background(), msgString, referenceId)
		if err == nil {
			err = hub.DevicePublishToChannel(referenceId, deviceData.DeviceID, msgString)
		}

		if err != nil {
			logger.Error(referenceId, "ERROR - Failed to process WebSocket message:", err)
		}
	}
}

// **Fungsi untuk menangani disconnect dengan aman**
func handleDeviceDisconnect(referenceId string, conn *sqlx.DB, hub *pubsub.WebSocketHub, wsConn *websocket.Conn, deviceID int64) {
	logger.Info(referenceId, "INFO - handleDeviceDisconnect - Device disconnecting:", deviceID)

	tx, err := conn.Beginx()
	if err != nil {
		logger.Error(referenceId, "ERROR - handleDeviceDisconnect - Failed to begin transaction:", err)
		wsConn.Close()
		return
	}

	_, err = tx.Exec(`INSERT INTO device.device_activity (unit_id, activity) VALUES ($1, 'Disconnect')`, deviceID)
	if err != nil {
		logger.Error(referenceId, "ERROR - handleDeviceDisconnect - Failed to insert disconnect activity")
		tx.Rollback()
		wsConn.Close()
		return
	}

	_, err = tx.Exec(`UPDATE device.unit SET st = 0 WHERE id = $1`, deviceID)
	if err != nil {
		logger.Error(referenceId, "ERROR - handleDeviceDisconnect - Failed to update device status on disconnect")
		tx.Rollback()
		wsConn.Close()
		return
	}

	if err = tx.Commit(); err != nil {
		logger.Error(referenceId, "ERROR - handleDeviceDisconnect - Failed to commit transaction")
		wsConn.Close()
		return
	}

	// Hapus device dari WebSocket
	hub.RemoveDeviceFromWebSocket(referenceId, wsConn)
	wsConn.Close()
	logger.Info(referenceId, "INFO - handleDeviceDisconnect - Device successfully disconnected:", deviceID)
}

/* func addDeviceIdField(referenceId string, message []byte, deviceID int64) (string, error) {
	// Decode JSON
	var messageData map[string]any
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
} */

func validateDeviceData(referenceId string, data []byte) bool {
	// Decode JSON dari []byte ke map[string]interface{}
	var jsonData map[string]any
	err := json.Unmarshal(data, &jsonData)
	if err != nil {
		logStr := fmt.Sprintf("Invalid JSON format: %s, Message: %s", err, string(data))
		logger.Error(referenceId, logStr)
		return false
	}

	// Daftar field yang wajib ada
	requiredFields := []string{"unit_id", "tstamp", "voltage", "current", "power", "energy", "frequency", "power_factor"}

	// Periksa apakah setiap field yang diperlukan ada dalam data
	for _, field := range requiredFields {
		if _, exists := jsonData[field]; !exists {
			logger.Error(referenceId, "ERROR - validateDeviceData - Data not valid: missing fields : ", field)
			return false
		}
	}

	return true
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

type DeviceData struct {
	DeviceID           int64 `db:"id" json:"device_id"`
	DeviceReadInterval int16 `db:"read_interval" json:"device_read_interval"`
}

func Device_Get_Data(w http.ResponseWriter, r *http.Request) {
	// Konteks request ID
	var ctxKey HTTPContextKey = "requestID"
	referenceID, ok := r.Context().Value(ctxKey).(string)
	if !ok {
		referenceID = "unknown"
	}

	startTime := time.Now()
	defer func() {
		logger.Debug(referenceID, "DEBUG - Device_Get_Data - Execution completed in:", time.Since(startTime))
	}()

	logger.Debug(referenceID, "DEBUG - Device_Get_Data - raw body: ", r.Body)

	// Ambil body
	param, err := utils.Request(r)
	if err != nil {
		logger.Error(referenceID, "ERROR - Device_Get_Data - Failed to parse request body:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400000",
			ErrorMessage: "Invalid Request",
		})
		return
	}

	deviceName, ok := param["name"].(string)
	if !ok || deviceName == "" {
		logger.Error(referenceID, "ERROR - Device_Get_Data - Missing or invalid 'name'")
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400001",
			ErrorMessage: "Invalid Request",
		})
		return
	}

	password, ok := param["password"].(string)
	if !ok || password == "" {
		logger.Error(referenceID, "ERROR - Device_Get_Data - Missing or invalid 'password'")
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400002",
			ErrorMessage: "Invalid Request",
		})
		return
	}

	logStr := fmt.Sprintf("name: %s , password: %s ", deviceName, password)
	logger.Info(referenceID, "INFO - Device_Get_Data - ", logStr)

	// Dapatkan koneksi database
	conn, err := db.GetConnection()
	if err != nil {
		logger.Error(referenceID, "ERROR - Device_Get_Data - Failed to get database connection")
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500000",
			ErrorMessage: "Internal Server Error",
		})
		return
	}
	defer db.ReleaseConnection()

	queryToGetSalt := `SELECT salt FROM device.unit WHERE name = $1`

	var salt string
	err = conn.QueryRow(queryToGetSalt, deviceName).Scan(&salt)
	if err != nil {
		logger.Error(referenceID, "ERROR - Device_Get_Data - Failed to get salt:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "401000",
			ErrorMessage: "Unauthorized",
		})
		return
	}

	logger.Info(referenceID, "INFO - Device_Get_Data - Salt retrieved:", salt)

	// Generate hashed password menggunakan PBKDF2
	saltedPassword, errSaltedPass := crypto.GeneratePBKDF2(password, salt, 32, configs.GetPBKDF2Iterations())
	if errSaltedPass != nil {
		logger.Error(referenceID, "ERROR - Device_Get_Data - Failed to generate salted password:", errSaltedPass)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500001",
			ErrorMessage: "Internal Server Error",
		})
		return
	}

	logger.Info(referenceID, "INFO - Device_Get_Data - Salted password generated:", saltedPassword)

	// Query untuk mendapatkan data perangkat
	queryTogetDeviceData := `SELECT id, read_interval FROM device.unit WHERE name = $1 AND salted_password = $2`

	var deviceData DeviceData
	err = conn.QueryRow(queryTogetDeviceData, deviceName, saltedPassword).Scan(
		&deviceData.DeviceID,
		&deviceData.DeviceReadInterval,
	)
	if err != nil {
		logger.Error(referenceID, "ERROR - Device_Get_Data - Failed to get device data:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "401000",
			ErrorMessage: "Unauthorized",
		})
		return
	}

	logStr = fmt.Sprintf("Device ID: %d , Device Read Interval: %d", deviceData.DeviceID, deviceData.DeviceReadInterval)
	logger.Debug(referenceID, "DEBUG - Device_Get_Data - Device data retrieved:", logStr)

	// Kirim respons
	utils.Response(w, utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload: map[string]any{
			"status":        "success",
			"device_id":     deviceData.DeviceID,
			"read_interval": deviceData.DeviceReadInterval,
		},
	})
}
