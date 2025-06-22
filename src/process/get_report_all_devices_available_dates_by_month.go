package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

type AllDeviceDayAvailable struct {
	DayDateNum int     `db:"day_date_num" json:"day_date_number"`
	DayNum     int     `db:"day_num" json:"day_number"` // 1-7, 1=Monday, 7=Sunday
	Energy     float64 `db:"energy" json:"total_energy"`
}

func Get_Report_All_Devices_Available_DayDates_By_Month(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	yearSelected, ok := param["year"].(float64)
	if !ok || yearSelected <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR -Get_Report_All_Devices_Available_DayDates_By_Month-   Invalid year: %v", param["year"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request"
		return result
	}

	monthSelected, ok := param["month"].(float64)
	if !ok || monthSelected <= 0 || monthSelected > 12 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_All_Devices_Available_DayDates_By_Month - Invalid month: %v", param["month"]))
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request"
		return result
	}

	query := `
SELECT
	EXTRACT(DAY FROM day_ts)::int AS day_date_num,
	CASE 
		WHEN EXTRACT(DOW FROM day_ts) = 0 THEN 7
		ELSE EXTRACT(DOW FROM day_ts)::int
	END AS day_num,
	MAX(energy) - MIN(energy) AS energy
FROM (
	SELECT
		DATE_TRUNC('day', d.timestamp) AS day_ts,
		d.energy
	FROM device.data d
	WHERE EXTRACT(YEAR FROM d.timestamp) = $1
	  AND EXTRACT(MONTH FROM d.timestamp) = $2
) daily
GROUP BY day_ts
ORDER BY day_ts;
`

	var dayList []AllDeviceDayAvailable
	err := conn.Select(&dayList, query, int(yearSelected), int(monthSelected))
	if err != nil {
		logger.Error(referenceId, "ERROR - Get_Report_All_Devices_Available_DayDates_By_Month - Query failed:", err.Error())
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	result.Payload = map[string]any{
		"year":     int(yearSelected),
		"month":    int(monthSelected),
		"day_list": dayList,
		"status":   "success",
	}

	return result
}
