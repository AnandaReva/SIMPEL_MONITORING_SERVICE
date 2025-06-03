package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

func Get_Report_Available_Months_By_Year(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	// Validate inputs
	deviceId, ok := param["device_id"].(float64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Available_Months_By_Year - Invalid device_id: %v", param["device_id"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	yearSelected, ok := param["year"].(float64)
	if !ok || yearSelected <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Available_Months_By_Year - Invalid year: %v", param["year"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request"
		return result
	}

	query := `
		SELECT DISTINCT EXTRACT(MONTH FROM d.timestamp)::int AS month_number
		FROM device.data d
		WHERE d.unit_id = $1 AND EXTRACT(YEAR FROM d.timestamp) = $2
		ORDER BY month_number;
	`

	var monthNumbers []int
	err := conn.Select(&monthNumbers, query, int64(deviceId), int(yearSelected))
	if err != nil {
		logger.Error(referenceId, "ERROR - Get_Report_Available_Months_By_Year - Get_Month_Numbers query failed:", err.Error())
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	result.Payload = map[string]any{
		"year":          int(yearSelected),
		"device_id":     int64(deviceId),
		"month_numbers": monthNumbers,
	}

	return result
}
