package process

import (
	"database/sql"
	"encoding/json"
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
	 	"update": {
	 	"attachment_id": 1,
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
	var deviceName string
	var deviceSt int8
	queryCheck := `SELECT name, st FROM device.unit WHERE id = $1;`
	errCheck := conn.QueryRow(queryCheck, deviceIdInt).Scan(&deviceName, &deviceSt)
	if errCheck != nil {
		logger.Error(referenceId, "ERROR - Update_Device_Data - Device ID not found:", deviceIdInt)
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request"
		return result
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

	// Inisialisasi beforeData dan afterData sebagai variabel lokal
	beforeData := make(map[string]any)
	afterData := make(map[string]any)

	// Tambah last_tstamp
	changeFields["last_tstamp"] = time.Now().Unix()

	// check if password exist in change_fields

	if newDevicePassword, ok := changeFields["password"].(string); ok && newDevicePassword != "" {
		// Hanya proses password jika field "password" ada dan tidak kosong
		key := os.Getenv("KEY")
		logger.Debug(referenceId, "DEBUG - Update_Device_Data - key:", key)

		if key == "" {
			logger.Error(referenceId, "ERROR - Update_Device_Data - KEY is not set")
			result.ErrorCode = "500000"
			result.ErrorMessage = "Internal server error"
			return result
		}

		afterData["password"] = newDevicePassword

		// Encrypt password baru
		chiperPassword, iv, err := crypto.EncryptAES256(newDevicePassword, key)
		if err != nil {
			logger.Error(referenceId, "ERROR - Update_Device_Data - Failed to encrypt password:", err)
			result.ErrorCode = "500001"
			result.ErrorMessage = "Internal server error"
			return result
		}

		// Hapus field "password" dari perubahan, ganti dengan salt dan salted_password
		delete(changeFields, "password")
		changeFields["salt"] = iv
		changeFields["salted_password"] = chiperPassword

		// Ambil data password lama
		var oldSalt sql.NullString
		var oldSaltedPassword sql.NullString

		querySelect := `SELECT salt, salted_password FROM device.unit WHERE id = $1;`
		err = tx.QueryRow(querySelect, deviceIdInt).Scan(&oldSalt, &oldSaltedPassword)
		if err != nil {
			logger.Error(referenceId, "ERROR - Update_Device_Data - Failed to fetch original device data:", err)
			result.ErrorCode = "500002"
			result.ErrorMessage = "Internal server error"
			return result
		}

		// Decrypt password lama jika valid
		if oldSalt.Valid && oldSaltedPassword.Valid {
			oldPlainPassword, err := crypto.DecryptAES256(oldSaltedPassword.String, oldSalt.String, key)
			if err != nil {
				logger.Error(referenceId, "ERROR - Update_Device_Data - Failed to decrypt old password:", err)
				result.ErrorCode = "500003"
				result.ErrorMessage = "Internal server error"
				return result
			}
			beforeData["password"] = oldPlainPassword
		} else {
			logger.Warning(referenceId, "WARN - Update_Device_Data - Old password data is not valid")
			beforeData["password"] = nil
		}

		// Simpan password baru dalam bentuk plain text ke afterData
		afterData["password"] = newDevicePassword

		logger.Debug(referenceId, "DEBUG - Update_Device_Data - changeFields after password handled:", changeFields)
	}
	/////////////  HANDLE UDPATE DATA FIELD ///////////

	// Update field data
	if dataField, ok := changeFields["data"].(map[string]any); ok && len(dataField) > 0 {
		// Pass beforeData dan afterData sebagai parameter
		success := updateDeviceDataField(referenceId, tx, deviceIdInt, dataField, beforeData, afterData)
		if !success {
			result.ErrorCode = "500001"
			result.ErrorMessage = "Failed to update device data field"
			return result
		}
	}
	/////////////  HANDLE UDPATE ATTACHEMENT FIELD ///////////

	/* if rawAttachment, ok := changeFields["attachment"].(map[string]any); ok {
		logger.Debug(referenceId, "DEBUG - Update_Device_Data - rawAttachment:", rawAttachment)
		if insertData, ok := rawAttachment["insert"].(map[string]any); ok {
			logger.Debug(referenceId, "DEBUG - Update_Device_Data - attachment.insert:", insertData)
			// INSERT attachment (insert baru)
			rawAttachmentData, hasData := insertData["attachment_data"]

			if !hasData {
				logger.Error(referenceId, "ERROR - Missing attachment_data in insert")
				result.ErrorCode = "400010"
				result.ErrorMessage = "Missing attachment_data for insert"
				return result
			}

			attachmentData, ok := rawAttachmentData.(string)
			if !ok {
				logger.Error(referenceId, "ERROR - attachment_data is not a string (insert)")
				result.ErrorCode = "400011"
				result.ErrorMessage = "Invalid attachment_data for insert"
				return result
			}

			imageName := fmt.Sprintf("%s_%s", deviceName, time.Now().Format("20060102150405"))

			// Insert baru → simpan ke tabel sysfile.file
			insertQuery := `
				INSERT INTO sysfile.file (data, name, tstamp)
				VALUES ($1, $2, EXTRACT(epoch FROM now())::bigint)
				RETURNING id;
			`

			var newattachmentId int64
			err := tx.QueryRow(insertQuery, attachmentData, imageName).Scan(&newattachmentId)
			if err != nil {
				logger.Error(referenceId, "ERROR - Failed to insert attachment:", err)
				result.ErrorCode = "500011"
				result.ErrorMessage = "Failed to insert attachment"
				return result
			}

			logger.Info(referenceId, "INFO - Inserted new attachment with ID:", newattachmentId)

			// Set ID baru ke kolom device (misal update ke device.unit)
			updateDeviceQuery := `UPDATE device.unit SET attachment = $1 WHERE id = $2;`
			_, err = tx.Exec(updateDeviceQuery, newattachmentId, deviceIdInt)
			if err != nil {
				logger.Error(referenceId, "ERROR - Failed to update device with new attachment:", err)
				result.ErrorCode = "500012"
				result.ErrorMessage = "Failed to update device attachment"
				return result
			}

		} else if updateData, ok := rawAttachment["update"].(map[string]interface{}); ok {
			// UPDATE attachment
			logger.Debug(referenceId, "DEBUG - Update_Device_Data - attachment.update:", updateData)
			rawattachmentId, hasId := updateData["attachment_id"]
			rawAttachmentData, hasData := updateData["attachment_data"]

			if !hasId || !hasData {
				logger.Error(referenceId, "ERROR - Missing fields in attachment update")
				result.ErrorCode = "400020"
				result.ErrorMessage = "Missing attachment_id or data for update"
				return result
			}

			attachmentIdFloat, ok := rawattachmentId.(float64)
			if !ok {
				logger.Error(referenceId, "ERROR - attachment_id is not a number (update)")
				result.ErrorCode = "400021"
				result.ErrorMessage = "Invalid attachment_id for update"
				return result
			}

			attachmentData, ok := rawAttachmentData.(string)
			if !ok {
				logger.Error(referenceId, "ERROR - attachment_data is not a string (update)")
				result.ErrorCode = "400022"
				result.ErrorMessage = "Invalid attachment_data for update"
				return result
			}

			imageName := fmt.Sprintf("%s_%s", deviceName, time.Now().Format("20060102150405"))

			success := handleUpdateDeviceAttachment(referenceId, tx, sql.NullInt64{Int64: int64(attachmentIdFloat), Valid: true}, attachmentData, imageName, deviceIdInt, beforeData, afterData)
			if !success {
				result.ErrorCode = "500013"
				result.ErrorMessage = "Failed to update attachment"
				return result
			}

		} else if deleteData, ok := rawAttachment["delete"].(map[string]interface{}); ok {
			// DELETE attachment
			logger.Debug(referenceId, "DEBUG - Update_Device_Data - attachment.delete:", deleteData)
			rawattachmentId, hasId := deleteData["attachment_id"]
			if !hasId {
				logger.Error(referenceId, "ERROR - Missing attachment_id for delete")
				result.ErrorCode = "400030"
				result.ErrorMessage = "Missing attachment_id for delete"
				return result
			}

			attachmentIdFloat, ok := rawattachmentId.(float64)
			if !ok {
				logger.Error(referenceId, "ERROR - attachment_id is not a number (delete)")
				result.ErrorCode = "400031"
				result.ErrorMessage = "Invalid attachment_id for delete"
				return result
			}

			deleteQuery := `DELETE FROM sysfile.file WHERE id = $1;`
			_, err := tx.Exec(deleteQuery, int64(attachmentIdFloat))
			if err != nil {
				logger.Error(referenceId, "ERROR - Failed to delete attachment:", err)
				result.ErrorCode = "500014"
				result.ErrorMessage = "Failed to delete attachment"
				return result
			}

			logger.Info(referenceId, "INFO - Attachment deleted successfully")
		} else {
			logger.Info(referenceId, "INFO - No attachment changes detected")
		}
	} */

	// Main function usage
	if rawAttachment, ok := changeFields["attachment"].(map[string]any); ok {
		success, err := handleUpdateDeviceAttachment(referenceId, tx, rawAttachment, deviceIdInt, deviceName, beforeData, afterData)
		if !success {
			logger.Info(referenceId, "INFO - Error wijle deleting attachement")
			result.ErrorCode = "500013"
			result.ErrorMessage = err.Error()
			return result
		}
		logger.Info(referenceId, "INFO - Attachment deleted successfully")
	}

	// Build update query
	updateFields := []string{}
	updateValues := []any{}
	fieldNames := []string{}
	paramIndex := 1

	for key, value := range changeFields {
		if key != "data" && key != "attachment" && key != "salt" && key != "salted_password" && key != "last_tstamp" {
			updateFields = append(updateFields, fmt.Sprintf("%s = $%d", key, paramIndex))
			updateValues = append(updateValues, value)
			fieldNames = append(fieldNames, key) // hanya yang ini dimasukkan ke before/after log
			paramIndex++
		} else if key == "salt" || key == "salted_password" || key == "last_tstamp" {
			updateFields = append(updateFields, fmt.Sprintf("%s = $%d", key, paramIndex))
			updateValues = append(updateValues, value)
			paramIndex++
		}
	}

	logger.Debug(referenceId, " - Update_Device_Data - fieldNames: ", fieldNames)

	// Ambil beforeData
	if len(fieldNames) > 0 {
		fieldList := strings.Join(fieldNames, ", ")
		queryToGetBeforeData := fmt.Sprintf(`SELECT %s FROM device.unit WHERE id = $1`, fieldList)

		row := make(map[string]any)
		err := tx.QueryRowx(queryToGetBeforeData, deviceId).MapScan(row)
		if err != nil {
			logger.Error(referenceId, "ERROR - Failed to fetch beforeData for updated fields:", err)
			result.ErrorCode = "500004"
			result.ErrorMessage = "Internal server error"
			return result
		}

		// === Simpan log before ===
		for _, key := range fieldNames {
			if val, exists := row[key]; exists {
				beforeData[key] = val
			}
		}

		// === Simpan log after ===
		for _, key := range fieldNames {
			if val, exists := changeFields[key]; exists {
				afterData[key] = val

			}
		}
	}

	// Tambahkan last_tstamp
	// updateFields = append(updateFields, fmt.Sprintf("last_tstamp = $%d", paramIndex))
	// updateValues = append(updateValues, time.Now().Unix())
	// paramIndex++

	// Tambahkan WHERE id
	query := fmt.Sprintf("UPDATE device.unit SET %s WHERE id = $%d", strings.Join(updateFields, ", "), paramIndex)
	updateValues = append(updateValues, deviceIdInt)

	logger.Debug(referenceId, "Update_Device_Data - final update query: ", query)
	logger.Debug(referenceId, "Update_Device_Data - final updateValues: ", updateValues)

	_, err = tx.Exec(query, updateValues...)
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_Device_Data - Failed to execute update query:", err)
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal server error"
		return result
	}

	//////////////////////// INSERT ACTIVITY LOG ////////////////////////

	// INSERT ACTIVITY LOG
	queryToInsertActivity := `
	INSERT INTO device.device_activity (
		unit_id, actor, activity, before, after
		) VALUES ($1, $2, $3, $4, $5 )`

	beforeJson, err := json.Marshal(beforeData)
	if err != nil {
		logger.Error(referenceId, "ERROR - Failed to marshal beforeData:", err)
		result.ErrorCode = "500004"
		result.ErrorMessage = "Internal server error"
		return result
	}

	afterJson, err := json.Marshal(afterData)
	if err != nil {
		logger.Error(referenceId, "ERROR - Failed to marshal afterData:", err)
		result.ErrorCode = "500004"
		result.ErrorMessage = "Internal server error"
		return result
	}

	_, err = tx.Exec(queryToInsertActivity,
		deviceIdInt, // unit_id
		userID,      // actor, pastikan kamu punya nilai ini
		"Update",    // activity
		string(beforeJson),
		string(afterJson),
	)

	if err != nil {
		logger.Error(referenceId, "ERROR - Failed to insert activity log:", err)
		result.ErrorCode = "500005"
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
func updateDeviceDataField(referenceId string, tx *sqlx.Tx, deviceId int64, dataField map[string]any,
	beforeData, afterData map[string]any) bool {

	var err error

	// Ambil data eksisting sebelum perubahan
	var existingDataRaw sql.NullString
	querySelect := `SELECT data FROM device.unit WHERE id = $1;`
	err = tx.Get(&existingDataRaw, querySelect, deviceId)
	if err != nil {
		logger.Error(referenceId, "ERROR - UpdateDeviceDataField - Failed to fetch current data:", err)
		return false
	}

	currentData := make(map[string]any)
	if existingDataRaw.Valid {
		currentData, err = utils.JSONStringToMap(existingDataRaw.String)
		if err != nil {
			logger.Error(referenceId, "ERROR - UpdateDeviceDataField - Failed to parse JSON:", err)
			return false
		}
	}

	// Siapkan before/after snapshot
	originalBefore := make(map[string]any)
	updatedAfter := maps.Clone(currentData)

	// Step 1: Handle delete fields
	if deleteData, ok := dataField["delete"].(map[string]any); ok && len(deleteData) > 0 {
		for key := range deleteData {
			// Simpan value sebelum dihapus
			if val, exists := currentData[key]; exists {
				originalBefore[key] = val
				delete(updatedAfter, key)
			}
		}

		// Hapus dari database
		expr := ""
		for key := range deleteData {
			expr += " - '" + key + "'"
		}
		queryDelete := fmt.Sprintf(`UPDATE device.unit SET data = data%s WHERE id = $1;`, expr)
		_, err = tx.Exec(queryDelete, deviceId)
		if err != nil {
			logger.Error(referenceId, "ERROR - UpdateDeviceDataField - Failed to delete fields from JSON:", err)
			return false
		}
	}

	// Step 2: Handle update fields
	if updateData, ok := dataField["update"].(map[string]any); ok {
		for key, newVal := range updateData {
			if oldVal, exists := currentData[key]; exists && oldVal != newVal {
				originalBefore[key] = oldVal
				updatedAfter[key] = newVal
			}
		}
	}

	// Step 3: Handle insert fields
	if insertData, ok := dataField["insert"].(map[string]any); ok {
		for key, newVal := range insertData {
			if _, exists := currentData[key]; !exists {
				updatedAfter[key] = newVal
			}
		}
	}

	// Step 4: Simpan hasil akhir ke database
	updatedJSON, err := utils.MapToJSON(updatedAfter)
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

	// Step 5: Simpan ke beforeData dan afterData global
	if len(originalBefore) > 0 {
		beforeData["data"] = originalBefore
	}
	if len(updatedAfter) > 0 {
		afterData["data"] = updatedAfter
	}

	logger.Info(referenceId, "INFO - UpdateDeviceDataField - Successfully updated device data field")
	return true
}

/* \d sysfile.file
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

// handleUpdateDeviceAttachment updates the attachment information for a device
func handleUpdateDeviceAttachment(referenceId string, tx *sqlx.Tx, rawAttachment map[string]any, deviceIdInt int64, deviceName string, beforeData, afterData map[string]any) (bool, error) {
	if insertData, ok := rawAttachment["insert"].(map[string]any); ok {
		logger.Debug(referenceId, "DEBUG - Update_Device_Data - attachment.insert:", insertData)

		// INSERT attachment (new attachment)
		rawAttachmentData, hasData := insertData["attachment_data"]
		if !hasData {
			return false, fmt.Errorf("missing attachment_data for insert")
		}

		attachmentData, ok := rawAttachmentData.(string)
		if !ok {
			return false, fmt.Errorf("invalid attachment_data for insert")
		}

		imageName := fmt.Sprintf("%s_%s", deviceName, time.Now().Format("20060102150405"))

		// Insert new attachment
		insertQuery := `
            INSERT INTO sysfile.file (data, name, tstamp)
            VALUES ($1, $2, EXTRACT(epoch FROM now())::bigint)
            RETURNING id;
        `

		var newAttachmentId int64
		err := tx.QueryRow(insertQuery, attachmentData, imageName).Scan(&newAttachmentId)
		if err != nil {
			return false, fmt.Errorf("failed to insert attachment: %v", err)
		}

		logger.Info(referenceId, "INFO - Inserted new attachment with ID:", newAttachmentId)

		// Update device with new attachment ID
		updateDeviceQuery := `UPDATE device.unit SET attachment = $1 WHERE id = $2;`
		_, err = tx.Exec(updateDeviceQuery, newAttachmentId, deviceIdInt)
		if err != nil {
			return false, fmt.Errorf("failed to update device with new attachment: %v", err)
		}
		// set beforedata
		beforeData["attachment"] = map[string]any{}

		// Set afterData for insert
		afterData["attachment"] = map[string]any{
			"name": imageName,
		}

	} else if updateData, ok := rawAttachment["update"].(map[string]any); ok {
		// UPDATE attachment
		logger.Debug(referenceId, "DEBUG - Update_Device_Data - attachment.update:", updateData)

		rawAttachmentId, hasId := updateData["attachment_id"]
		rawAttachmentData, hasData := updateData["attachment_data"]

		if !hasId || !hasData {
			return false, fmt.Errorf("missing attachment_id or data for update")
		}

		attachmentIdFloat, ok := rawAttachmentId.(float64)
		if !ok {
			return false, fmt.Errorf("invalid attachment_id for update")
		}

		attachmentData, ok := rawAttachmentData.(string)
		if !ok {
			return false, fmt.Errorf("invalid attachment_data for update")
		}

		attachmentId := sql.NullInt64{Int64: int64(attachmentIdFloat), Valid: true}
		imageName := fmt.Sprintf("%s_%s", deviceName, time.Now().Format("20060102150405"))

		// Get existing attachment data
		var existingAttachment struct {
			Name string `db:"name"`
			Data string `db:"data"`
		}
		selectQuery := `SELECT name, data FROM sysfile.file WHERE id = $1`
		err := tx.Get(&existingAttachment, selectQuery, attachmentId.Int64)
		if err != nil {
			return false, fmt.Errorf("failed to fetch existing attachment data: %v", err)
		}

		// Set beforeData
		beforeData["attachment"] = map[string]any{}
		if existingAttachment.Name != "" {
			beforeData["attachment"] = map[string]any{
				"name": existingAttachment.Name,
			}
		}

		// Update attachment
		updateQuery := `
            UPDATE sysfile.file 
            SET data = $1, name = $2, tstamp = EXTRACT(epoch FROM now())::bigint 
            WHERE id = $3;
        `
		_, err = tx.Exec(updateQuery, attachmentData, imageName, attachmentId.Int64)
		if err != nil {
			return false, fmt.Errorf("failed to update attachment: %v", err)
		}

		// Set afterData
		afterData["attachment"] = map[string]any{
			"name": imageName,
		}

	} else if deleteData, ok := rawAttachment["delete"].(map[string]any); ok {
		// DELETE attachment
		logger.Debug(referenceId, "DEBUG - Update_Device_Data - attachment.delete:", deleteData)

		rawAttachmentId, hasId := deleteData["attachment_id"]
		if !hasId {
			return false, fmt.Errorf("missing attachment_id for delete")
		}

		attachmentIdFloat, ok := rawAttachmentId.(float64)
		if !ok {
			return false, fmt.Errorf("invalid attachment_id for delete")
		}

		// Get existing attachment data for beforeData
		var existingAttachment struct {
			Name string `db:"name"`
			Data string `db:"data"`
		}
		selectQuery := `SELECT name, data FROM sysfile.file WHERE id = $1`
		err := tx.Get(&existingAttachment, selectQuery, int64(attachmentIdFloat))
		if err != nil {
			return false, fmt.Errorf("failed to fetch existing attachment data: %v", err)
		}

		// Set beforeData
		beforeData["attachment"] = map[string]any{}
		if existingAttachment.Name != "" {
			beforeData["attachment"] = map[string]any{
				"name": existingAttachment.Name,
			}
		}

		// Delete attachment
		deleteQuery := `DELETE FROM sysfile.file WHERE id = $1;`
		_, err = tx.Exec(deleteQuery, int64(attachmentIdFloat))
		if err != nil {
			return false, fmt.Errorf("failed to delete attachment: %v", err)
		}

		// Set afterData for delete
		afterData["attachment"] = map[string]any{}

		logger.Info(referenceId, "INFO - Attachment deleted successfully")
	} else {
		logger.Info(referenceId, "INFO - No attachment changes detected")
		return true, nil
	}

	return true, nil
}
