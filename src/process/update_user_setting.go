package process

import (
	"monitoring_service/logger"
	"monitoring_service/utils"
	"time"

	"github.com/jmoiron/sqlx"
)

/*
!note : table sysuser.user , column data (jsonb)
*/

// Param expected:
// {
//   "change_fields": {
//     "general": { "emission_factor": 0.5 },
//     "account": { "username": "new_username", "email": "new@mail.com", "full_name": "New Name" },
//     "system": { "timezone": "Asia/Jakarta", "theme": "dark", "language": "id" }
//   }
// }

func Update_User_Settings(referenceId string, conn *sqlx.DB, user_id int64, role string, param map[string]any) utils.ResultFormat {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		logger.Debug(referenceId, "DEBUG - Update_User_Settings - Execution completed in", duration)
	}()

	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Update_User_Settings - params:", param)

	// Validasi dan ambil change_fields
	changeFields, ok := param["change_fields"].(map[string]any)
	if !ok || len(changeFields) == 0 {
		logger.Error(referenceId, "ERROR - Update_User_Settings - Invalid change_fields")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid change_fields"
		return result
	}

	// Ambil data lama dari database
	var existingData map[string]any
	querySelect := `SELECT data FROM sysuser."user" WHERE id = $1`
	err := conn.Get(&existingData, querySelect, user_id)
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_User_Settings - Failed to get existing data:", err)
		result.ErrorCode = "500002"
		result.ErrorMessage = "User not found"
		return result
	}

	// Merge existingData dengan changeFields secara rekursif
	merged := utils.MergeJSONB(existingData, changeFields)

	// Konversi hasil merge ke JSON string
	jsonData, err := utils.MapToJSON(merged)
	if err != nil {
		result.ErrorCode = "400002"
		result.ErrorMessage = "Failed to convert change_fields"
		return result
	}

	// Siapkan query update
	lastTimestamp := time.Now().Unix()
	queryUpdate := `
		UPDATE sysuser."user"
		SET data = $1, last_timestamp = $2
		WHERE id = $3
	`

	_, err = conn.Exec(queryUpdate, jsonData, lastTimestamp, user_id)
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_User_Settings - Update failed:", err)
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal server error"
		return result
	}

	logger.Info(referenceId, "INFO - Update_User_Settings - Update success")
	result.Payload["status"] = "success"
	return result
}
