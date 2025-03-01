/*
format : http://host/handler/process
exp :  http://localhost:5000/device/register_device
*/
package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"monitoring_service/crypto"
	"monitoring_service/db"
	"monitoring_service/logger"
	"monitoring_service/process"
	"monitoring_service/utils"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"
)

var InitPrcs bool = false

// Map proses yang dapat dijalankan
var prcsMap = make(map[string]prcs)

type prcs struct {
	function func(referenceId string, dbConn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat
	class    string
	role     []string /* ["guest", "system user", "system admin", "system master"],   */
}

func initProcessMap() {
	if InitPrcs {
		return
	}

	prcsMap["register_device"] = prcs{
		function: process.Register_Device,
		class:    "device",
		role:     []string{"system admin", "system master"},
	}

	prcsMap["get_active_devices"] = prcs{
		function: process.Get_Active_Devices,
		class:    "user",
		role:     []string{"system user", "system admin", "system master"},
	}

	prcsMap["get_device_list"] = prcs{
		function: process.Get_Device_List,
		class:    "user",
		role:     []string{"system user", "system admin", "system master"},
	}

	prcsMap["get_dummy_active_devices"] = prcs{
		function: process.Get_Dummy_Active_Devices,
		class:    "user",
		role:     []string{"system user", "system admin", "system master"},
	}

	InitPrcs = true
}

type UserInfo struct {
	UserID      int64  `db:"user_id"`
	UserRole    string `db:"user_role"`
	SessionID   string
	SessionHash string `db:"session_hash"`
}

func Process(w http.ResponseWriter, r *http.Request) {
	var ctxKey HTTPContextKey = "requestID"
	referenceId, ok := r.Context().Value(ctxKey).(string)
	if !ok {
		referenceId = "unknown"
	}

	startTime := time.Now()
	defer func() {
		logger.Debug(referenceId, "DEBUG - Execution completed in:", time.Since(startTime))
	}()

	initProcessMap()

	if r.Method != http.MethodPost {
		utils.Response(w, utils.ResultFormat{ErrorCode: "405000", ErrorMessage: "Method not allowed"})
		return
	}

	processName := r.Header.Get("process")
	if processName == "" {
		utils.Response(w, utils.ResultFormat{ErrorCode: "400001", ErrorMessage: "Invalid request"})
		logger.Error(referenceId, "PROCESS - ERROR - Missing process name in header")
		return
	}
	logger.Info(referenceId, "PROCECSS - INFO - process_name: ", processName)

	prc, exists := prcsMap[processName]
	if !exists {
		utils.Response(w, utils.ResultFormat{ErrorCode: "400002", ErrorMessage: "Invalid request"})
		return
	}

	conn, err := db.GetConnection()
	if err != nil {
		utils.Response(w, utils.ResultFormat{ErrorCode: "500000", ErrorMessage: "Internal server error"})
		logger.Error(referenceId, "PROCESS - ERROR - Database connection error:", err)
		return
	}
	defer db.ReleaseConnection()

	// Validasi sesi pengguna
	userInfo, err := validateSession(r, conn, referenceId)
	if err != nil {
		utils.Response(w, utils.ResultFormat{ErrorCode: "401000", ErrorMessage: "Unauthorized"})
		logger.Error(referenceId, "PROCESS - ERROR - error when validating user session : ", err)
		return
	}

	userIndoLogStr := fmt.Sprintf("USER ID : %d , USER ROLE: %s", userInfo.UserID, userInfo.UserRole)

	logger.Info(referenceId, "INFO - ", userIndoLogStr)

	// Validasi peran pengguna
	if len(prc.role) > 0 && !utils.Contains(prc.role, userInfo.UserRole) {
		utils.Response(w, utils.ResultFormat{ErrorCode: "403000", ErrorMessage: "Forbidden"})
		logger.Error(referenceId, "PROCESS ERROR - User does not have the required role")
		return
	}

	var param map[string]interface{}
	param, err = utils.Request(r)
	if err != nil {
		utils.Response(w, utils.ResultFormat{ErrorCode: "400003", ErrorMessage: "Invalid request"})
		logger.Error(referenceId, "ERROR - Failed to parse request body:", err)
		return
	}

	// Validasi signature
	if err := validateSignature(r, param, userInfo, referenceId); err != nil {
		utils.Response(w, utils.ResultFormat{ErrorCode: "401002", ErrorMessage: "Unauthorized"})
		logger.Error(referenceId, "ERROR -", err)
		return
	}
	logger.Info(referenceId, "INFO - SIGNATURE VALID")

	result := prc.function(referenceId, conn, userInfo.UserID, userInfo.UserRole, param)
	utils.Response(w, result)
}

func validateSession(r *http.Request, conn *sqlx.DB, referenceId string) (UserInfo, error) {
	sessionID := r.Header.Get("session_id")
	if sessionID == "" {
		return UserInfo{}, errors.New("unauthorized: Missing session information")
	}

	var userInfo UserInfo
	query := `SELECT su.id AS user_id, su.role AS user_role, ss.session_hash 
		FROM sysuser.user su LEFT JOIN sysuser.session ss 
		ON su.id = ss.user_id WHERE ss.session_id = $1`
	err := conn.QueryRow(query, sessionID).Scan(&userInfo.UserID, &userInfo.UserRole, &userInfo.SessionHash)
	if err != nil {
		return UserInfo{}, errors.New("unauthorized: Invalid session")
	}
	return userInfo, nil
}

func validateSignature(r *http.Request, param map[string]interface{}, userInfo UserInfo, referenceId string) error {
	bodyRequest, err := json.Marshal(param)
	if err != nil {
		return errors.New("failed to marshal request body")
	}

	message := string(bodyRequest)
	computedSignature, _ := crypto.GenerateHMAC(message, userInfo.SessionHash)
	clientSignature := r.Header.Get("signature")

	if computedSignature != clientSignature {
		return errors.New("unauthorized: Invalid signature")
	}

	return nil
}
