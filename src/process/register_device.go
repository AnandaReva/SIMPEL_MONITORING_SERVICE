package process

import (
	"fmt"
	"monitoring_service/configs"
	"monitoring_service/crypto"
	"monitoring_service/logger"
	"monitoring_service/utils"
	"time"

	"github.com/jmoiron/sqlx"
)

// Register_Device mendaftarkan perangkat baru ke dalam sistem secara dinamis
func Register_Device(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Register_Device - param: ", param)

	// Validasi device name
	deviceName, ok := param["name"].(string)
	if !ok || deviceName == "" || len(deviceName) > 20 {
		logger.Error(referenceId, "ERROR - Register_Device - Missing / invalid name: ", deviceName)
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Validasi password
	password, ok := param["password"].(string)
	if !ok || password == "" {
		logger.Error(referenceId, "ERROR - Register_Device - Missing password")
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// interval
	readInterval, ok := param["read_interval"].(float64)
	if !ok || readInterval == 0 || readInterval <= 0 {
		logger.Error(referenceId, "ERROR - Regiter_device - Missing read interval")
		result.ErrorCode = "400004"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Generate salt
	salt, err := utils.RandomStringGenerator(16)
	if err != "" {
		logger.Error(referenceId, "ERROR - Register_Device - Failed to generate salt: ", err)
		result.ErrorCode = "500000"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Generate hashed password menggunakan PBKDF2
	saltedPassword, errSaltedPass := crypto.GeneratePBKDF2(password, salt, 32, configs.GetPBKDF2Iterations())
	if errSaltedPass != "" {
		logger.Error(referenceId, "ERROR - Register_Device - Failed to generate salted password: ", errSaltedPass)
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	logger.Info(referenceId, "INFO - Register_Device - Salt generated:", salt)
	logger.Info(referenceId, "INFO - Register_Device - Salted password generated:", saltedPassword)

	// Cek apakah device name sudah ada di database
	queryCheckDeviceName := `SELECT COUNT(*) FROM device.unit WHERE name = $1;`
	var count int
	if err := conn.Get(&count, queryCheckDeviceName, deviceName); err != nil {
		logger.Error(referenceId, "ERROR - Register_Device - Failed to check existing device name: ", err)
		return utils.ResultFormat{ErrorCode: "500003", ErrorMessage: "Internal Server Error"}
	}

	if count > 0 {
		logger.Error(referenceId, "ERROR - Register_Device - Device name already exists")
		return utils.ResultFormat{ErrorCode: "409000", ErrorMessage: "Conflict"}
	}

	// Mulai transaksi
	tx, errTx := conn.Beginx()
	if errTx != nil {
		logger.Error(referenceId, "ERROR - Register_Device - Failed to begin transaction: ", errTx)
		return utils.ResultFormat{ErrorCode: "500003", ErrorMessage: "Internal Server Error"}
	}
	defer tx.Rollback() // Akan dieksekusi jika terjadi error

	var newDeviceID int
	var attachmentID *int

	// Cek apakah ada attachment yang dikirim
	if attachment, hasAttachment := param["attachment"].(string); hasAttachment && attachment != "" {
		imageName := fmt.Sprintf("%s_%s", deviceName, time.Now().Format("20060102150405"))

		queryAttachment := `INSERT INTO sysfile.file (name, data) VALUES ($1, $2) RETURNING id;`
		if err := tx.QueryRow(queryAttachment, imageName, attachment).Scan(&attachmentID); err != nil {
			logger.Error(referenceId, "ERROR - Register_Device - Failed to insert attachment: ", err)
			return utils.ResultFormat{ErrorCode: "500004", ErrorMessage: "Internal Server Error"}
		}
	}

	// Cek apakah ada data tambahan
	jsonData := "{}"
	if deviceData, hasData := param["data"].(map[string]any); hasData {
		if jsonDataBytes, err := utils.MapToJSON(deviceData); err == nil {
			jsonData = string(jsonDataBytes)
		} else {
			logger.Error(referenceId, "ERROR - Register_Device - Failed to convert map to JSON: ", err)
			return utils.ResultFormat{ErrorCode: "500008", ErrorMessage: "Internal Server Error"}
		}
	}

	// Insert device
	queryDevice := `
		INSERT INTO device.unit (name, st, salt, salted_password, data, attachment, read_interval)
		VALUES ($1, $2, $3, $4, $5, $6, $7) 
		RETURNING id;
	`
	if err := tx.QueryRow(queryDevice, deviceName, 0, salt, saltedPassword, jsonData, attachmentID, readInterval).Scan(&newDeviceID); err != nil {
		logger.Error(referenceId, "ERROR - Register_Device - Failed to insert new device: ", err)
		return utils.ResultFormat{ErrorCode: "500005", ErrorMessage: "Internal Server Error"}
	}

	// Insert ke activity log
	queryActivity := `
		INSERT INTO device.device_activity (unit_id, actor, activity)
		VALUES ($1, $2, 'Register device');
	`
	if _, err := tx.Exec(queryActivity, newDeviceID, userID); err != nil {
		logger.Error(referenceId, "ERROR - Register_Device - Failed to insert activity log: ", err)
		return utils.ResultFormat{ErrorCode: "500006", ErrorMessage: "Internal Server Error"}
	}

	// Commit transaksi jika berhasil
	if err := tx.Commit(); err != nil {
		logger.Error(referenceId, "ERROR - Register_Device - Failed to commit transaction: ", err)
		return utils.ResultFormat{ErrorCode: "500007", ErrorMessage: "Internal Server Error"}
	}

	logger.Info(referenceId, "INFO - Register_Device - Successfully registered device with ID =", newDeviceID)

	// Kirim response sukses
	result.Payload["status"] = "success"
	result.Payload["device_id"] = newDeviceID

	return result
}
