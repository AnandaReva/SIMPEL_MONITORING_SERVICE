package process

import (
	"encoding/json"
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

func Update_User_data(referenceId string, conn *sqlx.DB, editorUserId int64, editorRole string, param map[string]any) utils.ResultFormat {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		logger.Debug(referenceId, "DEBUG - Update_User_data - Execution completed in", duration)
	}()

	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Update_User_data - params:", param)

	userId, ok := param["user_id"].(float64)
	if !ok || userId <= 0 {
		logger.Error(referenceId, "ERROR - Update_User_data - Invalid user_id")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	userIdInt := int64(userId)

	var targetRole string
	err := conn.Get(&targetRole, `SELECT role FROM sysuser.user WHERE id = $1`, userIdInt)
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_User_data - Target user not found")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Hak akses berdasarkan role hierarchy
	if editorUserId != userIdInt {
		// editorRole harus lebih tinggi dari targetRole
		if !canEditUser(editorRole, targetRole) {
			logger.Error(referenceId, "ERROR - Update_User_data - Forbidden by role hierarchy")
			result.ErrorCode = "403001"
			result.ErrorMessage = "Forbidden"
			return result
		}
	} else {
		// Jika user edit dirinya sendiri, larang ganti role
		if editorRole == "system_master" {
			if cf, ok := param["change_fields"].(map[string]any); ok {
				if _, found := cf["role"]; found {
					logger.Error(referenceId, "ERROR - Update_User_data - Master cannot change own role")
					result.ErrorCode = "403001"
					result.ErrorMessage = "Forbidden"
					return result
				}
			}
		}
	}
	changeFields, ok := param["change_fields"].(map[string]any)
	if !ok || len(changeFields) == 0 {
		logger.Error(referenceId, "ERROR - Update_User_data - Invalid change_fields")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}
	// Validasi khusus status (jika ada)
	if val, ok := changeFields["status"]; ok {
		statusFloat, ok := val.(float64)
		if !ok || (statusFloat != 0 && statusFloat != 1) {
			logger.Error(referenceId, "ERROR - Update_User_data - Invalid status value")
			result.ErrorCode = "400002"
			result.ErrorMessage = "Invalid status value"
			return result
		}

		// Konversi ke int dan update dengan key baru "st"
		changeFields["st"] = int(statusFloat)
		delete(changeFields, "status") // Hapus key lama
	}

	// Validasi role (jika ada)
	if newRole, ok := changeFields["role"].(string); ok {
		if targetRole == "system_admin" && editorRole != "system_master" {
			logger.Error(referenceId, "ERROR - Update_User_data - Cannot change role of system_admin")
			result.ErrorCode = "403001"
			result.ErrorMessage = "Forbidden"
			return result
		}
		if newRole == "system_admin" && editorRole != "system_master" {
			logger.Error(referenceId, "ERROR - Update_User_data - Only master can promote to admin")
			result.ErrorCode = "403001"
			result.ErrorMessage = "Forbidden"
			return result
		}
	}

	// Siapkan query UPDATE dari semua field yang dikirim
	updateFields := []string{}
	updateValues := []any{}
	i := 1
	for key, val := range changeFields {
		// Jika tipe val adalah map[string]interface{}, marshal ke JSON string
		if m, ok := val.(map[string]interface{}); ok {
			jsonVal, err := json.Marshal(m)
			if err != nil {
				logger.Error(referenceId, "ERROR - Update_User_data - JSON Marshal failed:", err)
				result.ErrorCode = "500002"
				result.ErrorMessage = "Failed to encode JSON"
				return result
			}
			val = string(jsonVal)
		}

		updateFields = append(updateFields, fmt.Sprintf("%s = $%d", key, i))
		updateValues = append(updateValues, val)
		i++
	}

	lastTimestamp := time.Now().Unix()
	updateFields = append(updateFields, fmt.Sprintf("last_timestamp = $%d", i))
	updateValues = append(updateValues, lastTimestamp)
	i++

	updateQuery := fmt.Sprintf("UPDATE sysuser.user SET %s WHERE id = $%d", strings.Join(updateFields, ", "), i)
	updateValues = append(updateValues, userIdInt)

	_, err = conn.Exec(updateQuery, updateValues...)
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_User_data - Update failed:", err)
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	logger.Info(referenceId, "INFO - Update_User_data - Update success")
	result.Payload["status"] = "success"
	return result

}

func canEditUser(editorRole, targetRole string) bool {
	hierarchy := map[string]int{
		"system master": 3,
		"system admin":  2,
		"system user":   1,
	}

	editorLevel, ok1 := hierarchy[editorRole]
	targetLevel, ok2 := hierarchy[targetRole]

	if !ok1 || !ok2 {
		return false
	}

	return editorLevel > targetLevel
}
