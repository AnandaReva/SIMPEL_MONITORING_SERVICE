/*
format : http://host/handler/process

	exp :  headers: {
				"Content-Type" : "application/json",
				"process" : "get_device_list",
			}
			body : {
				"device_id" : 1,
				"page_size" : 5,
			}
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
	"os"
	"time"

	"github.com/jmoiron/sqlx"
)

var InitPrcs bool = false

// Map proses yang dapat dijalankan
var prcsMap = make(map[string]prcs)

type prcs struct {
	userFunction   func(referenceId string, dbConn *sqlx.DB, userId int64, role string, param map[string]any) utils.ResultFormat
	deviceFunction func(referenceId string, dbConn *sqlx.DB, deviceId int64, param map[string]any) utils.ResultFormat
	class          string
	role           []string
}

func initProcessMap() {
	if InitPrcs {
		return
	}

	// device processes

	prcsMap["get_active_devices"] = prcs{
		userFunction: process.Get_Active_Devices,
		class:        "user",
		role:         []string{"system user", "system admin", "system master"},
	}

	prcsMap["get_device_list"] = prcs{
		userFunction: process.Get_Device_List,
		class:        "user",
		role:         []string{"system user", "system admin", "system master"},
	}

	prcsMap["get_device_detail"] = prcs{
		userFunction: process.Get_Device_Detail,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}

	prcsMap["get_device_activity_list"] = prcs{
		userFunction: process.Get_Device_Activity_List,
		class:        "user",
		role:         []string{"system user", "system admin", "system master"},
	}

	prcsMap["get_dummy_active_devices"] = prcs{
		userFunction: process.Get_Dummy_Active_Devices,
		class:        "user",
		role:         []string{"system user", "system admin", "system master"},
	}

	//device_management

	prcsMap["add_device_data"] = prcs{
		userFunction: process.Add_Device_Data,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}

	prcsMap["update_device_data"] = prcs{
		userFunction: process.Update_Device_Data,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}

	prcsMap["delete_device_data"] = prcs{
		userFunction: process.Delete_Device_Data,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}

	// user processes
	prcsMap["get_user_list"] = prcs{
		userFunction: process.Get_User_List,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}

	prcsMap["get_user_detail"] = prcs{
		userFunction: process.Get_User_Detail,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}

	prcsMap["add_user_data"] = prcs{
		userFunction: process.Add_User_Data,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}

	prcsMap["update_user_data"] = prcs{
		userFunction: process.Update_User_data,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}

	prcsMap["delete_user_data"] = prcs{
		userFunction: process.Delete_User_Data,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}

	prcsMap["get_user_activity_list"] = prcs{
		userFunction: process.Get_User_Activity_List,
		class:        "user",
		role:         []string{"system user", "system admin", "system master"},
	}

	/// device processes
	prcsMap["device_get_data"] = prcs{
		deviceFunction: process.Device_Get_Data,
		class:          "device",
	}

	// report processes
	prcsMap["get_report_available_years"] = prcs{
		userFunction: process.Get_Report_Available_Years,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}

	prcsMap["get_report_year_detail"] = prcs{
		userFunction: process.Get_Report_Year_Detail,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}

	prcsMap["get_report_available_months_by_year"] = prcs{
		userFunction: process.Get_Report_Available_Months_By_Year,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}
	prcsMap["get_report_month_detail"] = prcs{
		userFunction: process.Get_Report_Month_Detail,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}

	/* // report processes
	prcsMap["get_report_year_list_detail"] = prcs{
		userFunction: process.Get_Report_Year_List_Detail,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}
	prcsMap["get_report_month_list"] = prcs{
		userFunction: process.Get_Report_Month_List,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}

	prcsMap["get_report_day_list"] = prcs{
		userFunction: process.Get_Report_Day_List,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}
	prcsMap["get_report_day_detail"] = prcs{
		userFunction: process.Get_Report_Day_Detail,
		class:        "user",
		role:         []string{"system admin", "system master"},
	} */

	prcsMap["get_report_available_day_dates_by_month"] = prcs{
		userFunction: process.Get_Available_DayDates_By_Month,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}

	prcsMap["get_report_available_hours_by_day_date"] = prcs{
		userFunction: process.Get_Report_Available_Hours_By_Day_Date,
		class:        "user",
		role:         []string{"system admin", "system master"},
	}

	// prcsMap["get_csv_month_data"] = prcs{
	// 	userFunction: process.Get_Report_Month_List,
	// 	class:        "user",
	// 	role:         []string{"system admin", "system master"},
	// }

	// prcsMap["get_csv_year_data"] = prcs{
	// 	userFunction: process.Get_Csv_Year_Data,
	// 	class:        "user",
	// 	role:         []string{"system admin", "system master"},
	// }

	InitPrcs = true
}

