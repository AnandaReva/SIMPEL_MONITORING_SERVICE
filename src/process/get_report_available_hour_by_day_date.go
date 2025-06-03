package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

type TimeAvailable struct {
	Hour int `db:"hour" json:"hour"` // misal: 8, 12, 18
}

func Get_Report_Available_Hours_By_Day_Date(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	deviceId, ok := param["device_id"].(float64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERRO - Get_Report_Available_Hours_By_Day_Date - Invalid device_id: %v", param["device_id"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	yearSelected, ok := param["year"].(float64)
	if !ok || yearSelected <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Available_Hours_By_Day_Date - Invalid year: %v", param["year"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request"
		return result
	}

	monthSelected, ok := param["month"].(float64)
	if !ok || monthSelected <= 0 || monthSelected > 12 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Available_Hours_By_Day_Date - Invalid month: %v", param["month"]))
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request"
		return result
	}

	daySelected, ok := param["day_date"].(float64)
	if !ok || daySelected <= 0 || daySelected > 31 {
		logger.Error(referenceId, fmt.Sprintf("ERROR = Get_Report_Available_Hours_By_Day_Date - Invalid day_date: %v", param["day_date"]))
		result.ErrorCode = "400004"
		result.ErrorMessage = "Invalid request"
		return result
	}

	query := `
		SELECT DISTINCT
			EXTRACT(HOUR FROM d.timestamp)::int AS hour
		FROM device.data d
		WHERE d.unit_id = $1
		  AND EXTRACT(YEAR FROM d.timestamp) = $2
		  AND EXTRACT(MONTH FROM d.timestamp) = $3
		  AND EXTRACT(DAY FROM d.timestamp) = $4
		ORDER BY hour;
	`

	var hourList []TimeAvailable
	err := conn.Select(&hourList, query, int64(deviceId), int(yearSelected), int(monthSelected), int(daySelected))
	if err != nil {
		logger.Error(referenceId, "ERROR - Get_Report_Available_Hours_By_Date query failed:", err.Error())
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	result.Payload = map[string]any{
		"year":      int(yearSelected),
		"month":     int(monthSelected),
		"day":       int(daySelected),
		"device_id": int64(deviceId),
		"hour_list": hourList,
	}

	return result
}
