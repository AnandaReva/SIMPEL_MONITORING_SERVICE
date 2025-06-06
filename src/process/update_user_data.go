package process

import (
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

	userId, ok := param["user_id"].(int64)
	if !ok || userId <= 0 {
		logger.Error(referenceId, "ERROR - Update_User_data - Invalid user_id")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	var targetRole string
	err := conn.Get(&targetRole, `SELECT role FROM sysuser.user WHERE id = $1`, userId)
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_User_data - Target user not found")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Hak akses
	if editorUserId != userId {
		if editorRole != "system_master" && (editorRole != "system_admin" || targetRole == "system_admin") {
			logger.Error(referenceId, "ERROR - Update_User_data - Forbidden")
			result.ErrorCode = "403001"
			result.ErrorMessage = "Forbidden"
			return result
		}
	} else {
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

	// Filter field yang dilarang
	disallowedFields := map[string]bool{"email": true, "saltedpassword": true}
	filteredFields := map[string]any{}
	for k, v := range changeFields {
		if !disallowedFields[strings.ToLower(k)] {
			filteredFields[k] = v
		}
	}
	if len(filteredFields) == 0 {
		logger.Error(referenceId, "ERROR - Update_User_data - No allowed fields to update")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Validasi username
	if username, ok := filteredFields["username"].(string); ok && username != "" {
		var existing string
		err := conn.Get(&existing, `SELECT username FROM sysuser.user WHERE username = $1 AND id != $2`, username, userId)
		if err == nil && existing != "" {
			logger.Error(referenceId, "ERROR - Update_User_data - Username exists")
			result.ErrorCode = "409001"
			result.ErrorMessage = "Forbidden"
			return result
		}
	}

	// Validasi role (jika ada)
	if newRole, ok := filteredFields["role"].(string); ok {
		if targetRole == "system_admin" && editorRole != "system_master" {
			logger.Error(referenceId, "ERROR - Update_User_data - Cannot change admin")
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

	// Query UPDATE
	updateFields := []string{}
	updateValues := []any{}
	i := 1
	for key, val := range filteredFields {
		updateFields = append(updateFields, fmt.Sprintf("%s = $%d", key, i))
		updateValues = append(updateValues, val)
		i++
	}

	lastTimestamp := time.Now().Unix()
	updateFields = append(updateFields, fmt.Sprintf("last_timestamp = $%d", i))
	updateValues = append(updateValues, lastTimestamp)
	i++

	updateQuery := fmt.Sprintf("UPDATE sysuser.user SET %s WHERE id = $%d", strings.Join(updateFields, ", "), i)
	updateValues = append(updateValues, userId)

	_, err = conn.Exec(updateQuery, updateValues...)
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_User_data - Update failed:", err)
		result.ErrorCode = "500001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	logger.Info(referenceId, "INFO - Update_User_data - Update success")
	result.Payload["status"] = "success"
	return result
}
