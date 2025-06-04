package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"
	"strings"

	"github.com/jmoiron/sqlx"
)

type AvailableMonths struct {
	MonthNumber int     `db:"month" json:"month_number"`
	Energy      float64 `db:"energy" json:"total_energy"`
}

func Get_Report_Available_Months_By_Year(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	// Validasi parameter
	deviceId, ok := param["device_id"].(float64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Invalid device_id: %v", param["device_id"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request: device_id"
		return result
	}

	yearSelected, ok := param["year"].(float64)
	if !ok || yearSelected <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Invalid year: %v", param["year"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request: year"
		return result
	}

	// Default order_by and sort_type
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

	// Susun query dengan ORDER BY dinamis (whitelisted)
	query := fmt.Sprintf(`
		SELECT 
			EXTRACT(MONTH FROM d.timestamp)::int AS month,
			MAX(d.energy)::numeric AS energy
		FROM device.data d
		WHERE d.unit_id = $1
		  AND EXTRACT(YEAR FROM d.timestamp)::int = $2
		GROUP BY month
		ORDER BY %s %s;
	`, orderBy, sortType)

	var monthList []AvailableMonths
	err := conn.Select(&monthList, query, int64(deviceId), int(yearSelected))
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Query failed: %v", err))
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	result.Payload = map[string]any{
		"year":       int(yearSelected),
		"device_id":  int64(deviceId),
		"month_list": monthList,
	}

	return result
}
