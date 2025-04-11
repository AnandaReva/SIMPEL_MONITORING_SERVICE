package process

import (
	"fmt"
	"monitoring_service/crypto"
	"monitoring_service/logger"
	"monitoring_service/utils"
	"os"
	"time"

	"github.com/jmoiron/sqlx"
)

// Add_Device_Data mendaftarkan perangkat baru ke dalam sistem secara dinamis
func Add_Device_Data(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Add_Device_Data - param: ", param)

	deviceName, ok := param["name"].(string)
	if !ok || deviceName == "" || len(deviceName) > 20 {
		logger.Error(referenceId, "ERROR - Add_Device_Data - Missing / invalid name: ", deviceName)
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	password, ok := param["password"].(string)
	if !ok || password == "" {
		logger.Error(referenceId, "ERROR - Add_Device_Data - Missing password")
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request"
		return result
	}

	readInterval, ok := param["read_interval"].(float64)
	if !ok || readInterval == 0 || readInterval <= 0 {
		logger.Error(referenceId, "ERROR - Add_Device_Data - Missing read interval")
		result.ErrorCode = "400004"
		result.ErrorMessage = "Invalid request"
		return result
	}

	key := os.Getenv("KEY")
	logger.Debug(referenceId, "DEBUG - Add_Device_Data - key:", key)
	if key == "" {
		logger.Error(referenceId, "ERROR - Add_Device_Data - KEY is not set")
		result.ErrorCode = "500000"
		result.ErrorMessage = "Internal server error"
		return result
	}

	chiperPassword, iv, err := crypto.EncryptAES256(password, key)
	if err != nil {
		logger.Error(referenceId, "ERROR - Add_Device_Data - Failed to generate salted password: ", err)
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	logger.Info(referenceId, "INFO - Add_Device_Data - encrypted password generated:", chiperPassword)
	logger.Info(referenceId, "INFO - Add_Device_Data - initialization vector generated:", iv)

	queryCheckDeviceName := `SELECT COUNT(*) FROM device.unit WHERE name = $1;`
	var count int
	if err := conn.Get(&count, queryCheckDeviceName, deviceName); err != nil {
		logger.Error(referenceId, "ERROR - Add_Device_Data - Failed to check existing device name: ", err)
		return utils.ResultFormat{ErrorCode: "500003", ErrorMessage: "Internal Server Error"}
	}
	if count > 0 {
		logger.Error(referenceId, "ERROR - Add_Device_Data - Device name already exists")
		return utils.ResultFormat{ErrorCode: "409000", ErrorMessage: "Conflict"}
	}

	tx, errTx := conn.Beginx()
	if errTx != nil {
		logger.Error(referenceId, "ERROR - Add_Device_Data - Failed to begin transaction: ", errTx)
		return utils.ResultFormat{ErrorCode: "500003", ErrorMessage: "Internal Server Error"}
	}
	defer tx.Rollback()

	var newDeviceID int
	var imageID *int

	if image, hasImage := param["image"].(string); hasImage && image != "" {
		imageName := fmt.Sprintf("%s_%s", deviceName, time.Now().Format("20060102150405"))

		queryImage := `INSERT INTO sysfile.file (name, data) VALUES ($1, $2) RETURNING id;`
		if err := tx.QueryRow(queryImage, imageName, image).Scan(&imageID); err != nil {
			logger.Error(referenceId, "ERROR - Add_Device_Data - Failed to insert image: ", err)
			return utils.ResultFormat{ErrorCode: "500004", ErrorMessage: "Internal Server Error"}
		}
	}

	jsonData := "{}"
	if deviceData, hasData := param["data"].(map[string]any); hasData {
		if jsonDataBytes, err := utils.MapToJSON(deviceData); err == nil {
			jsonData = string(jsonDataBytes)
		} else {
			logger.Error(referenceId, "ERROR - Add_Device_Data - Failed to convert map to JSON: ", err)
			return utils.ResultFormat{ErrorCode: "500008", ErrorMessage: "Internal Server Error"}
		}
	}

	queryDevice := `
		INSERT INTO device.unit (name, st, salt, salted_password, data, image, read_interval)
		VALUES ($1, $2, $3, $4, $5, $6, $7) 
		RETURNING id;
	`
	if err := tx.QueryRow(queryDevice, deviceName, 0, iv, chiperPassword, jsonData, imageID, readInterval).Scan(&newDeviceID); err != nil {
		logger.Error(referenceId, "ERROR - Add_Device_Data - Failed to insert new device: ", err)
		return utils.ResultFormat{ErrorCode: "500005", ErrorMessage: "Internal Server Error"}
	}

	queryActivity := `
		INSERT INTO device.device_activity (unit_id, actor, activity)
		VALUES ($1, $2, 'rsegister');
	`
	if _, err := tx.Exec(queryActivity, newDeviceID, userID); err != nil {
		logger.Error(referenceId, "ERROR - Add_Device_Data - Failed to insert activity log: ", err)
		return utils.ResultFormat{ErrorCode: "500006", ErrorMessage: "Internal Server Error"}
	}

	if err := tx.Commit(); err != nil {
		logger.Error(referenceId, "ERROR - Add_Device_Data - Failed to commit transaction: ", err)
		return utils.ResultFormat{ErrorCode: "500007", ErrorMessage: "Internal Server Error"}
	}

	logger.Info(referenceId, "INFO - Add_Device_Data - Successfully registered device with ID =", newDeviceID)

	result.Payload["status"] = "success"
	result.Payload["device_id"] = newDeviceID

	return result
}
