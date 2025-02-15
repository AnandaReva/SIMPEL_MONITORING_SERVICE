package handlers

import (
	"database/sql"
	"fmt"
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

var userUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type UserClientData struct {
	UserID   int64  `db:"user_id"`
	Username string `db:"username"`
	Role     string `db:"role"`
}

func Users_Create_Conn(w http.ResponseWriter, r *http.Request) {
	// Mendapatkan reference ID untuk logging
	ctxKey := "requestID"
	referenceID, ok := r.Context().Value(ctxKey).(string)
	if !ok {
		referenceID = "unknown"
	}

	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		logger.Debug(referenceID, "DEBUG - Users_Create_Conn - Execution completed in:", duration)
	}()

	// Ambil session dari header
	sessionId := r.Header.Get("session_id")
	sessionHash := r.Header.Get("session_hash")
	deviceId := r.Header.Get("device_id")

	if sessionId == "" || sessionHash == "" || deviceId == "" {
		logger.Error(referenceID, "ERROR - Users_Create_Conn - Missing credentials")
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400000",
			ErrorMessage: "Invalid Request",
		})
		return
	}

	logstr := fmt.Sprintf("session_id: %s, session_hash: %s, device_id: %s", sessionId, sessionHash, deviceId)
	logger.Info(referenceID, "INFO - Users_Create_Conn - ", logstr)

	// Dapatkan koneksi database
	conn, err := db.GetConnection()
	if err != nil {
		logger.Error(referenceID, "ERROR - Users_Create_Conn - Failed to get database connection:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500000",
			ErrorMessage: "Internal Server Error",
		})
		return
	}
	defer db.ReleaseConnection()

	var userData UserClientData

	// Ambil credential dari database
	query := `
    SELECT su.id AS user_id, su.username, su.role 
    FROM sysuser.user su 
    INNER JOIN sysuser.session ss ON su.id = ss.user_id 
    WHERE ss.session_id = $1 
    AND ss.session_hash = $2 
    AND su.st = 1 
    AND ss.st = 1
`
	err = conn.QueryRow(query, sessionId, sessionHash).Scan(&userData.UserID, &userData.Username, &userData.Role)

	if err != nil {
		if err == sql.ErrNoRows {
			logger.Error(referenceID, "ERROR - Users_Create_Conn - Invalid session_id or session_hash")
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "401000",
				ErrorMessage: "Unauthorized",
			})
		} else {
			logger.Error(referenceID, "ERROR - Users_Create_Conn - Database query failed:", err)
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "500002",
				ErrorMessage: "Internal Server Error",
			})
		}
		return
	}

	// Dapatkan WebSocketHub
	hub, err := pubsub.GetWebSocketHub(referenceID)
	if err != nil {
		logger.Error(referenceID, "ERROR - Users_Create_Conn - Failed to initialize WebSocketHub:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500003",
			ErrorMessage: "Internal Server Error",
		})
		return
	}

	// Upgrade ke WebSocket
	wsConn, err := userUpgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(referenceID, "ERROR - Users_Create_Conn - WebSocket upgrade failed:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500003",
			ErrorMessage: "Internal Server Error",
		})
		return
	}

	// Tambah user ke hub
	hub.AddUser(referenceID, wsConn, userData.UserID, userData.Username, userData.Role)

	// Subscribe user ke channel Redis berdasarkan deviceId
	hub.SubscribeUserToChannel(referenceID, wsConn, deviceId)

	go func() {
		defer func() {
			hub.RemoveUser(referenceID, wsConn)
			logger.Info(referenceID, "INFO - Users_Create_Conn - WebSocket connection closed")
		}()

		for {
			_, _, err := wsConn.ReadMessage()
			if err != nil {
				logger.Error(referenceID, "ERROR - Users_Create_Conn - WebSocket read error:", err)
				break
			}
		}
	}()

	logger.Info(referenceID, "INFO - WebSocket connection established for user:", userData.Username)
}
