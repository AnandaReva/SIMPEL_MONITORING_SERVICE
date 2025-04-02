package process

import (
	"database/sql"
	"fmt"
	"monitoring_service/crypto"
	"monitoring_service/logger"
	"monitoring_service/pubsub"
	"monitoring_service/utils"
	"os"
	"strings"
	"time"

	"maps"

	"github.com/jmoiron/sqlx"
)

/*
dont remove comments
{
  "device_id": 28,
  "change_fields": {
    "read_interval": 1,
    "name": "new device name",
	"password" : ""new device password,
    "data": {
      "update": {
        "updated_field1": "value1"
      },
      "insert": {
        "new_field1": "value1",
        "new_field2": "value2"
      },
      "delete": ["deleted_field1", "deleted_field2"]
    } ,
	 "attachment": {
	 	"attachemnt_id": 1,
		 "attachment_change_fields" : {
				 "attachemnt_name": "new name",
				 "attachment_data": "new data",
		}
	 }
  }
}

*/

/*
simpel=> \d device.unit

	                                    Table "device.unit"
	Column      |          Type          | Collation | Nullable |              Default
-----------------+------------------------+-----------+----------+-----------------------------------
	id              | bigint                 |           | not null | nextval('device_id_sq'::regclass)
	name            | character varying(255) |           | not null |
	st              | integer                |           | not null |
	salt            | character varying(64)  |           | not null |
	salted_password | character varying(128) |           | not null |
	data            | jsonb                  |           | not null |
	create_tstamp   | bigint                 |           |          | EXTRACT(epoch FROM now())::bigint
	last_tstamp     | bigint                 |           |          | EXTRACT(epoch FROM now())::bigint
	attachment      | bigint                 |           |          |
	read_interval   | integer                |           | not null |

Indexes:

	"unit_pkey" PRIMARY KEY, btree (id)
	"idx_device_name" btree (name)

Foreign-key constraints:

	"fk_attachment" FOREIGN KEY (attachment) REFERENCES sysfile.file(id) ON DELETE SET NULL

Referenced by:

	TABLE "_timescaledb_internal._hyper_5_13_chunk" CONSTRAINT "13_13_fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
	TABLE "_timescaledb_internal._hyper_5_14_chunk" CONSTRAINT "14_14_fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
	TABLE "device.data" CONSTRAINT "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
	TABLE "device.device_activity" CONSTRAINT "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE

simpel=> \d device.device_activity

	                           Table "device.device_activity"
	Column  |  Type  | Collation | Nullable |                   Default
----------+--------+-----------+----------+---------------------------------------------
	id       | bigint |           | not null | nextval('device.activity_id_seq'::regclass)
	unit_id  | bigint |           | not null |
	actor    | bigint |           |          |
	activity | text   |           | not null |
	tstamp   | bigint |           | not null | EXTRACT(epoch FROM now())::bigint
	before   | jsonb  |           |          |
	after    | jsonb  |           |          |

Indexes:

	"activity_pkey" PRIMARY KEY, btree (id)

Foreign-key constraints:

	"fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
	"fk_user" FOREIGN KEY (actor) REFERENCES sysuser."user"(id) ON DELETE SET NULL
simpel=> \d sysfile.file;
                                        Table "sysfile.file"
 Column |          Type          | Collation | Nullable |                  Default
--------+------------------------+-----------+----------+-------------------------------------------
 id     | bigint                 |           | not null | nextval('sysfile.image_id_seq'::regclass)
 tstamp | bigint                 |           | not null | EXTRACT(epoch FROM now())::bigint
 data   | text                   |           | not null |
 name   | character varying(255) |           | not null |
Indexes:
    "image_pkey" PRIMARY KEY, btree (id)
Referenced by:
    TABLE "device.unit" CONSTRAINT "fk_attachment" FOREIGN KEY (attachment) REFERENCES sysfile.file(id) ON DELETE SET NULL



*/

