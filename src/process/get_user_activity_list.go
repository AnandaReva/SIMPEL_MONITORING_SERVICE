/*
simpel=> \d sysuser.user_activity

	                               Table "sysuser.user_activity"
	Column   |  Type  | Collation | Nullable |                      Default

-----------+--------+-----------+----------+---------------------------------------------------

	id        | bigint |           | not null | nextval('sysuser.user_activity_id_seq'::regclass)
	user_id   | bigint |           | not null |
	actor     | bigint |           |          |
	activity  | text   |           | not null |
	before    | jsonb  |           |          |
	after     | jsonb  |           |          |
	timestamp | bigint |           | not null | EXTRACT(epoch FROM now())::bigint

Indexes:

	"user_activity_pkey" PRIMARY KEY, btree (id)

Foreign-key constraints:

	    "fk_user_activity_actor" FOREIGN KEY (actor) REFERENCES sysuser."user"(id) ON DELETE SET NULL
	    "fk_user_activity_target" FOREIGN KEY (user_id) REFERENCES sysuser."user"(id) ON DELETE CASCADE


			exp req :

			{
					"user_id" : 1 ,
					"page_size" : 5,
					"page_number" : 1,
					"filter" : "update" // -> get where activity = "update"
					"order_by": "timestamp" // default
					"sort_type" "ASC" // defualt
			}

			exp res : {
					"user_activities" : [
						{
							"activity_id" : 1,
							"activity" : "",
							"actor" : {
								"actor_id" : 1,
								"actor_full_name" : ""
							},
							"timestamp" : epoch unix ,
							"before" ; {
							}, after : {
							}
						}
					],
					"total_data" : 47,
					"total_page" : 4,
					"status_success"

			}
*/
package process

import (
	"encoding/json"
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

type UserActivity struct {
	ActivityId int64              `db:"id" json:"activity_id"`
	ActorRaw   *json.RawMessage   `db:"actor" json:"-"`
	Actor      *UserActivityActor `json:"actor"` // final result
	Activity   string             `db:"activity" json:"activity_name"`
	Tstamp     int64              `db:"timestamp" json:"timestamp"`
	Before     string             `db:"before" json:"activity_before"`
	After      string             `db:"after" json:"activity_after"`
}

type UserActivityActor struct {
	ActorId       int64  `db:"actor_id" json:"actor_id"`
	ActorFullname string `db:"full_name" json:"actor_full_name"`
}

func Get_User_Activity_List(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Get_User_Activity_List param: ", param)

	// Validasi user_id
	targetUserId, ok := param["user_id"].(float64)
	if !ok || targetUserId <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_User_Activity_List - Invalid user_id: %v", param["user_id"]))
		result.ErrorCode = "400000"
		result.ErrorMessage = "Invalid request"
		return result
	}

	pageSize, ok := param["page_size"].(float64)
	if !ok || pageSize <= 0 {
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid page_size"
		return result
	}

	pageNumber, ok := param["page_number"].(float64)
	if !ok || pageNumber < 1 {
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid page_number"
		return result
	}

	offset := (int(pageNumber) - 1) * int(pageSize)

	// Base query
	baseQuery := `
	SELECT 
		ua.id,
		COALESCE(ua.activity, '') AS activity,
		ua.timestamp,
		COALESCE(ua.before::text, '{}'::text) AS before,
		COALESCE(ua.after::text, '{}'::text) AS after,
		CASE 
			WHEN ua.actor IS NOT NULL THEN json_build_object(
				'actor_id', u.id,
				'actor_full_name', COALESCE(u.full_name, '')
			)
			ELSE NULL
		END AS actor
	FROM sysuser.user_activity ua
	LEFT JOIN sysuser.user u ON ua.actor = u.id
	WHERE ua.user_id = $1
	`

	countQuery := `
	SELECT COUNT(*) 
	FROM sysuser.user_activity 
	WHERE user_id = $1
	`

	queryParams := []any{int64(targetUserId)}

	// Apply activity filter
	if filter, ok := param["filter"].(string); ok && filter != "" {
		if filter == "other" {
			baseQuery += ` AND ua.activity NOT IN ('update', 'connect', 'disconnect')`
			countQuery += ` AND activity NOT IN ('update', 'connect', 'disconnect')`
		} else {
			baseQuery += " AND ua.activity = $2"
			countQuery += " AND activity = $2"
			queryParams = append(queryParams, filter)
		}
	}

	// Get total data
	var totalData int
	if err := conn.Get(&totalData, countQuery, queryParams...); err != nil {
		logger.Error(referenceId, "ERROR - Count user_activity failed: ", err)
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Sorting
	orderBy := "timestamp"
	if val, ok := param["order_by"].(string); ok && val != "" {
		orderBy = val
	}

	sortType := "DESC"
	if val, ok := param["sort_type"].(string); ok && val != "" {
		sortType = val
	}

	// Final query with pagination
	finalQuery := fmt.Sprintf(`
		%s 
		ORDER BY ua.%s %s 
		LIMIT %d OFFSET %d
	`, baseQuery, orderBy, sortType, int(pageSize), offset)

	var rawActivities []UserActivity
	if err := conn.Select(&rawActivities, finalQuery, queryParams...); err != nil {
		logger.Error(referenceId, "ERROR - Select user_activity failed: ", err)
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Decode actor JSON
	activities := make([]UserActivity, 0, len(rawActivities))
	for _, raw := range rawActivities {
		var actor UserActivityActor
		if raw.ActorRaw != nil && string(*raw.ActorRaw) != "null" {
			if err := json.Unmarshal(*raw.ActorRaw, &actor); err != nil {
				logger.Error(referenceId, "ERROR - Decode actor JSON failed: ", err)
				result.ErrorCode = "500004"
				result.ErrorMessage = "Internal server error"
				return result
			}
			raw.Actor = &actor
		}
		activities = append(activities, raw)
	}

	totalPage := (totalData + int(pageSize) - 1) / int(pageSize)

	result.Payload["user_activities"] = activities
	result.Payload["total_data"] = totalData
	result.Payload["total_page"] = totalPage
	result.Payload["status"] = "success"

	logger.Info(referenceId, "INFO - Get_User_Activity_List success")
	return result
}
