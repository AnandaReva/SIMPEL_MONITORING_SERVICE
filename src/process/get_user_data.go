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

type UserData struct {
	UserId   int64  `db:"id" json:"user_id"`
	Username string `db:"username" json:"username"`
	FullName string `db:"full_name" json:"user_full_name"`
	Email    string `db:"email" json:"user_email"`
	Role     string `db:"role" json:"user_role"`
	UserSt   int    `db:"st" json:"user_st"`
	Data     string `db:"data" json:"user_data"`

}

func Get_User_Data(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {

	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Get_User_List param: ", param)

	UserIdParam, ok := param["user_id"].(float64)
	if !ok || UserIdParam <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_User_Data - Invalid UserIdParam: %v", param["UserIdParam"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	userIdParamInt := int64(UserIdParam)
	logger.Info(referenceId, fmt.Sprintf("INFO - Get_User_Data - userIdParamInt: %d", userIdParamInt))
	var userData UserData
	err := conn.Get(&userData, `SELECT id, username, full_name, email, role, st, data FROM sysuser.user WHERE id = $1`, userIdParamInt)
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_User_Data - Failed to get user data: %v", err))
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal server error"
		return result
	}
	if userData.UserId == 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_User_Data - User not found: %d", userIdParamInt))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request"
		return result
	}
	result.Payload["user_data"] = userData
	result.ErrorCode = "000000"
	result.ErrorMessage = "Success"
	logger.Info(referenceId, "INFO - Get_User_Data - Success")
	return result
	

}
