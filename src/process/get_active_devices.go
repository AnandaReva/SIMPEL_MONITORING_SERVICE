package process

import (
	"fmt"
	"monitoring_service/logger"
	pubsub "monitoring_service/pubsub"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

func Get_Active_Devices(reference_id string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(reference_id, "INFO - Get_Active_Devices param: ", param)

	// Validasi parameter pagination
	pageSizeFloat, ok := param["page_size"].(float64)
	if !ok || pageSizeFloat <= 0 {
		logger.Error(reference_id, fmt.Sprintf("ERROR - Get_Active_Devices - Invalid page_size: %v", param["page_size"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid page size"
		return result
	}

	pageNumberFloat, ok := param["page_number"].(float64)
	if !ok || pageNumberFloat < 0 {
		logger.Error(reference_id, fmt.Sprintf("ERROR - Get_Active_Devices - Invalid page_number: %v", param["page_number"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid page number"
		return result
	}

	// Convert ke int64 setelah validasi berhasil
	pageSize := int64(pageSizeFloat)
	pageNumber := int64(pageNumberFloat)

	// Ambil WebSocketHub instance
	hub, err := pubsub.GetWebSocketHub(reference_id)
	if err != nil {
		logger.Error(reference_id, "ERROR - Failed to get WebSocketHub instance")
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Ambil total perangkat yang terhubung
	totalDevices := int64(len(hub.Devices))

	// Dapatkan daftar device yang aktif berdasarkan pagination
	activeDevices := hub.GetActiveDevices(reference_id, pageNumber, pageSize)

	// Format hasil ke dalam payload
	devicesData := []map[string]interface{}{}
	for _, device := range activeDevices {
		devicesData = append(devicesData, map[string]interface{}{
			"device_id":   device.DeviceID,
			"device_name": device.DeviceName,
		})
	}

	result.Payload["status"] = "success"
	result.Payload["devices"] = devicesData
	result.Payload["total_data"] = totalDevices
	logger.Info(reference_id, fmt.Sprintf("INFO - Found %d active devices", len(activeDevices)))

	return result
}
