/*
simpel=> \d device.data;

											 Table "device.data"
	Column    |            Type             | Collation | Nullable |                 Default

--------------+-----------------------------+-----------+----------+------------------------------------------

	id           | bigint                      |           | not null | nextval('device.data2_id_seq'::regclass)
	unit_id      | bigint                      |           | not null |
	timestamp    | timestamp without time zone |           | not null | now()
	voltage      | double precision            |           | not null |
	current      | double precision            |           | not null |
	power        | double precision            |           | not null |
	energy       | double precision            |           | not null |
	frequency    | double precision            |           | not null |
	power_factor | double precision            |           | not null |

Indexes:

	"data_tstamp_idx" btree ("timestamp" DESC)
	"data_unique_idx" UNIQUE, btree (id, "timestamp")

Foreign-key constraints:

	"fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE

Triggers:

		ts_insert_blocker BEFORE INSERT ON device.data FOR EACH ROW EXECUTE FUNCTION _timescaledb_functions.insert_blocker()


	  exp get years with pagination

SELECT

	EXTRACT(YEAR FROM timestamp) AS year,
	MIN(timestamp) AS first_timestamp,
	MAX(timestamp) AS last_timestamp,
	COUNT(*) AS record_count

FROM device.data
GROUP BY year
ORDER BY year DESC
LIMIT 5 OFFSET 0;

// total years
SELECT COUNT(DISTINCT EXTRACT(YEAR FROM timestamp)) AS total_years
FROM device.data;


*/
package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"
	"strings"

	"github.com/jmoiron/sqlx"
)

type YearList struct {
	Year                 int64  `db:"year" json:"year"`
	FirstRecordTimestamp string `db:"first_record_timestamp" json:"first_record_timestamp"`
	LastRecordTimestamp  string `db:"last_record_timestamp" json:"last_record_timestamp"`
	RecordCount          int64  `db:"record_count" json:"record_count"`
}

func Get_Report_Year_List(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	deviceId, ok := param["device_id"].(float64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Year_List - Invalid device_id: %v", param["device_id"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	pageSize, ok := param["page_size"].(float64)
	if !ok || pageSize <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Year_List - Invalid page_size: %v", param["page_size"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request"
		return result
	}

	pageNumber, ok := param["page_number"].(float64)
	if !ok || pageNumber < 1 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Year_List - Invalid page_number: %v", param["page_number"]))
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// default: ORDER BY year DESC
	sortType := "DESC"
	if val, ok := param["sort_type"].(string); ok {
		lower := strings.ToLower(val)
		if lower == "asc" || lower == "desc" {
			sortType = strings.ToUpper(lower)
		}
	}

	orderBy := "year"
	if val, ok := param["order_by"].(string); ok {
		lower := strings.ToLower(val)
		if lower == "year" || lower == "record_count" {
			orderBy = lower
		}
	}

	offset := (int(pageNumber) - 1) * int(pageSize)
	var totalData int

	baseQuery := `
	FROM device.data 
	WHERE unit_id = (
		SELECT id FROM device.unit 
		WHERE device_id = $1 AND deleted_at IS NULL LIMIT 1
	)
	`

	// Query total data
	countQuery := `
	SELECT COUNT(DISTINCT EXTRACT(YEAR FROM timestamp)) AS total
	` + baseQuery

	err := conn.Get(&totalData, countQuery, int64(deviceId))
	if err != nil {
		logger.Error(referenceId, "ERROR - Get_Report_Year_List - Count Query Failed: ", err.Error())
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Mapping nama kolom agar aman dari SQL Injection
	orderByMap := map[string]string{
		"year":         "year",
		"record_count": "record_count",
	}

	orderColumn, ok := orderByMap[orderBy]
	if !ok {
		orderColumn = "year"
	}

	dataQuery := fmt.Sprintf(`
		SELECT
			EXTRACT(YEAR FROM timestamp) AS year,
			MIN(timestamp) AS first_record_timestamp,
			MAX(timestamp) AS last_record_timestamp,
			COUNT(*) AS record_count
		%s
		GROUP BY year
		ORDER BY %s %s
		LIMIT $2 OFFSET $3
	`, baseQuery, orderColumn, sortType)

	var yearList []YearList
	err = conn.Select(&yearList, dataQuery, int64(deviceId), int(pageSize), offset)
	if err != nil {
		logger.Error(referenceId, "ERROR - Get_Report_Year_List - Data Query Failed: ", err.Error())
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error"
		return result
	}

	result.Payload["total_data"] = totalData
	result.Payload["page_number"] = int(pageNumber)
	result.Payload["page_size"] = int(pageSize)
	result.Payload["year_list"] = yearList

	return result
}
