package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

type DayAvailable struct {
	MonthNumber int `db:"month_number" json:"month_number"`
	DayDateNum  int `db:"day_date_num" json:"day_date_num"` // tanggal (1–31)
	DayNum      int `db:"day_num" json:"day_num"`           // hari dalam minggu (1=Senin ... 7=Minggu)
}

func Get_Available_Dates_By_Month(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {

	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	deviceId, ok := param["device_id"].(float64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Available_Dates_By_Month - Invalid device_id: %v", param["device_id"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	yearSelected, ok := param["year"].(float64)
	if !ok || yearSelected <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Available_Dates_By_Month - Invalid year: %v", param["year"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request"
		return result
	}

	monthSelected, ok := param["month"].(float64)
	if !ok || monthSelected <= 0 || monthSelected > 12 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Available_Dates_By_Month - Invalid month: %v", param["month"]))
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request"
		return result
	}

	query := `
		SELECT DISTINCT
			EXTRACT(MONTH FROM d.timestamp)::int AS month_number,
			EXTRACT(DAY FROM d.timestamp)::int AS day_date_num,
			CASE 
				WHEN EXTRACT(DOW FROM d.timestamp) = 0 THEN 7
				ELSE EXTRACT(DOW FROM d.timestamp)::int
			END AS day_num
		FROM device.data d
		WHERE d.unit_id = $1
		  AND EXTRACT(YEAR FROM d.timestamp) = $2
		  AND EXTRACT(MONTH FROM d.timestamp) = $3
		ORDER BY day_date_num;
	`

	var dayList []DayAvailable
	err := conn.Select(&dayList, query, int64(deviceId), int(yearSelected), int(monthSelected))
	if err != nil {
		logger.Error(referenceId, "ERROR - Get_Available_Dates_By_Month - query failed:", err.Error())
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	result.Payload = map[string]any{
		"year":               int(yearSelected),
		"month":              int(monthSelected),
		"device_id":          int64(deviceId),
		"available_day_list": dayList,
	}

	return result
}
