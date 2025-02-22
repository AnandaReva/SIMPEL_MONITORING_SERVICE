/*
format : http://host/handler/process
exp :  http://localhost:5000/device/register_device
*/
package handlers

import (
	"encoding/json"
	"errors"
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
	function  func(reference_id string, dbConn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat
	class     string
	Need_hash bool
}

func initProcessMap() {
	if InitPrcs {
		return
	}

	prcsMap["register_device"] = prcs{
		function:  process.Register_Device,
		class:     "device",
		Need_hash: false,
	}

	prcsMap["get_active_devices"] = prcs{
		function:  process.Get_Active_Devices,
		class:     "user",
		Need_hash: true,
	}

	prcsMap["get_dummy_active_devices"] = prcs{
		function:  process.Get_Dummy_Active_Devices,
		class:     "user",
		Need_hash: true,
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
	referenceID, ok := r.Context().Value(ctxKey).(string)
	if !ok {
		referenceID = "unknown"
	}

	startTime := time.Now()
	defer func() {
		logger.Debug(referenceID, "DEBUG - Execution completed in:", time.Since(startTime))
	}()

	initProcessMap()

	if r.Method != http.MethodPost {
		utils.Response(w, utils.ResultFormat{ErrorCode: "405000", ErrorMessage: "Method not allowed"})
		return
	}

	processName := r.Header.Get("process")
	if processName == "" {
		utils.Response(w, utils.ResultFormat{ErrorCode: "400001", ErrorMessage: "Invalid request"})
		logger.Error(referenceID, "ERROR - Missing process name in header")
		return
	}

	logger.Info(referenceID, "DEBUG - process name:", processName)

	prc, exists := prcsMap[processName]
	if !exists {
		utils.Response(w, utils.ResultFormat{ErrorCode: "400002", ErrorMessage: "Invalid request"})
		return
	}

	conn, err := db.GetConnection()
	if err != nil {
		utils.Response(w, utils.ResultFormat{ErrorCode: "500000", ErrorMessage: "Internal server error"})
		logger.Error(referenceID, "ERROR - Database connection error:", err)
		return
	}
	defer db.ReleaseConnection()

	var userInfo UserInfo
	var param map[string]interface{}

	// Parsing request body terlebih dahulu
	param, err = utils.Request(r)
	if err != nil {
		utils.Response(w, utils.ResultFormat{ErrorCode: "400003", ErrorMessage: "Invalid request"})
		logger.Error(referenceID, "ERROR - Failed to parse request body:", err)
		return
	}

	// Jika membutuhkan hashing (autentikasi)
	if prc.Need_hash {
		userInfo, err = validateSession(r, conn, referenceID)
		if err != nil {
			utils.Response(w, utils.ResultFormat{ErrorCode: "401000", ErrorMessage: "Invalid request"})
			logger.Error(referenceID, "ERROR -", err)
			return
		}

		if err := validateSignature(r, param, userInfo, referenceID); err != nil {
			utils.Response(w, utils.ResultFormat{ErrorCode: "401002", ErrorMessage: "Invalid request"})
			logger.Error(referenceID, "ERROR -", err)
			return
		}
		logger.Info(referenceID, "INFO - SIGNATURE VALID")
	}

	result := prc.function(referenceID, conn, userInfo.UserID, userInfo.UserRole, param)
	utils.Response(w, result)
}

// validateSession menangani pengecekan sesi pengguna
func validateSession(r *http.Request, conn *sqlx.DB, referenceID string) (UserInfo, error) {
	sessionID := r.Header.Get("session_id")
	signature := r.Header.Get("signature")

	if sessionID == "" || signature == "" {
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

// validateSignature menangani validasi HMAC signature
func validateSignature(r *http.Request, param map[string]interface{}, userInfo UserInfo, referenceID string) error {
	bodyRequest, err := json.Marshal(param)
	if err != nil {
		return errors.New("failed to marshal request body")
	}

	computedSignature, _ := crypto.GenerateHMAC(string(bodyRequest), userInfo.SessionHash)
	clientSignature := r.Header.Get("signature")

	logger.Info(referenceID, "INFO - Computed Signature:", computedSignature)
	logger.Info(referenceID, "INFO - Client Signature:", clientSignature)

	if computedSignature != clientSignature {
		return errors.New("unauthorized: Invalid signature")
	}

	return nil
}
