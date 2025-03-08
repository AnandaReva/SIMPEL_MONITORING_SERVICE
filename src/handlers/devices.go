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
		logger.Debug(referenceId, "DEBUG - DeviceWebSocketHandler - Execution completed in:", time.Since(startTime))
	}()

	deviceName := r.URL.Query().Get("name")
	password := r.URL.Query().Get("password")

	logger.Info(referenceId, "INFO - Incoming WebSocket connection - Device:", deviceName)

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
	if errSaltedPass != "" || saltedPassword != deviceData.SaltedPassword {
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

	tx, err := conn.Beginx()
	if err != nil {
		utils.Response(w, utils.ResultFormat{ErrorCode: "500005", ErrorMessage: "Internal Server Error"})
		return
	}
	defer tx.Rollback()

	_, err = tx.Exec(`UPDATE device.unit SET st = 1 WHERE id = $1`, deviceData.DeviceID)
	if err == nil {
		_, err = tx.Exec(`INSERT INTO device.device_activity (unit_id, activity) VALUES ($1, 'Connect Device')`, deviceData.DeviceID)
	}

	if err != nil || tx.Commit() != nil {
		utils.Response(w, utils.ResultFormat{ErrorCode: "500006", ErrorMessage: "Internal Server Error"})
		return
	}

	defer func() {
		hub.RemoveDeviceFromWebSocket(referenceId, wsConn)
		wsConn.Close()
		_, err := conn.Exec(`UPDATE device.unit SET st = 0 WHERE id = $1`, deviceData.DeviceID)
		if err != nil {
			logger.Error(referenceId, "ERROR - Failed to update device status on disconnect:", err)
		}
	}()

	for {
		messageType, message, err := wsConn.ReadMessage()
		if err != nil {
			break
		}

		if messageType != websocket.TextMessage {
			continue
		}

		if !validateDeviceData(referenceId, message) {
			continue
		}

		msgString, err := addDeviceIdField(referenceId, message, deviceData.DeviceID)
		if err != nil {
			continue
		}

		err = pubsub.PushDataToBuffer(context.Background(), msgString, referenceId)
		if err == nil {
			err = hub.DevicePublishToChannel(referenceId, deviceData.DeviceID, msgString)
		}

		if err != nil {
			logger.Error(referenceId, "ERROR - Failed to process WebSocket message:", err)
		}
	}
}

// Contoh data dari device yang diterima:
// {
//   "Tstamp": "'2025-03-08 14:30:00'",
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
//   "Tstamp": "'2025-03-08 14:30:00'",
//   "Voltage": 220.5,
//   "Current": 2.3,
//   "Power": 700.15,
//   "Energy": 900.75,
//   "Frequency": 50.1,
//   "Power_factor": 0.98
// }

// !! NOTE: Langkah-langkah untuk memproses data dari sensor:
//  1. Periksa apakah semua field yang diperlukan ada dalam data yang diterima.
//     Jika ada field yang hilang, kirim pesan error {Data not valid} dan abaikan data.
//  2. Tambahkan "Device_Id" ke dalam pesan dengan mengambil dari DeviceClientData.DeviceId.
//  3. Jika data valid, publish ke Redis. Jika tidak, lewati proses.

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
