package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

func Delete_Device_Data(referenceId string, conn *sqlx.DB, userId int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Delete_Device_Data param: ", param)

	// Validasi device_id
	deviceIdFloat, ok := param["device_id"].(float64)
	if !ok || deviceIdFloat <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Delete_Device_Data - Invalid device_id: %v", param["device_id"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}
	deviceId := int64(deviceIdFloat)

	// Mulai transaction
	tx, err := conn.Beginx()
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Delete_Device_Data - Cannot begin tx: %v", err))
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}
	defer func() {
		// Jika panic/return sebelum commit, rollback
		if err := recover(); err != nil {
			tx.Rollback()
			panic(err)
		}
	}()

	// 1) Hapus dulu semua detail/data turunan (misal di tabel device.data)
	deleteDataQuery := `DELETE FROM device.data WHERE unit_id = $1`
	if _, err := tx.Exec(deleteDataQuery, deviceId); err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Delete_Device_Data - Failed to delete data: %v", err))
		tx.Rollback()
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error"
		return result
	}
	logger.Info(referenceId, fmt.Sprintf("INFO - Delete_Device_Data - Deleted device data for device ID: %d", deviceId))

	// 1) Hapus dulu semua detail/activity turunan (misal di tabel device.data)
	deleteActivityQuery := `DELETE FROM device.device_activity WHERE unit_id = $1`
	if _, err := tx.Exec(deleteActivityQuery, deviceId); err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Delete_Device_Data - Failed to delete activity data: %v", err))
		tx.Rollback()
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error"
		return result
	}
	logger.Info(referenceId, fmt.Sprintf("INFO - Delete_Device_Data - Deleted devvice activity for device ID: %d", deviceId))

	// 2) Hapus baris utama di device.unit
	deleteParentQuery := `DELETE FROM device.unit WHERE id = $1`
	if _, err := tx.Exec(deleteParentQuery, deviceId); err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Delete_Device_Data - Failed to delete device: %v", err))
		tx.Rollback()
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal server error"
		return result
	}
	logger.Info(referenceId, fmt.Sprintf("INFO - Delete_Device_Data - Successfully deleted device with ID: %d", deviceId))

	// Commit
	if err := tx.Commit(); err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Delete_Device_Data - Commit failed: %v", err))
		result.ErrorCode = "500004"
		result.ErrorMessage = "Internal server error"
		return result
	}

	result.Payload["status"] = "success"
	return result
}
