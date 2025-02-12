/*
format : http://host/handler/process
exp :  http://localhost:5000/device/register_device
*/
package handlers

import (
	"encoding/json"
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
		function:  process.Register_device,
		class:     "device",
		Need_hash: false,
	}

	/* 	prcsMap["connect_device"] = prcs{
		function:  process.Connect_device,
		class:     "device",
		Need_hash: false,
	} */

	/* prcsMap["get_device_list"] = prcs{
		function:  process.GetDeviceData,
		class:     "device",
		Need_hash: false,
	}

	prcsMap["update_device"] = prcs{
		function:  process.UpdateDevice,
		class:     "device",
		Need_hash: false,
	}

	prcsMap["get_broker_data"] = prcs{
		function:  process.GetBrokerData,
		class:     "broker",
		Need_hash: true,
	}

	prcsMap["get_report_today"] = prcs{
		function: process.GetReportToday,
		class:    "report",
	} */

	InitPrcs = true
}

type UserInfo struct {
	UserID      int64  `db:"user_id"`
	UserRole    string `db:"user_role"`
	SessionID   string
	SessionHash string `db:"session_hash"`
}

/* func Process(w http.ResponseWriter, r *http.Request) {
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

	initProcessMap()

	// Ambil nama proses dari URL path
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 3 {
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400001",
			ErrorMessage: "Missing process name in URL",
		})
		return
	}

	processName := pathParts[2] // Ambil nama proses dari URL (http://host/process/{process_name})

	// Cek apakah processName ada di map proses yang terdaftar
	prc, exists := prcsMap[processName]
	if !exists {
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400002",
			ErrorMessage: "Invalid process name",
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

	var userInfo UserInfo

	// Jika proses membutuhkan autentikasi hash
	if prc.Need_hash {
		sessionID := r.Header.Get("session_id")
		signature := r.Header.Get("signature")
		if sessionID == "" || signature == "" {
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "401000",
				ErrorMessage: "Unauthorized",
			})
			logger.Error(reference_id, "ERROR - Unauthorized: Missing session information")
			return
		}

		query := `SELECT su.id AS user_id, su.role AS user_role, ss.session_secret FROM sysuser.user su 
			LEFT JOIN sysuser.session ss ON su.id = ss.user_id WHERE ss.session_hash = $1`
		err = conn.QueryRow(query, sessionID).Scan(&userInfo.UserID, &userInfo.UserRole, &userInfo.SessionHash)
		if err != nil {
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "401001",
				ErrorMessage: "Unauthorized",
			})
			logger.Error(reference_id, "ERROR - Unauthorized: Invalid session:", err)
			return
		}

		// Parsing request body
		body, err := utils.Request(r)
		if err != nil {
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "400003",
				ErrorMessage: "Invalid request",
			})
			logger.Error(reference_id, "ERROR - Invalid request body:", err)
			return
		}

		// Marshal body ke JSON untuk pengecekan signature
		bodyRequest, err := json.Marshal(body)
		if err != nil {
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "400003",
				ErrorMessage: "Failed to marshal request body",
			})
			logger.Error(reference_id, "ERROR - Failed to marshal request body:", err)
			return
		}

		// Validasi signature
		computedSignature, _ := crypto.GenerateHMAC(string(bodyRequest), userInfo.SessionHash)
		if computedSignature != signature {
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "401002",
				ErrorMessage: "Unauthorized",
			})
			logger.Error(reference_id, "ERROR - Unauthorized: Invalid signature")
			return
		}
	}

	// Mengambil parameter dari request
	param, err := utils.Request(r)
	if err != nil {
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400002",
			ErrorMessage: "Failed to parse parameters",
		})
		return
	}

	// Eksekusi proses yang sesuai dengan function yang telah didaftarkan
	result := prc.function(reference_id, conn, userInfo.UserID, userInfo.UserRole, param)

	// Mengirim response berdasarkan hasil eksekusi proses
	utils.Response(w, result)
}
 */

func Process(w http.ResponseWriter, r *http.Request) {
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

	initProcessMap()

	// Mendapatkan nama proses dari header request
	processName := r.Header.Get("process")
	if processName == "" {
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400001",
			ErrorMessage: "Missing process name in header",
		})
		return
	}

	// Cek apakah processName ada di map proses yang terdaftar
	prc, exists := prcsMap[processName]
	if !exists {
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400002",
			ErrorMessage: "Invalid process name",
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

	var userInfo UserInfo

	// Jika proses membutuhkan autentikasi hash
	if prc.Need_hash {
		sessionID := r.Header.Get("session_id")
		signature := r.Header.Get("signature")
		if sessionID == "" || signature == "" {
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "401000",
				ErrorMessage: "Unauthorized",
			})
			logger.Error(reference_id, "ERROR - Unauthorized: Missing session information")
			return
		}

		query := `SELECT su.id AS user_id, su.role AS user_role, ss.session_secret FROM sysuser.user su
			LEFT JOIN sysuser.session ss ON su.id = ss.user_id WHERE ss.session_hash = $1`
		err = conn.QueryRow(query, sessionID).Scan(&userInfo.UserID, &userInfo.UserRole, &userInfo.SessionHash)
		if err != nil {
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "401001",
				ErrorMessage: "Unauthorized",
			})
			logger.Error(reference_id, "ERROR - Unauthorized: Invalid session:", err)
			return
		}

		// Using Request to parse the request body
		body, err := utils.Request(r)
		if err != nil {
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "400003",
				ErrorMessage: "Invalid request",
			})
			logger.Error(reference_id, "ERROR - Invalid request body:", err)
			return
		}

		// Marshal the body to JSON string for signature check
		bodyRequest, err := json.Marshal(body)
		if err != nil {
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "400003",
				ErrorMessage: "Failed to marshal request body",
			})
			logger.Error(reference_id, "ERROR - Failed to marshal request body:", err)
			return
		}

		// Convert bodyRequest to string for signature comparison
		computedSignature, _ := crypto.GenerateHMAC(string(bodyRequest), userInfo.SessionHash)
		if computedSignature != signature {
			utils.Response(w, utils.ResultFormat{
				ErrorCode:    "401002",
				ErrorMessage: "Unauthorized",
			})
			logger.Error(reference_id, "ERROR - Unauthorized: Invalid signature")
			return
		}
	}

	// Mengambil parameter dari request
	param, err := utils.Request(r)
	if err != nil {
		utils.Response(w, utils.ResultFormat{
			ErrorCode:    "400002",
			ErrorMessage: "Failed to parse parameters",
		})
		return
	}

	// Eksekusi proses yang sesuai dengan function yang telah didaftarkan
	result := prc.function(reference_id, conn, userInfo.UserID, userInfo.UserRole, param)

	// Mengirim response berdasarkan hasil eksekusi proses
	utils.Response(w, result)
}

