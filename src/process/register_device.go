package process

import (
	"monitoring_service/configs"
	"monitoring_service/crypto"
	"monitoring_service/logger"
	"monitoring_service/utils"
	"time"

	"github.com/jmoiron/sqlx"
)

// Register_Device mendaftarkan perangkat baru ke dalam sistem
func Register_Device(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

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

	// Generate salt
	salt, err := utils.RandomStringGenerator(16)
	if err != "" {
		logger.Error(referenceId, "ERROR - Register_devce -  Failed to generate salt: ", err)
		result.ErrorCode = "500000"
		result.ErrorMessage = "Internal server error"

		return result
	}

	// Generate hashed password menggunakan PBKDF2
	saltedPassword, errSaltedPass := crypto.GeneratePBKDF2(password, salt, 32, configs.GetPBKDF2Iterations())
	if errSaltedPass != "" {
		logger.Error(referenceId, "ERROR - Register_devce -  Failed to generate salted password: ", errSaltedPass)
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"

		return result
	}

	logger.Info(referenceId, "INFO - Register_devce -  Salt generated:", salt)
	logger.Info(referenceId, "INFO - Register_devce -  Salted password generated:", saltedPassword)

	// Cek apakah device name sudah ada di database
	queryCheckDeviceName := `SELECT COUNT(*) FROM device.unit WHERE name = $1;`

	var count int
	errQuery := conn.Get(&count, queryCheckDeviceName, deviceName)
	if errQuery != nil {
		logger.Error(referenceId, "ERROR - Register_devce -  Failed to check existing device name: ", errQuery)
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal Server Error"

		return result
	}

	if count > 0 {
		logger.Error(referenceId, "ERROR - Register_devce -  Device name already exists")
		result.ErrorCode = "409000"
		result.ErrorMessage = "Conflict"

		return result
	}

	tx, errTx := conn.Beginx()

	if errTx != nil {
		logger.Error(referenceId, "ERROR - Register_device - Failed to begin transaction: ", errTx)
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal Server Error"
		return result
	}

	var newDeviceID int
	query := `
	WITH new_device AS (
		INSERT INTO device.unit (name, status, salt, salted_password, data, create_tstamp)
		VALUES ($1, $2, $3, $4, $5, $6) 
		RETURNING id
	)
	INSERT INTO device.device_activity (unit_id, actor, activity, tstamp)
	SELECT id, $7, 'Register device', EXTRACT(epoch FROM now())::bigint FROM new_device
	RETURNING unit_id;
`

	var createTstamp = time.Now().Unix()

	errQuery2 := tx.QueryRow(query, deviceName, 0, salt, saltedPassword, "{}", createTstamp, userID).Scan(&newDeviceID)
	if errQuery2 != nil {
		tx.Rollback()
		logger.Error(referenceId, "ERROR - Register_device - Failed to insert new device and activity: ", errQuery2)
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal Server Error"
		return result
	}

	// Commit transaksi jika berhasil
	errCommit := tx.Commit()
	if errCommit != nil {
		logger.Error(referenceId, "ERROR - Register_device - Failed to commit transaction: ", errCommit)
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal Server Error"
		return result
	}

	logger.Info(referenceId, "INFO - Register_device - Successfully registered device with ID =", newDeviceID)

	// Kirim response sukses
	result.Payload["status"] = "success"
	result.Payload["device_id"] = newDeviceID

	return result

}