type UserInfo struct {
	UserID      int64  `db:"user_id"`
	UserRole    string `db:"user_role"`
	SessionID   string
	SessionHash string `db:"session_hash"`
}

type DeviceInfo struct {
	DeviceId   int64  `db:"id"`
	DeviceName string `db:"name"`
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
		logger.Warning(referenceId, "WARNING - Process - Method Not Allowed")
		return
	}

	processName := r.Header.Get("process")
	if processName == "" {
		logger.Warning(referenceId, "PROCESS - Warning - Missing process name in header")
		utils.Response(w, utils.ResultFormat{ErrorCode: "400001", ErrorMessage: "Invalid request"})
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
		logger.Error(referenceId, "PROCESS - ERROR - Database connection error:", err)
		utils.Response(w, utils.ResultFormat{ErrorCode: "500000", ErrorMessage: "Internal server error"})
		return
	}

	defer db.ReleaseConnection()

	switch prc.class {
	case "user":
		logger.Info(referenceId, "PROCESS - INFO - Executing USER class process")

		userInfo, err := validateSession(r, conn, referenceId)
		if err != nil {
			logger.Warning(referenceId, "PROCESS - WARNING - error when validating user session: ", err)
			utils.Response(w, utils.ResultFormat{ErrorCode: "401000", ErrorMessage: "Unauthorized"})
			return
		}

		logger.Info(referenceId, fmt.Sprintf("INFO - USER ID: %d, USER ROLE: %s", userInfo.UserID, userInfo.UserRole))

		if len(prc.role) > 0 && !utils.Contains(prc.role, userInfo.UserRole) {
			logger.Warning(referenceId, "PROCESS - WARNING - User does not have the required role")
			utils.Response(w, utils.ResultFormat{ErrorCode: "403000", ErrorMessage: "Forbidden"})
			return
		}

		param, err := utils.Request(r)
		if err != nil {
			logger.Error(referenceId, "ERROR - Failed to parse request body:", err)
			utils.Response(w, utils.ResultFormat{ErrorCode: "50001", ErrorMessage: "Internal server error"})
			return
		}

		if err := validateSignature(r, param, userInfo, referenceId); err != nil {
			logger.Warning(referenceId, "WARNING -", err)
			utils.Response(w, utils.ResultFormat{ErrorCode: "401002", ErrorMessage: "Unauthorized"})
			return
		}

		logger.Info(referenceId, "INFO - SIGNATURE VALID")

		result := prc.userFunction(referenceId, conn, userInfo.UserID, userInfo.UserRole, param)
		utils.Response(w, result)

	case "device":
		logger.Info(referenceId, "PROCESS - INFO - Executing DEVICE class process")

		deviceInfo, err := validateDevice(r, conn, referenceId)
		if err != nil {
			logger.Error(referenceId, "ERROR - Device validation failed: ", err)

			var code string
			switch err.Error() {
			case "invalid request":
				code = "400001"
			case "unauthorized":
				code = "401001"
			case "internal server error":
				code = "500001"
			default:
				code = "401000"
			}

			utils.Response(w, utils.ResultFormat{
				ErrorCode:    code,
				ErrorMessage: err.Error(),
			})
			return
		}

		logger.Info(referenceId, fmt.Sprintf("INFO - DEVICE ID: %d, DEVICE NAME: %s", deviceInfo.DeviceId, deviceInfo.DeviceName))

		param, _ := utils.Request(r)
		result := prc.deviceFunction(referenceId, conn, deviceInfo.DeviceId, param)
		utils.Response(w, result)

	default:
		logger.Warning(referenceId, "WARNING - PROCESS - INFO - Class do not exist", prc.class)
		utils.Response(w, utils.ResultFormat{ErrorCode: "400003", ErrorMessage: "Invalid request"})
	}
}
func validateSession(r *http.Request, conn *sqlx.DB, referenceId string) (UserInfo, error) {
	sessionID := r.Header.Get("session_id")
	if sessionID == "" {
		return UserInfo{}, errors.New("unauthorized: Missing session information")
	}

	var userInfo UserInfo
	query := `
		SELECT su.id AS user_id, su.role AS user_role, ss.session_hash 
		FROM sysuser.user su 
		LEFT JOIN sysuser.session ss ON su.id = ss.user_id 
		WHERE ss.session_id = $1`
	err := conn.QueryRow(query, sessionID).Scan(&userInfo.UserID, &userInfo.UserRole, &userInfo.SessionHash)
	if err != nil {
		return UserInfo{}, errors.New("unauthorized: Invalid session")
	}

	return userInfo, nil
}
func validateSignature(r *http.Request, param map[string]any, userInfo UserInfo, referenceId string) error {
	bodyRequest, err := json.Marshal(param)
	if err != nil {
		return errors.New("failed to marshal request body")
	}

	message := string(bodyRequest)
	computedSignature, _ := crypto.GenerateHMAC(message, userInfo.SessionHash)
	clientSignature := r.Header.Get("signature")

	logger.Info(referenceId, "INFO - message: ", message)
	logger.Info(referenceId, "INFO - key: ", userInfo.SessionHash)
	logger.Debug(referenceId, "DEBUG - Computed signature: ", computedSignature)
	logger.Debug(referenceId, "DEBUG - Client signature: ", clientSignature)

	if computedSignature != clientSignature {
		return errors.New("unauthorized: error signature dont match")
	}

	return nil
}
func validateDevice(r *http.Request, conn *sqlx.DB, referenceId string) (DeviceInfo, error) {
	param, err := utils.Request(r)
	if err != nil {
		logger.Error(referenceId, "ERROR - Failed to parse request body:", err)
		return DeviceInfo{}, errors.New("internal server error")
	}

	deviceName, okName := param["name"].(string)
	devicePassword, okPass := param["password"].(string)
	if !okName || !okPass || deviceName == "" || devicePassword == "" {
		logger.Error(referenceId, "ERROR - Device_Get_Data - Invalid device name or password")
		return DeviceInfo{}, errors.New("invalid request")
	}

	var (
		salt, saltedPassword string
		deviceId             int64
	)
	query := `SELECT id, salt, salted_password FROM device.unit WHERE name = $1`
	err = conn.QueryRow(query, deviceName).Scan(&deviceId, &salt, &saltedPassword)
	if err != nil {
		logger.Error(referenceId, "ERROR - Device_Get_Data - Device not found or DB error:", err)
		return DeviceInfo{}, errors.New("unauthorized")
	}

	key := os.Getenv("KEY")
	if key == "" {
		logger.Error(referenceId, "ERROR - Device_Get_Data - KEY is not set")
		return DeviceInfo{}, errors.New("internal server error")
	}

	plainTextPassword, err := crypto.DecryptAES256(saltedPassword, salt, key)
	if err != nil {
		logger.Error(referenceId, "ERROR - Device_Get_Data - Failed to decrypt password: ", err)
		return DeviceInfo{}, errors.New("internal server error")
	}

	if plainTextPassword != devicePassword {
		logger.Warning(referenceId, "WARNING - Device_Get_Data - Password invalid")
		return DeviceInfo{}, errors.New("unauthorized")
	}

	return DeviceInfo{DeviceId: deviceId, DeviceName: deviceName}, nil
}
