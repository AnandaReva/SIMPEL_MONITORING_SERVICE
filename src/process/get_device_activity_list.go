/*
	\d device.device_activity ;
	                            Table "device.device_activity"
	 Column  |  Type  | Collation | Nullable |                   Default

----------+--------+-----------+----------+---------------------------------------------

	id       | bigint |           | not null | nextval('device.activity_id_seq'::regclass)
	unit_id  | bigint |           | not null |
	actor    | bigint |           |          |
	activity | text   |           | not null |
	tstamp   | bigint |           | not null | EXTRACT(epoch FROM now())::bigint
	before   | jsonb  |           |          |
	after    | jsonb  |           |          |

Indexes:

	"activity_pkey" PRIMARY KEY, btree (id)

Foreign-key constraints:

	    "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
	    "fk_user" FOREIGN KEY (actor) REFERENCES sysuser."user"(id) ON DELETE SET NULL

		exp req :

		{
				"device_id" : 1 ,
				"page_size" : 5,
				"page_number" : 1,
				"filter" : "update" // -> get where activity = "update"
				"order_by": "tstamp" // default
				"sort_type" "ASC" // defualt
		}

		exp res : {
				"device_activities" : [
					{
						"activity_id" : 1,
						"activity" : "",
						"actor" : {
							"actor_id" : 1,
							"actor_full_name" : ""
						},
						"tstamp" : epoch unix ,
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

type DeviceActivity struct {
	ActivityId int64           `db:"id" json:"activity_id"`
	ActorRaw   json.RawMessage `db:"actor" json:"-"` // untuk unmarshalling manual
	Actor      *ActivityActor  `json:"actor"`        // final result
	Activity   string          `db:"activity" json:"activity_name"`
	Tstamp     int64           `db:"tstamp" json:"tstamp"`
	Before     string          `db:"before" json:"activity_before"`
	After      string          `db:"after" json:"activity_after"`
}

type ActivityActor struct {
	ActorId       int64  `db:"actor_id" json:"actor_id"`
	ActorFullname string `db:"full_name" json:"actor_full_name"`
}

func Get_Device_Activity_List(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Get_Device_Activity_List param: ", param)

	// Validate device_id
	deviceId, ok := param["device_id"].(float64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Device_Activity_List - Invalid device_id: %v", param["device_id"]))
		result.ErrorCode = "400000"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Validasi parameter pagination
	pageSize, ok := param["page_size"].(float64)
	if !ok || pageSize <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Device_Activity_List - Invalid page_size: %v", param["page_size"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	pageNumber, ok := param["page_number"].(float64)
	if !ok || pageNumber < 1 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Device_Activity_List - Invalid page_number: %v", param["page_number"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request"
		return result
	}

	offset := (int(pageNumber) - 1) * int(pageSize)

	// Base query with JOIN to get actor information
	baseQuery := `
	SELECT 
		da.id,
		COALESCE(da.activity, '') AS activity,
		da.tstamp,
		COALESCE(da.before::text, '{}'::text) AS before,
		COALESCE(da.after::text, '{}'::text) AS after,
		CASE 
			WHEN da.actor IS NOT NULL THEN json_build_object(
				'actor_id', u.id,
				'actor_full_name', COALESCE(u.full_name, '')
			)
			ELSE NULL
		END AS actor
	FROM device.device_activity da
	LEFT JOIN sysuser.user u ON da.actor = u.id
	WHERE da.unit_id = $1
`

	countQuery := `
		SELECT COUNT(*) 
		FROM device.device_activity 
		WHERE unit_id = $1
	`

	queryParams := []interface{}{int64(deviceId)}

	logger.Debug(referenceId, "DEBUG - Get_Device_Activity_List - Query Parameters: ", queryParams)


	// Apply activity filter if provided
	filter, hasFilter := param["filter"].(string)
	if hasFilter && filter != "" {
		if filter == "other" {
			baseQuery += ` AND da.activity NOT IN ('update', 'connect', 'disconnect')`
			countQuery += ` AND activity NOT IN ('update', 'connect', 'disconnect')`
		} else {
			baseQuery += " AND da.activity = $2"
			countQuery += " AND activity = $2"
			queryParams = append(queryParams, filter)
		}
	}

	// Get total count
	var totalData int
	err := conn.Get(&totalData, countQuery, queryParams...)
	if err != nil {
		logger.Error(referenceId, "ERROR - Get_Device_Activity_List - Failed to get total data: ", err)
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Apply sorting
	orderBy, ok := param["order_by"].(string)
	if !ok || orderBy == "" {
		orderBy = "tstamp" // Default order
	}

	sortType, ok := param["sort_type"].(string)
	if !ok || sortType == "" {
		sortType = "DESC" // Default to DESC for chronological order
	}

	// Final query with pagination
	finalQuery := fmt.Sprintf(`
		%s 
		ORDER BY da.%s %s 
		LIMIT %d OFFSET %d
	`, baseQuery, orderBy, sortType, int(pageSize), offset)

	logger.Debug(referenceId, "DEBUG - Get_Device_Activity_List - Final Query: ", finalQuery)
	logger.Debug(referenceId, "DEBUG - Get_Device_Activity_List - Final Count Query: ", countQuery)

	var rawActivities []DeviceActivity
	err = conn.Select(&rawActivities, finalQuery, queryParams...)
	if err != nil {
		logger.Error(referenceId, "ERROR - Query Execution Failed: ", err)
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal server error"
		return result
	}

	//logger.Debug(referenceId, "DEBUG - Get_Device_Activity_List rawActivities result: " , rawActivities)

	// Decode JSON actor
	activities := make([]DeviceActivity, 0, len(rawActivities))
	for _, raw := range rawActivities {
		var actor ActivityActor
		if len(raw.ActorRaw) > 0 && string(raw.ActorRaw) != "null" {
			if err := json.Unmarshal(raw.ActorRaw, &actor); err != nil {
				logger.Error(referenceId, "ERROR - Failed to decode actor JSON: ", err)
				result.ErrorCode = "500004"
				result.ErrorMessage = "Internal server error"
				return result
			}
			raw.Actor = &actor
		}
		activities = append(activities, raw)
	}

	logger.Debug(referenceId, "DEBUG - Get_Device_Activity_List activities result: " , activities)


	logger.Debug(referenceId, "DEBUG - Get_Device_Activity_List - totalData: ", totalData)

	// Calculate total pages
	totalPage := (totalData + int(pageSize) - 1) / int(pageSize)

	result.Payload["device_activities"] = activities
	result.Payload["total_data"] = totalData
	result.Payload["total_page"] = totalPage
	result.Payload["status"] = "success"

	logger.Info(referenceId, "INFO - Get_Device_Activity_List completed successfully")
	return result
}
