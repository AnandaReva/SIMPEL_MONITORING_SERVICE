package handlers

import (
	"database/sql"
	"monitoring_service/crypto"
	"monitoring_service/db"
	"monitoring_service/logger"
	"monitoring_service/pubsub"
	"monitoring_service/utils"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
) /*

 select * from sysuser.user;
 username |  full_name  | st |       salt       |                          saltedpassword                          | data | id | role
----------+-------------+----+------------------+------------------------------------------------------------------+------+----+-------
 master   | Master User |  1 | 3w1WjEyeRFiYiQaB | 3ee0c18f1887594557e3e2884e9d8a9c54cb5371e42769a3f4b12dda522ec5cd | {}   | 12 | guest
(1 row)


simple=> select * from sysuser.session;
    session_id    | user_id |                           session_hash                           |   tstamp   | st | last_ms_tstamp | last_sequence
------------------+---------+------------------------------------------------------------------+------------+----+----------------+---------------
 npGnYG2IUWnDauIa |       1 | ad8c333e1141e78d4d87ff2f3e55d80529094fe6f21685c054557f3beb2e39fa | 1738681697 |  1 |                |
 Vp9x2QkGfNl20r3P |       9 | 2ed6d9f9c3d3d6267df55ca5dc14ebf676f0b9b729662324e2a1c99c203275c4 | 1738725661 |  1 |                |
 2wsgprZ2J80vDcfp |      12 | fe38f2559bf1962363fcf6620a751c0101b7446f91e91831a4342299f4494cb5 | 1738944883 |  1 |                |
(3 rows)
*/

// Token = HMAC-SHA256((session_id + device_id), session_hash)

//exp param : ws://localhost:5001/user-connect?token={token}&session_id={session_id}&device_id={device_id}

var userUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type UserClientData struct {
	UserID   int64  `db:"user_id"`
	Username string `db:"username"`
	Role     string `db:"role"`
}

func Users_Create_Conn(w http.ResponseWriter, r *http.Request) {
	ctxKey := "requestID"
	referenceId, ok := r.Context().Value(ctxKey).(string)
	if !ok {
		referenceId = "unknown"
	}

	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		logger.Debug(referenceId, "DEBUG - Users_Create_Conn - Execution completed in:", duration)
	}()

	// Ambil token, session_id dan device_id dari query parameter
	token := r.URL.Query().Get("token")
	sessionID := r.URL.Query().Get("session_id")
	deviceID := r.URL.Query().Get("device_id")

	if token == "" || sessionID == "" || deviceID == "" {
		logger.Error(referenceId, "WARN - Users_Create_Conn - Missing required parameters")
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400001",
			ErrorMessage: "Invalid request",
		})
		return
	}

	logger.Info(referenceId, "token", token)
	logger.Info(referenceId, "session_id", sessionID)
	logger.Info(referenceId, "device_id", deviceID)

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

	// Ambil session_hash dari database
	var sessionHash string

	var userData UserClientData
	errQuery1 := conn.QueryRow("SELECT session_hash, user_id FROM sysuser.session WHERE session_id = $1", sessionID).Scan(&sessionHash, &userData.UserID)
	if errQuery1 != nil {
		if errQuery1 == sql.ErrNoRows {
			logger.Error(referenceId, "WARN - Users_Create_Conn - Session not found: ", errQuery1)
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "401001",
				ErrorMessage: "Unauthorized",
			})
			return
		}
		logger.Error(referenceId, "ERROR - Users_Create_Conn - Database errQuery1 failed: ", errQuery1)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500001",
			ErrorMessage: "Internal Server Error",
		})
		return
	}

	logger.Info(referenceId, "INFO - Users_Create_Conn - user id found: ", userData.UserID)

	// Susun token yang harus diverifikasi
	generatedToken, errHmac := crypto.GenerateHMAC(sessionID+deviceID, sessionHash)
	logger.Info(referenceId, "generatedToken: ", generatedToken)

	if errHmac != "" {
		logger.Error(referenceId, "WARN - Users_Create_Conn - error generating HMAC: ", errHmac)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500002",
			ErrorMessage: "Internal Server Error",
		})
		return

	}

	// Verifikasi token
	if generatedToken != token {
		logger.Error(referenceId, "WARN - Users_Create_Conn - Invalid token")
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "401002",
			ErrorMessage: "Unauthorized",
		})
		return
	}

	// Dapatkan informasi pengguna dari session_id

	errQuery2 := conn.QueryRow("SELECT username, role FROM sysuser.user WHERE id = $1", userData.UserID).
		Scan(&userData.Username, &userData.Role)

	if errQuery2 != nil {
		logger.Error(referenceId, "ERROR - Users_Create_Conn - Failed to retrieve user data:", errQuery2)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500002",
			ErrorMessage: "Internal Server Error",
		})
		return
	}

	// Dapatkan WebSocketHub
	hub, err := pubsub.GetWebSocketHub(referenceId)
	if err != nil {
		logger.Error(referenceId, "ERROR - Users_Create_Conn - Failed to initialize WebSocketHub:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500003",
			ErrorMessage: "Internal Server Error",
		})
		return
	}

	// Upgrade ke WebSocket
	wsConn, err := userUpgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(referenceId, "ERROR - Users_Create_Conn - WebSocket upgrade failed:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500004",
			ErrorMessage: "Internal Server Error",
		})
		return
	}

	// Tambah user ke hub
	hub.AddUserToWebsocket(referenceId, wsConn, userData.UserID, userData.Username, userData.Role)

	// Subscribe user ke channel Redis berdasarkan deviceId
	hub.SubscribeUserToChannel(referenceId, wsConn, deviceID)

	go func() {
		defer func() {
			hub.RemoveUserFromWebSocket(referenceId, wsConn)
			logger.Info(referenceId, "INFO - Users_Create_Conn - WebSocket connection closed")
		}()

		for {
			_, _, err := wsConn.ReadMessage()
			if err != nil {
				logger.Error(referenceId, "ERROR - Users_Create_Conn - WebSocket read error:", err)
				break
			}
		}
	}()

	logger.Info(referenceId, "INFO - WebSocket connection established for user:", userData.Username)
}
