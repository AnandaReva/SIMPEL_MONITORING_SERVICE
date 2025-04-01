package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

/*
{
	"change_fields" : {
		"username" : "new username",
		"full_name" : " new full name",
		etc
	},
}
*/

func Update_User_data(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {

	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		logger.Debug(referenceId, "DEBUG - Update_User_data - Execution completed in ", duration)
	}()

	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Update_User_data - params: ", param)

	// check if change_fields is present and object
	changeFields, ok := param["change_fields"].(map[string]any)
	if !ok || len(changeFields) == 0 {
		logger.Error(referenceId, "ERROR - Update_User_data - Missing or Invalid change_fields")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}
	// check if username is present in change_fields
	if username, ok := changeFields["username"].(string); ok && username != "" {

		// check if username already exists
		var existingField string
		queryCheck := `SELECT username FROM sysuser.user WHERE username = $1;`
		errCheck := conn.Get(&existingField, queryCheck, username)
		if errCheck == nil && existingField != "" {
			logger.Warning(referenceId, "Warning - Update_User_data - Username already exists: ", existingField)
			result.ErrorCode = "409001"
			result.ErrorMessage = "Conflict"
			return result
		}
	}

	// Update user data dynamically
	updateFields := []string{}
	updateValues := []any{}
	for key, value := range changeFields {
		updateFields = append(updateFields, fmt.Sprintf("%s = $%d", key, len(updateValues)+1))
		updateValues = append(updateValues, value)
	}

	logger.Debug(referenceId, "DEBUG - Update_User_data - updateFields:", updateFields)
	logger.Debug(referenceId, "DEBUG - Update_User_data - updateValues:", updateValues)

	// Append timestamp field
	currentTimestamp := time.Now()
	updateFields = append(updateFields, fmt.Sprintf("last_tstamp = $%d", len(updateValues)+1))
	updateValues = append(updateValues, currentTimestamp)

	logger.Debug(referenceId, "DEBUG - UpdateUserData - userID:", userID)

	updateQuery := fmt.Sprintf("UPDATE sysuser.user SET %s WHERE id = $%d", strings.Join(updateFields, ", "), len(updateValues)+1)
	updateValues = append(updateValues, userID)

	logger.Info(referenceId, "INFO - UpdateUserData - updateQuery:", updateQuery)
	_, err := conn.Exec(updateQuery, updateValues...)
	if err != nil {
		logger.Error(referenceId, "ERROR - UpdateUserData - Failed to execute update query:", err)
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal server error"
		return result
	}

	logger.Info(referenceId, "INFO - UpdateUserData - User data updated successfully")
	result.Payload["status"] = "success"
	return result
}
