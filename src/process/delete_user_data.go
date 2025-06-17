package process

import (

	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

/* simpel=> \d sysuser.user
                                           Table "sysuser.user"
     Column     |          Type          | Collation | Nullable |                 Default
----------------+------------------------+-----------+----------+------------------------------------------
 username       | character varying(225) |           | not null |
 full_name      | character varying(255) |           | not null |
 st             | integer                |           | not null |
 salt           | character varying(64)  |           | not null |
 saltedpassword | character varying(128) |           | not null |
 data           | jsonb                  |           | not null |
 id             | bigint                 |           | not null | nextval('sysuser.user_id_seq'::regclass)
 role           | character varying(128) |           | not null |
 email          | character varying(255) |           | not null |
Indexes:
    "user_pkey" PRIMARY KEY, btree (id)
    "unique_email" UNIQUE CONSTRAINT, btree (email)
    "user_unique_name" UNIQUE CONSTRAINT, btree (username)
Referenced by:
    TABLE "device.device_activity" CONSTRAINT "fk_user" FOREIGN KEY (actor) REFERENCES sysuser."user"(id) ON DELETE SET NULL
    TABLE "sysuser.token" CONSTRAINT "fk_user_id" FOREIGN KEY (user_id) REFERENCES sysuser."user"(id) ON DELETE CASCADE

*/

func Delete_User_Data(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Delete_User_Data param: ", param)

	UserIdParam, ok := param["user_id"].(float64)
	if !ok || UserIdParam <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Delete_User_Data - Invalid user_id: %v", param["user_id"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	userIdParamInt := int64(UserIdParam)
	logger.Info(referenceId, fmt.Sprintf("INFO - Delete_User_Data - userIdParamInt: %d", userIdParamInt))

	// Ambil role target terlebih dahulu
	var targetRole string
	err := conn.Get(&targetRole, `SELECT role FROM sysuser.user WHERE id = $1`, userIdParamInt)
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Delete_User_Data - User not found: %v", err))
		result.ErrorCode = "400004"
		result.ErrorMessage = "User not found"
		return result
	}

	// Cek hak akses berdasarkan hierarki
	if !canEditUser(role, targetRole) {
		logger.Error(referenceId, "ERROR - Delete_User_Data - Forbidden by role hierarchy")
		result.ErrorCode = "403001"
		result.ErrorMessage = "Forbidden"
		return result
	}

	// Eksekusi query DELETE
	queryToDeleteUser := `DELETE FROM sysuser.user WHERE id = $1`
	_, err = conn.Exec(queryToDeleteUser, userIdParamInt)
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Delete_User_Data - Failed to delete user: %v", err))
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	result.Payload["status"] = "success"
	logger.Info(referenceId, "INFO - Delete_User_Data - Success")
	return result
}
