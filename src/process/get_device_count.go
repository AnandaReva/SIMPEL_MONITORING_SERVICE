package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

func Get_Device_Count(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Get_Device_Count - Fetching total device count")

	var totalDevices int
	query := `SELECT COUNT(*) FROM device.unit`

	err := conn.Get(&totalDevices, query)
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Device_Count - Query failed: %v", err))
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	result.Payload["total_devices"] = totalDevices
	result.Payload["status"] = "success"
	return result
}
