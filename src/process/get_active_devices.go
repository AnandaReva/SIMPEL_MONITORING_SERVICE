package process

import (
	"fmt"
	"monitoring_service/logger"
	pubsub "monitoring_service/pubsub"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

func Get_Active_Devices(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Get_Active_Devices param: ", param)

	// Validasi parameter pagination
	pageSizeFloat, ok := param["page_size"].(float64)
	if !ok || pageSizeFloat <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Active_Devices - Invalid page_size: %v", param["page_size"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid page size"
		return result
	}

	pageNumberFloat, ok := param["page_number"].(float64)
	if !ok || pageNumberFloat < 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Active_Devices - Invalid page_number: %v", param["page_number"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid page number"
		return result
	}

	// Convert ke int64 setelah validasi berhasil
	pageSize := int64(pageSizeFloat)
	pageNumber := int64(pageNumberFloat)

	// Ambil WebSocketHub instance
	hub, err := pubsub.GetWebSocketHub(referenceId)
	if err != nil {
		logger.Error(referenceId, "ERROR - Failed to get WebSocketHub instance: ", err)
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Ambil total perangkat yang terhubung
	totalDevices := int64(len(hub.Devices))

	// Dapatkan daftar device yang aktif berdasarkan pagination
	activeDevices := hub.GetActiveDevices(referenceId, pageNumber, pageSize)

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
	logger.Info(referenceId, fmt.Sprintf("INFO - Found %d active devices", len(activeDevices)))

	return result
}

func Get_Dummy_Active_Devices(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Get_Dummy_Active_Devices param: ", param)

	// Validasi parameter pagination
	pageSizeFloat, ok := param["page_size"].(float64)
	if !ok || pageSizeFloat <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Dummy_Active_Devices - Invalid page_size: %v", param["page_size"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid page size"
		return result
	}

	pageNumberFloat, ok := param["page_number"].(float64)
	if !ok || pageNumberFloat <= 0 { // Harus >= 1
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Dummy_Active_Devices - Invalid page_number: %v", param["page_number"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid page number"
		return result
	}

	// Jumlah total dummy devices (default 100 jika tidak ada input `amount`)
	dummyDataAmount, ok := param["amount"].(float64)
	if !ok || dummyDataAmount <= 0 {
		dummyDataAmount = 40
	}
	totalData := int64(dummyDataAmount) // Total perangkat dummy yang tersedia

	// Convert ke int64 setelah validasi berhasil
	pageSize := int64(pageSizeFloat)
	pageNumber := int64(pageNumberFloat)

	// Menghitung offset berdasarkan page_number seperti SQL
	startIndex := (pageNumber - 1) * pageSize
	endIndex := startIndex + pageSize

	// Jika endIndex melebihi jumlah total data, batasi ke total data
	if endIndex > totalData {
		endIndex = totalData
	}

	devicesData := []map[string]interface{}{}

	// Generate dummy data sesuai halaman yang diminta
	for i := startIndex; i < endIndex; i++ {
		deviceID := i + 1 // Memastikan device_id selalu dimulai dari 1
		deviceName := fmt.Sprintf("device_%d", deviceID)

		devicesData = append(devicesData, map[string]interface{}{
			"device_id":   deviceID,
			"device_name": deviceName,
		})
	}

	// Isi response dengan pagination yang benar
	result.Payload["status"] = "success"
	result.Payload["devices"] = devicesData
	result.Payload["total_data"] = totalData
	result.Payload["page_number"] = pageNumber
	result.Payload["page_size"] = pageSize
	result.Payload["total_pages"] = (totalData + pageSize - 1) / pageSize // Pembulatan ke atas

	logger.Info(referenceId, fmt.Sprintf("INFO - Page %d, Showing %d of %d devices", pageNumber, len(devicesData), totalData))

	return result
}
