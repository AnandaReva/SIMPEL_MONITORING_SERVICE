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

exp get Montoh data :  SELECT

	EXTRACT(MONTH FROM timestamp) AS month_number,
	TO_CHAR(timestamp, 'Month') AS month_name,
	COUNT(*) AS total_data,
	MIN(timestamp) AS first_record_timestamp,
	MAX(timestamp) AS last_record_timestamp

FROM device.data
WHERE EXTRACT(YEAR FROM timestamp) = 2025
GROUP BY month_number, month_name
ORDER BY month_number;
*/
package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

type MonthList struct {
	MonthNumber          int64   `db:"month_number" json:"month_number"`
	MonthName            string  `db:"month_name" json:"month_name"`
	FirstRecordTimestamp string  `db:"first_record_timestamp" json:"first_record_timestamp"`
	LastRecordTimestamp  string  `db:"last_record_timestamp" json:"last_record_timestamp"`
	EnegyConsumption     float64 `db:"max_energy" json:"energy_consumed_count"`
	TotalData            int64   `db:"total_data" json:"total_data"`
	TotalSize            float64 `db:"total_size" json:"total_size_bytes"`
}

func Get_Report_Month_List(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	deviceId, ok := param["device_id"].(float64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Month_List - Invalid device_id: %v", param["device_id"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	yearSelected, ok := param["year"].(float64)
	if !ok || yearSelected <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Month_List - Invalid year: %v", param["year"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Query utama
	query := `
		SELECT
			EXTRACT(MONTH FROM timestamp) AS month_number,
			TO_CHAR(timestamp, 'Month') AS month_name,

			TO_CHAR(MIN(timestamp), 'YYYY-MM-DD HH24:MI:SS') AS first_record_timestamp,
			TO_CHAR(MAX(timestamp), 'YYYY-MM-DD HH24:MI:SS') AS last_record_timestamp,
			MAX(energy) AS max_energy,
			SUM(pg_column_size(data.*))::float AS total_size,

			COUNT(*) AS total_data
		FROM device.data
		WHERE unit_id = (
			SELECT id FROM device.unit 
			WHERE id = $1  LIMIT 1
		)
		AND EXTRACT(YEAR FROM timestamp) = $2
		GROUP BY month_number, month_name
		ORDER BY month_number ASC
	`

	var monthList []MonthList
	err := conn.Select(&monthList, query, int64(deviceId), int(yearSelected))
	if err != nil {
		logger.Error(referenceId, "ERROR - Get_Report_Month_List - Query Failed: ", err.Error())
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	result.Payload["year"] = int(yearSelected)
	result.Payload["month_list"] = monthList

	return result
}
