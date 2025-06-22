/*     Column    |            Type             | Collation | Nullable |                 Default
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
    "idx_data_unit_id_timestamp" btree (unit_id, "timestamp" DESC)
    "idx_data_unit_year" btree (unit_id, EXTRACT(year FROM "timestamp"))
Foreign-key constraints:
    "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
Triggers:
    ts_insert_blocker BEFORE INSERT ON device.data FOR EACH ROW EXECUTE FUNCTION _timescaledb_functions.insert_blocker()
Number of child tables: 54 (Use \d+ to list them.)

simpel=>




*/

package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"
	"strings"

	"github.com/jmoiron/sqlx"
)

type AllDevicesAvailabelMonths struct {
	MonthNumber int     `db:"month" json:"month_number"`
	Energy      float64 `db:"energy" json:"total_energy"`
}

func Get_Report_All_Devices_Available_Months_By_Year(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	// Validasi parameter: tahun
	yearSelected, ok := param["year"].(float64)
	if !ok || yearSelected <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Invalid year: %v", param["year"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request: year"
		return result
	}

	// Ambil parameter sorting
	orderBy, _ := param["order_by"].(string)
	sortType, _ := param["sort_type"].(string)

	orderBy = strings.ToLower(orderBy)
	if orderBy != "energy" && orderBy != "month" {
		orderBy = "month"
	}

	sortType = strings.ToLower(sortType)
	if sortType != "asc" && sortType != "desc" {
		sortType = "desc"
	}

	// Query untuk seluruh perangkat
	query := fmt.Sprintf(`
	SELECT 
	EXTRACT(MONTH FROM day)::int AS month,
	SUM(daily_energy) AS energy
FROM (
	SELECT
		unit_id,
		DATE_TRUNC('day', timestamp) AS day,
		MAX(energy) - MIN(energy) AS daily_energy
	FROM device.data
	WHERE EXTRACT(YEAR FROM timestamp)::int = $1
	GROUP BY unit_id, day
) d
GROUP BY month
ORDER BY %s %s;

	`, orderBy, sortType)

	var monthList []AllDevicesAvailabelMonths
	err := conn.Select(&monthList, query, int(yearSelected))
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Query failed: %v", err))
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	result.Payload = map[string]any{
		"year":       int(yearSelected),
		"month_list": monthList,
		"status":     "success",
	}

	return result
}
