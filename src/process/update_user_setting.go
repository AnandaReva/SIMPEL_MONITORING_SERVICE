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

	// Mulai transaksi
	tx, err := conn.Beginx()
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_User_Settings - Failed to begin transaction:", err)
		result.ErrorCode = "500001"
		result.ErrorMessage = "Transaction start failed"
		return result
	}
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p) // lanjutkan panic setelah rollback
		}
	}()

	// Ambil data lama dari database
	var existingData map[string]any
	querySelect := `SELECT data FROM sysuser."user" WHERE id = $1`
	err = tx.Get(&existingData, querySelect, user_id)
	if err != nil {
		tx.Rollback()
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
		tx.Rollback()
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
	_, err = tx.Exec(queryUpdate, jsonData, lastTimestamp, user_id)
	if err != nil {
		tx.Rollback()
		logger.Error(referenceId, "ERROR - Update_User_Settings - Update failed:", err)
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Ambil kembali data user yang baru
	var newUserDetailedData map[string]any
	queryToGetUserDetailData := `SELECT data FROM sysuser."user" WHERE id = $1`
	err = tx.Get(&newUserDetailedData, queryToGetUserDetailData, user_id)
	if err != nil {
		tx.Rollback()
		logger.Error(referenceId, "ERROR - Update_User_Settings - Failed to retrieve updated user data:", err)
		result.ErrorCode = "500004"
		result.ErrorMessage = "Failed to retrieve updated data"
		return result
	}

	// Commit transaksi
	if err = tx.Commit(); err != nil {
		logger.Error(referenceId, "ERROR - Update_User_Settings - Commit failed:", err)
		result.ErrorCode = "500005"
		result.ErrorMessage = "Transaction commit failed"
		return result
	}

	logger.Info(referenceId, "INFO - Update_User_Settings - Update success")
	result.Payload["status"] = "success"
	result.Payload["new_user_detailed_data"] = newUserDetailedData
	return result
}