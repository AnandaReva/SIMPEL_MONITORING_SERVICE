package process

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"
	"strings"
	"time"

	"maps"

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

	// Validate user_id parameter
	userId, ok := param["user_id"].(float64)
	if !ok || userId <= 0 {
		logger.Error(referenceId, "ERROR - Update_User_data - Invalid user_id")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	userIdInt := int64(userId)

	// Validate change_fields parameter
	changeFields, ok := param["change_fields"].(map[string]any)
	if !ok || len(changeFields) == 0 {
		logger.Error(referenceId, "ERROR - Update_User_data - Invalid change_fields")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Begin transaction
	tx, err := conn.Beginx()
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_User_data - Failed to start transaction:", err)
		result.ErrorCode = "500000"
		result.ErrorMessage = "Internal server error"
		return result
	}
	defer tx.Rollback()

	// Initialize before/after data for activity log
	beforeData := make(map[string]any)
	afterData := make(map[string]any)

	// Get current user role for permission checks
	var currentRole string
	err = tx.Get(&currentRole, `SELECT role FROM sysuser.user WHERE id = $1`, userIdInt)
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_User_data - Target user not found:", err)
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Permission check
	if editorUserId != userIdInt {
		if !canEditUser(editorRole, currentRole) {
			logger.Error(referenceId, "ERROR - Update_User_data - Forbidden by role hierarchy")
			result.ErrorCode = "403001"
			result.ErrorMessage = "Forbidden"
			return result
		}
	} else {
		// Prevent self-role change
		if _, ok := changeFields["role"]; ok && editorRole == "system_master" {
			logger.Error(referenceId, "ERROR - Update_User_data - Master cannot change own role")
			result.ErrorCode = "403001"
			result.ErrorMessage = "Forbidden"
			return result
		}
	}

	// Handle status field conversion
	if val, ok := changeFields["status"]; ok {
		statusFloat, ok := val.(float64)
		if !ok || (statusFloat != 0 && statusFloat != 1) {
			logger.Error(referenceId, "ERROR - Update_User_data - Invalid status value")
			result.ErrorCode = "400002"
			result.ErrorMessage = "Invalid status value"
			return result
		}
		changeFields["st"] = int(statusFloat)
		delete(changeFields, "status")
	}

	// Validate role changes
	if newRole, ok := changeFields["role"].(string); ok {
		if currentRole == "system_admin" && editorRole != "system_master" {
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

	// Get list of fields that will be updated (excluding data and status)
	updateFieldNames := make([]string, 0)
	for key := range changeFields {
		if key != "data" && key != "status" {
			updateFieldNames = append(updateFieldNames, key)
		}
	}

	// Get beforeData only for fields that will be changed
	if len(updateFieldNames) > 0 {
		fieldList := strings.Join(updateFieldNames, ", ")
		queryToGetBeforeData := fmt.Sprintf(`SELECT %s FROM sysuser.user WHERE id = $1`, fieldList)

		row := make(map[string]any)
		err := tx.QueryRowx(queryToGetBeforeData, userIdInt).MapScan(row)
		if err != nil {
			logger.Error(referenceId, "ERROR - Failed to fetch beforeData for updated fields:", err)
			result.ErrorCode = "500004"
			result.ErrorMessage = "Internal server error"
			return result
		}

		// Save beforeData
		for _, key := range updateFieldNames {
			if val, exists := row[key]; exists {
				beforeData[key] = val
			}
		}
	}

	// Handle data field updates
	if dataField, ok := changeFields["data"].(map[string]any); ok && len(dataField) > 0 {
		success := updateUserDataField(referenceId, tx, userIdInt, dataField, beforeData, afterData)
		if !success {
			result.ErrorCode = "500002"
			result.ErrorMessage = "Failed to update user data field"
			return result
		}
	}

	// Build update query for basic fields
	updateFields := []string{}
	updateValues := []any{}
	i := 1

	for key, val := range changeFields {
		if key == "data" {
			continue // Already handled
		}

		// Handle JSON marshaling for complex types
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

	// Add timestamp update
	lastTimestamp := time.Now().Unix()
	updateFields = append(updateFields, fmt.Sprintf("last_timestamp = $%d", i))
	updateValues = append(updateValues, lastTimestamp)
	i++

	// Finalize query
	updateQuery := fmt.Sprintf("UPDATE sysuser.user SET %s WHERE id = $%d",
		strings.Join(updateFields, ", "), i)
	updateValues = append(updateValues, userIdInt)

	// Execute update
	_, err = tx.Exec(updateQuery, updateValues...)
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_User_data - Update failed:", err)
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Prepare afterData for changed fields
	for key, val := range changeFields {
		if key == "data" || key == "status" {
			continue
		}
		afterData[key] = val
	}
	if _, ok := changeFields["st"]; ok {
		afterData["status"] = changeFields["st"]
	}

	// Insert activity log
	queryToInsertActivity := `
	INSERT INTO sysuser.user_activity (
		user_id, actor, activity, before, after
	) VALUES ($1, $2, $3, $4, $5)`

	beforeJson, err := json.Marshal(beforeData)
	if err != nil {
		logger.Error(referenceId, "ERROR - Failed to marshal beforeData:", err)
		result.ErrorCode = "500004"
		result.ErrorMessage = "Internal server error"
		return result
	}

	afterJson, err := json.Marshal(afterData)
	if err != nil {
		logger.Error(referenceId, "ERROR - Failed to marshal afterData:", err)
		result.ErrorCode = "500004"
		result.ErrorMessage = "Internal server error"
		return result
	}

	_, err = tx.Exec(queryToInsertActivity,
		userIdInt,
		editorUserId,
		"update",
		string(beforeJson),
		string(afterJson),
	)
	if err != nil {
		logger.Error(referenceId, "ERROR - Failed to insert activity log:", err)
		result.ErrorCode = "500005"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Commit transaction
	err = tx.Commit()
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_User_data - Failed to commit transaction:", err)
		result.ErrorCode = "500006"
		result.ErrorMessage = "Internal server error"
		return result
	}

	logger.Info(referenceId, "INFO - Update_User_data - Update success")
	result.Payload["status"] = "success"
	return result
}

func updateUserDataField(referenceId string, tx *sqlx.Tx, userId int64, dataField map[string]any,
	beforeData, afterData map[string]any) bool {

	var err error

	// Get existing data
	var existingDataRaw sql.NullString
	querySelect := `SELECT data FROM sysuser.user WHERE id = $1;`
	err = tx.Get(&existingDataRaw, querySelect, userId)
	if err != nil {
		logger.Error(referenceId, "ERROR - updateUserDataField - Failed to fetch current data:", err)
		return false
	}

	currentData := make(map[string]any)
	if existingDataRaw.Valid {
		currentData, err = utils.JSONStringToMap(existingDataRaw.String)
		if err != nil {
			logger.Error(referenceId, "ERROR - updateUserDataField - Failed to parse JSON:", err)
			return false
		}
	}

	// Prepare before/after snapshot
	originalBefore := make(map[string]any)
	updatedAfter := maps.Clone(currentData)

	// Handle delete fields
	if deleteData, ok := dataField["delete"].([]any); ok && len(deleteData) > 0 {
		for _, key := range deleteData {
			if k, ok := key.(string); ok {
				if val, exists := currentData[k]; exists {
					originalBefore[k] = val
					delete(updatedAfter, k)
				}
			}
		}

		// Build delete expression
		expr := ""
		for _, key := range deleteData {
			if k, ok := key.(string); ok {
				expr += " - '" + k + "'"
			}
		}
		queryDelete := fmt.Sprintf(`UPDATE sysuser.user SET data = data%s WHERE id = $1;`, expr)
		_, err = tx.Exec(queryDelete, userId)
		if err != nil {
			logger.Error(referenceId, "ERROR - updateUserDataField - Failed to delete fields from JSON:", err)
			return false
		}
	}

	// Handle update fields
	if updateData, ok := dataField["update"].(map[string]any); ok {
		for key, newVal := range updateData {
			if oldVal, exists := currentData[key]; exists && oldVal != newVal {
				originalBefore[key] = oldVal
				updatedAfter[key] = newVal
			}
		}
	}

	// Handle insert fields
	if insertData, ok := dataField["insert"].(map[string]any); ok {
		for key, newVal := range insertData {
			if _, exists := currentData[key]; !exists {
				updatedAfter[key] = newVal
			}
		}
	}

	// Save final data
	updatedJSON, err := utils.MapToJSON(updatedAfter)
	if err != nil {
		logger.Error(referenceId, "ERROR - updateUserDataField - Failed to convert map to JSON:", err)
		return false
	}

	queryUpdate := `UPDATE sysuser.user SET data = $1 WHERE id = $2;`
	_, err = tx.Exec(queryUpdate, updatedJSON, userId)
	if err != nil {
		logger.Error(referenceId, "ERROR - updateUserDataField - Failed to update database:", err)
		return false
	}

	// Save to before/after data
	if len(originalBefore) > 0 {
		beforeData["data"] = originalBefore
	}
	if len(updatedAfter) > 0 {
		afterData["data"] = updatedAfter
	}

	logger.Info(referenceId, "INFO - updateUserDataField - Successfully updated user data field")
	return true
}
