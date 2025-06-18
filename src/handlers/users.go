package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
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

// Token = HMAC-SHA256((session_id + session_hash), session_hash)

//exp param : ws://localhost:5001/user-connect?clientToken={clientToken}&session_id={session_id}

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

	// Ambil parameter
	clientToken := r.URL.Query().Get("token")
	sessionID := r.URL.Query().Get("session_id")
	if clientToken == "" || sessionID == "" {
		logger.Error(referenceId, "WARN - Users_Create_Conn - Missing required parameters")
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400001",
			ErrorMessage: "Invalid request",
		})
		return
	}
	logger.Info(referenceId, "Users_Create_Conn - clientToken: ", clientToken)
	logger.Info(referenceId, "Users_Create_Conn - session_id: ", sessionID)

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

	//logger.Debug(referenceId, "DEBUG - Device_Create_Conn - Failed to get database connection:", err)

	var sessionHash string
	var userData UserClientData
	errQuery1 := conn.QueryRow("SELECT session_hash, user_id FROM sysuser.session WHERE session_id = $1", sessionID).Scan(&sessionHash, &userData.UserID)
	if errQuery1 != nil {
		if errQuery1 == sql.ErrNoRows {
			logger.Warning(referenceId, "WARN - Users_Create_Conn - Session not found: ", errQuery1)
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
	logger.Info(referenceId, "INFO - Users_Create_Conn - user data found : ", fmt.Sprintf("session_hash: %s, user_id: %d", sessionHash, userData.UserID))

	// Generate clientToken untuk validasi
	message := fmt.Sprintf(sessionID + sessionHash)
	generatedToken, errHmac := crypto.GenerateHMAC(message, sessionHash)
	if errHmac != nil {
		logger.Error(referenceId, "WARN - Users_Create_Conn - error generating HMAC: ", errHmac)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500002",
			ErrorMessage: "Internal Server Error",
		})
		return
	}

	logger.Debug(referenceId, "DEBUG - Users_Create_Conn - Generated Token: ", generatedToken)
	logger.Debug(referenceId, "DEBUG - Users_Create_Conn - Client Token: ", clientToken)

	if generatedToken != clientToken {
		logger.Error(referenceId, "WARN - Users_Create_Conn - Invalid clientToken")
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "401002",
			ErrorMessage: "Unauthorized",
		})
		return
	}

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

	// Hub WebSocket
	hub, err := pubsub.GetWebSocketHub(referenceId)
	if err != nil {
		logger.Error(referenceId, "ERROR - Users_Create_Conn - Failed to initialize WebSocketHub:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500003",
			ErrorMessage: "Internal Server Error",
		})
		return
	}

	var wsConn *websocket.Conn

	wsConn, err = userUpgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(referenceId, "ERROR - Users_Create_Conn - WebSocket upgrade failed:", err)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500004",
			ErrorMessage: "Internal Server Error",
		})
		return
	}
	logger.Info(referenceId, "INFO - WebSocket connection upgraded successfully")
	// Cek apakah user sudah punya koneksi WebSocket
	existingConn, ok := hub.UserConn[userData.UserID]
	logger.Debug(referenceId, "DEBUG - Users_Create_Conn - Existing conn: ", existingConn)
	if ok && existingConn != nil {
		hub.RemoveUserFromWebSocket(referenceId, existingConn, userData.UserID)
		existingConn.Close()
	}

	errAddUser := hub.AddUserToWebSocket(referenceId, wsConn, userData.UserID, userData.Username, userData.Role)
	if errAddUser != nil {
		wsConn.Close()
		logger.Error(referenceId, "ERROR - Users_Create_Conn - Failed to add user to WebSocket hub:", errAddUser)
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "500005",
			ErrorMessage: "Internal Server Error",
		})
		return
	}
	// Reader routine
	go func() {
		hub.Mu.Lock()
		userClient, ok := hub.Users[wsConn]
		hub.Mu.Unlock()
		if !ok {
			logger.Error(referenceId, "ERROR - WebSocket handler - User client not found for connection")
			return
		}
		defer func() {
			hub.RemoveUserFromWebSocket(referenceId, wsConn, userData.UserID)
			logger.Info(referenceId, "INFO - Users_Create_Conn - WebSocket connection closed")
		}()

		for {
			_, msg, err := wsConn.ReadMessage()
			if err != nil {
				logger.Error(referenceId, "ERROR - Users_Create_Conn - WebSocket read error:", err)
				break
			}

			logger.Debug(referenceId, fmt.Sprintf("DEBUG - Users_Create_Conn - incoming message from user: %d: ,  %+v", userData.UserID, msg))

			var incoming struct {
				Type     string `json:"type"`
				DeviceID int64  `json:"device_id"`
			}

			if err := json.Unmarshal(msg, &incoming); err != nil {
				logger.Warning(referenceId, fmt.Sprintf("WARNING - Users_Create_Conn - Invalid message format: %s", string(msg)))
				continue
			}

			if incoming.DeviceID == 0 || incoming.Type == "" {
				logger.Warning(referenceId, fmt.Sprintf("WARNING - Users_Create_Conn - Missing required fields in message: %s", string(msg)))
				continue
			}

			switch incoming.Type {
			case "subscribe":
				if err := hub.SubscribeUserToChannel(referenceId, wsConn, userData.UserID, incoming.DeviceID, conn, &hub.Mu); err != nil {
					logger.Error(referenceId, "ERROR - Users_Create_Conn - Subscribe failed:", err)

					// Kirim response error ke client
					errMsg := map[string]any{
						"type":      "subscribe_response",
						"status":    "error",
						"message":   "Failed to subscribe to device",
						"device_id": incoming.DeviceID,
					}
					if writeErr := userClient.SafeWriteJSON(errMsg); writeErr != nil {
						logger.Error(referenceId, "ERROR - Users_Create_Conn - Failed to send error message to client:", writeErr)
					}
				} else {

					successMsg := map[string]any{
						"type":      "subscribe_response",
						"status":    "success",
						"message":   "Successfully subscribe to device",
						"device_id": incoming.DeviceID,
					}
					_ = userClient.SafeWriteJSON(successMsg)

				}

			case "unsubscribe":
				channel := fmt.Sprintf("device:%d", incoming.DeviceID)

				if err := hub.UnsubscribeUserFromChannel(referenceId, wsConn, channel); err != nil {
					logger.Error(referenceId, "ERROR - Users_Create_Conn - Unsubscribe failed:", err)

					// Kirim response error ke client
					errMsg := map[string]any{
						"type":      "unsubscribe_response",
						"status":    "error",
						"message":   "Failed to unsubscribe from device",
						"device_id": incoming.DeviceID,
					}
					if writeErr := userClient.SafeWriteJSON(errMsg); writeErr != nil {
						logger.Error(referenceId, "ERROR - Users_Create_Conn - Failed to send unsubscribe error to client:", writeErr)
					}
				} else {
					logger.Info(referenceId, fmt.Sprintf("User %d successfully unsubscribed from device ID %d", userData.UserID, incoming.DeviceID))

					successMsg := map[string]any{
						"type":      "unsubscribe_response",
						"status":    "success",
						"message":   "Successfully unsubscribed from device",
						"device_id": incoming.DeviceID,
					}
					if writeErr := userClient.SafeWriteJSON(successMsg); writeErr != nil {
						logger.Error(referenceId, "ERROR - Users_Create_Conn - Failed to send unsubscribe success message to client:", writeErr)
					}
				}

			case "restart":
				err := hub.RestartDevice(referenceId, incoming.DeviceID)
				if err != nil {
					logger.Error(referenceId, fmt.Sprintf("ERROR - Failed to restart device ID %d: %v", incoming.DeviceID, err))
					break
				}
				logger.Info(referenceId, fmt.Sprintf("INFO - Restart command sent to device ID %d", incoming.DeviceID))

			case "deep_sleep":
				err := hub.DeepSleepDevice(referenceId, incoming.DeviceID)
				if err != nil {
					logger.Error(referenceId, fmt.Sprintf("ERROR - Failed to send deep sleep to device ID %d: %v", incoming.DeviceID, err))
					break
				}
				logger.Info(referenceId, fmt.Sprintf("INFO - Deep sleep command sent to device ID %d", incoming.DeviceID))

			default:
				logger.Warning(referenceId, fmt.Sprintf("WARNING - Users_Create_Conn - Unknown message type: %s", incoming.Type))
			}
		}
	}()

	logger.Info(referenceId, "INFO - WebSocket connection established for user:", userData.Username)

}
