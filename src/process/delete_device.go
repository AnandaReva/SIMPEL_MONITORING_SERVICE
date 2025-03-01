package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

func Delete_Device(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {

	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Delete_Device param: ", param)

	// Validasi parameter pagination
	deviceId, ok := param["device_id"].(float64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Delete_Device - Invalid device_id: %v", param["device_id"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	deviceIdInt := int64(deviceId)

	// Query untuk menghapus perangkat
	queryToDeleteDevice := `DELETE FROM device.unit WHERE id = $1`
	_, err := conn.Exec(queryToDeleteDevice, deviceIdInt)
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Delete_Device - Failed to delete device: %v", err))
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error"
		return result
	}

	logger.Info(referenceId, fmt.Sprintf("INFO - Delete_Device - Successfully deleted device with ID: %d", deviceIdInt))

	result.Payload["status"] = "success"
	return result

}