type DeviceHandler struct {
	Hub *pubsub.WebSocketHub
}

// Update_Device_Data handles updating device data
func Update_Device_Data(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		logger.Debug(referenceId, "DEBUG - Update_Device_Data - Execution completed in ", duration)
	}()

	// Initialize result format
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Update_Device_Data - params: ", param)

	// Validate device_id parameter
	deviceId, ok := param["device_id"].(float64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, "ERROR - Update_Device_Data - Missing or Invalid device_id")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	deviceIdInt := int64(deviceId)

	// Validate change_fields parameter
	changeFields, ok := param["change_fields"].(map[string]any)
	if !ok || len(changeFields) == 0 {
		logger.Error(referenceId, "ERROR - Update_Device_Data - Missing or Invalid change_fields")
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Check if device exists
	var deviceSt int8
	queryCheck := `SELECT st FROM device.unit WHERE id = $1;`
	errCheck := conn.Get(&deviceSt, queryCheck, deviceIdInt)
	if errCheck != nil {
		logger.Error(referenceId, "ERROR - Update_Device_Data - Device ID not found:", deviceIdInt)
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// check if password exist in change_fields

	newDevicePassword, ok := changeFields["password"].(string)
	if !ok || newDevicePassword != "" || len(newDevicePassword) <= 0 {

		key := os.Getenv("KEY")
		logger.Debug(referenceId, "DEBUG - Update_Device_Data - key:", key)
		if key == "" {
			logger.Error(referenceId, "ERROR - Update_Device_Data - KEY is not set")
			result.ErrorCode = "500000"
			result.ErrorMessage = "Internal server error"
			return result
		}

		// Generate hashed password menggunakan PBKDF2
		chiperPassword, iv, err := crypto.EncryptAES256(newDevicePassword, key)
		if err != nil {
			logger.Error(referenceId, "ERROR - Update_Device_Data - Failed to generate salted password: ", err)
			result.ErrorCode = "500001"
			result.ErrorMessage = "Internal server error"
			return result
		}

		// remove password field and value from change_fields
		delete(changeFields, "password")
		// add salt (iv) and salted_password (chiper) in  change_fields

		changeFields["salt"] = iv
		changeFields["salted_password"] = chiperPassword

		logger.Debug(referenceId, "DEBUG - Update_Device_Data - change_fields after handle password: ", changeFields)

	}

	// Begin transaction
	tx, err := conn.Beginx()
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_Device_Data - Failed to start transaction:", err)
		result.ErrorCode = "500000"
		result.ErrorMessage = "Internal server error"
		return result
	}
	defer tx.Rollback()

	// Update field data
	if dataField, ok := changeFields["data"].(map[string]any); ok && len(dataField) > 0 {
		success := updateDeviceDataField(referenceId, tx, deviceIdInt, dataField)
		if !success {
			result.ErrorCode = "500001"
			result.ErrorMessage = "Failed to update device data field"
			return result
		}
	}

	// Update attachment if provided
	if attachment, ok := changeFields["attachment"].(map[string]any); ok && len(attachment) > 0 {
		success := updateDeviceAttachment(referenceId, tx, attachment)
		if !success {
			result.ErrorCode = "500002"
			result.ErrorMessage = "Failed to update device attachment"
			return result
		}
	}

	// Build update query
	updateFields := []string{}
	updateValues := []any{}

	for key, value := range changeFields {
		if key != "data" && key != "attachment" { // Skip "data" and "attachment" since it's handled separately
			updateFields = append(updateFields, fmt.Sprintf("%s = ?", key))
			updateValues = append(updateValues, value)
		}
	}
	updateFields = append(updateFields, "last_tstamp = ?")
	updateValues = append(updateValues, time.Now().Unix())

	updateQuery := fmt.Sprintf("UPDATE device.unit SET %s WHERE id = ?", strings.Join(updateFields, ", "))
	updateValues = append(updateValues, deviceIdInt)

	_, err = tx.Exec(updateQuery, updateValues...)
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_Device_Data - Failed to execute update query:", err)
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Commit transaction
	err = tx.Commit()
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_Device_Data - Failed to commit transaction:", err)
		result.ErrorCode = "500004"
		result.ErrorMessage = "Internal server error"
		return result
	}

	logger.Info(referenceId, "INFO - Update_Device_Data - Device data updated successfully")
	result.Payload["status"] = "success"
	return result
}

