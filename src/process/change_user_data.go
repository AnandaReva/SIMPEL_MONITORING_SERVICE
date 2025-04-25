package process

import (
	"fmt"
	"strings"

	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

var roleHierarchy = map[string][]string{
	"system master": {"system admin", "system user"},
	"system admin":  {"system user"},
}

// Hanya field-field ini yang boleh diubah
var editableFields = map[string]bool{
	"role":      true,
	"st":        true,
	"full_name": true,
	"email":     true,
}

func Change_User_Data(referenceId string, conn *sqlx.DB, actorID int64, actorRole string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Change_User_Data param: ", param)

	// Validasi user_id target
	targetUserIdFloat, ok := param["user_id"].(float64)
	if !ok || targetUserIdFloat <= 0 {
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid user_id"
		return result
	}
	targetUserId := int64(targetUserIdFloat)

	// Tidak boleh ubah diri sendiri
	if targetUserId == actorID {
		result.ErrorCode = "403003"
		result.ErrorMessage = "You can't modify yourself"
		return result
	}

	// Cek role user target
	var targetRole string
	err := conn.Get(&targetRole, `SELECT role FROM sysuser.user WHERE id = $1`, targetUserId)
	if err != nil {
		result.ErrorCode = "500001"
		result.ErrorMessage = "Failed to get target user role"
		logger.Error(referenceId, "ERROR - Get target role: ", err)
		return result
	}

	// Tidak boleh mengubah user system master
	if strings.ToLower(targetRole) == "system master" {
		result.ErrorCode = "403004"
		result.ErrorMessage = "Cannot modify system master"
		return result
	}

	// Filter dan bangun bagian SET dari query
	setClauses := []string{}
	values := []interface{}{}
	idx := 1

	for field, val := range param {
		if field == "user_id" {
			continue
		}
		if !editableFields[field] {
			continue
		}

		// Validasi tambahan jika field yang diubah adalah "role"
		if field == "role" {
			newRole, ok := val.(string)
			if !ok || newRole == "" || newRole == "system master" {
				result.ErrorCode = "400003"
				result.ErrorMessage = "Invalid role value"
				return result
			}

			// Validasi hak akses
			allowedRoles, exists := roleHierarchy[actorRole]
			if !exists {
				result.ErrorCode = "403001"
				result.ErrorMessage = "Forbidden"
				return result
			}
			isAllowed := false
			for _, r := range allowedRoles {
				if r == newRole {
					isAllowed = true
					break
				}
			}
			if !isAllowed {
				result.ErrorCode = "403002"
				result.ErrorMessage = "Forbidden to assign this role"
				return result
			}
		}

		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", field, idx))
		values = append(values, val)
		idx++
	}

	if len(setClauses) == 0 {
		result.ErrorCode = "400004"
		result.ErrorMessage = "No valid fields to update"
		return result
	}

	values = append(values, targetUserId)
	query := fmt.Sprintf(`UPDATE sysuser.user SET %s WHERE id = $%d AND role != 'system master'`,
		strings.Join(setClauses, ", "), idx)

	logger.Info(referenceId, "INFO - Change_User_Data - Final Query: ", query)

	_, err = conn.Exec(query, values...)
	if err != nil {
		result.ErrorCode = "500002"
		result.ErrorMessage = "Failed to update user"
		logger.Error(referenceId, "ERROR - Exec update: ", err)
		return result
	}

	result.Payload["status"] = "success"
	logger.Info(referenceId, fmt.Sprintf("INFO - Change_User_Data success update user id: %d", targetUserId))
	return result
}
