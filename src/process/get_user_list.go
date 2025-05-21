/* \d sysuser.user;
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
 create_timestamp  | bigint                 |           | not null | EXTRACT(epoch FROM now())::bigint
 last_timestamp    | bigint                 |           | not null | EXTRACT(epoch FROM now())::bigint
Indexes:
    "user_pkey" PRIMARY KEY, btree (id)
    "unique_email" UNIQUE CONSTRAINT, btree (email)
    "user_unique_name" UNIQUE CONSTRAINT, btree (username)
Referenced by:
    TABLE "device.device_activity" CONSTRAINT "fk_user" FOREIGN KEY (actor) REFERENCES sysuser."user"(id) ON DELETE SET NULL
    TABLE "sysuser.token" CONSTRAINT "fk_user_id" FOREIGN KEY (user_id) REFERENCES sysuser."user"(id) ON DELETE CASCADE

*/

package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"
	"strings"

	"github.com/jmoiron/sqlx"
)

type UserList struct {
	UserId              int64  `db:"id" json:"user_id"`
	UserFullName        string `db:"full_name" json:"user_full_name"`
	UserRole            string `db:"role" json:"user_role"`
	UserSt              int    `db:"st" json:"user_st"`
	UserCreateTimeStamp int64  `db:"create_timestamp" json:"user_create_timestamp"`
	UserLastTimeStamp   int64  `db:"last_timestamp" json:"user_last_timestamp"`
}

func Get_User_List(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Get_User_List - Params: ", param)

	pageSize, ok := param["page_size"].(float64)
	if !ok || pageSize <= 0 {
		logger.Warning(referenceId, "Get_User_List - Invalid page_size: ", pageSize)
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid page size"
		return result
	}

	pageNumber, ok := param["page_number"].(float64)
	if !ok || pageNumber < 1 {
		logger.Warning(referenceId, "Get_User_List - Invalid page_number: ", pageNumber)
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid page number"
		return result
	}

	offset := (int(pageNumber) - 1) * int(pageSize)
	var totalData int
	var users []UserList

	baseQuery := `
		SELECT DISTINCT id, full_name, role, st, create_timestamp, last_timestamp
		FROM sysuser."user"
		WHERE 1=1`
	countQuery := `SELECT COUNT(DISTINCT id) FROM sysuser."user" WHERE 1=1`

	var conditions []string
	var args []any
	argIndex := 1

	// Filter pencarian
	if filter, ok := param["filter"].(string); ok && filter != "" {
		conditions = append(conditions,
			fmt.Sprintf(`(full_name ILIKE '%%' || $%d || '%%' OR data::text ILIKE '%%' || $%d || '%%')`, argIndex, argIndex),
		)
		args = append(args, filter)
		argIndex++
	}

	// Filter status
	if st, ok := param["st"].(float64); ok && (st == 0 || st == 1) {
		conditions = append(conditions, fmt.Sprintf("st = $%d", argIndex))
		args = append(args, int(st))
		argIndex++
	}

	// Filter: user tidak melihat dirinya sendiri
	conditions = append(conditions, fmt.Sprintf("id != $%d", argIndex))
	args = append(args, userID)
	argIndex++

	// Filter: system_admin tidak bisa lihat system_master
	if role == "system_admin" {
		conditions = append(conditions, "role != 'system_master'")
	}

	if len(conditions) > 0 {
		conditionSQL := " AND " + strings.Join(conditions, " AND ")
		baseQuery += conditionSQL
		countQuery += conditionSQL
	}

	logger.Info(referenceId, "INFO - Get_User_List - Count Query: ", countQuery)

	err := conn.Get(&totalData, countQuery, args...)
	if err != nil {
		logger.Error(referenceId, "ERROR - Get_User_List - Failed to get total data: ", err)
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error"
		return result
	}

	orderBy := "id"
	if val, ok := param["order_by"].(string); ok {
		lower := strings.ToLower(val)
		if lower == "full_name" || lower == "role" || lower == "id" || lower == "create_timestamp" || lower == "last_timestamp" {
			orderBy = lower
		}
	}

	sortType := "ASC"
	if val, ok := param["sort_type"].(string); ok {
		lower := strings.ToLower(val)
		if lower == "asc" || lower == "desc" {
			sortType = strings.ToUpper(lower)
		}
	}

	finalQuery := fmt.Sprintf(`%s ORDER BY %s %s LIMIT %d OFFSET %d`, baseQuery, orderBy, sortType, int(pageSize), offset)
	logger.Info(referenceId, "INFO - Get_User_List - Final Query: ", finalQuery)

	err = conn.Select(&users, finalQuery, args...)
	if err != nil {
		logger.Error(referenceId, "ERROR - Get_User_List - Query execution failed: ", err)
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal server error"
		return result
	}

	totalPage := (totalData + int(pageSize) - 1) / int(pageSize)

	result.Payload["users"] = users
	result.Payload["total_data"] = totalData
	result.Payload["total_page"] = totalPage
	result.Payload["status"] = "success"

	logger.Info(referenceId, "INFO - Get_User_List - Success, total_data: ", totalData, ", total_page: ", totalPage)
	return result
}