// updateDeviceDataField updates the data field of the device
func updateDeviceDataField(referenceId string, tx *sqlx.Tx, deviceId int64, dataField map[string]any) bool {
	var existingData sql.NullString
	querySelect := `SELECT data FROM device.unit WHERE id = $1;`
	err := tx.Get(&existingData, querySelect, deviceId)
	if err != nil {
		logger.Error(referenceId, "ERROR - UpdateDeviceDataField - Failed to fetch current data:", err)
		return false
	}

	currentData := make(map[string]any)
	if existingData.Valid {
		currentData, err = utils.JSONStringToMap(existingData.String)
		if err != nil {
			logger.Error(referenceId, "ERROR - UpdateDeviceDataField - Failed to parse JSON:", err)
			return false
		}
	}

	if updateData, ok := dataField["update"].(map[string]any); ok {
		maps.Copy(currentData, updateData)
	}

	if insertData, ok := dataField["insert"].(map[string]any); ok {
		maps.Copy(currentData, insertData)
	}

	if deleteData, ok := dataField["delete"].([]any); ok {
		for _, field := range deleteData {
			delete(currentData, field.(string))
		}
	}

	updatedJSON, err := utils.MapToJSON(currentData)
	if err != nil {
		logger.Error(referenceId, "ERROR - UpdateDeviceDataField - Failed to convert map to JSON:", err)
		return false
	}

	queryUpdate := `UPDATE device.unit SET data = $1 WHERE id = $2;`
	_, err = tx.Exec(queryUpdate, updatedJSON, deviceId)
	if err != nil {
		logger.Error(referenceId, "ERROR - UpdateDeviceDataField - Failed to update database:", err)
		return false
	}

	logger.Info(referenceId, "INFO - UpdateDeviceDataField - Successfully updated device data field")
	return true
}

// updateDeviceAttachment updates the attachment information for a device
func updateDeviceAttachment(referenceId string, tx *sqlx.Tx, attachment map[string]any) bool {
	attachmentID, ok := attachment["attachment_id"].(float64)
	if !ok || attachmentID <= 0 {
		logger.Error(referenceId, "ERROR - UpdateDeviceAttachment - Missing or Invalid attachment_id")
		return false
	}

	changeFields, ok := attachment["attachment_change_fields"].(map[string]any)
	if !ok {
		logger.Error(referenceId, "ERROR - UpdateDeviceAttachment - Invalid attachment_change_fields format")
		return false
	}

	// Build update query
	updateFields := []string{}
	updateValues := []any{}

	for key, value := range changeFields {
		updateFields = append(updateFields, fmt.Sprintf("%s = ?", key))
		updateValues = append(updateValues, value)
	}
	updateFields = append(updateFields, "tstamp = ?")
	updateValues = append(updateValues, time.Now().Unix())

	updateQuery := fmt.Sprintf("UPDATE sysfile.file SET %s WHERE id = ?", strings.Join(updateFields, ", "))
	updateValues = append(updateValues, attachmentID)

	_, err := tx.Exec(updateQuery, updateValues...)
	if err != nil {
		logger.Error(referenceId, "ERROR - UpdateDeviceAttachment - Failed to execute update query:", err)
		return false
	}

	logger.Info(referenceId, "INFO - UpdateDeviceAttachment - Successfully updated device attachment")
	return true
}
