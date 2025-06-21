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
	 "image": {
	 	"update": {
	 	"file_id": 1,
		"file_data": "new data",
	}
	 }
  }
}

*/

/*
simpel=>
       Table "device.unit"
      Column      |          Type          | Collation | Nullable |              Default
------------------+------------------------+-----------+----------+-----------------------------------
 id               | bigint                 |           | not null | nextval('device_id_sq'::regclass)
 name             | character varying(255) |           | not null |
 st               | integer                |           | not null |
 data             | jsonb                  |           | not null |
 create_timestamp | bigint                 |           |          | EXTRACT(epoch FROM now())::bigint
 last_timestamp   | bigint                 |           |          | EXTRACT(epoch FROM now())::bigint
 image            | bigint                 |           |          |
 read_interval    | integer                |           | not null |
 salted_password  | character varying(128) |           | not null |
 salt             | character varying(32)  |           | not null |
Indexes:
    "unit_pkey" PRIMARY KEY, btree (id)
    "idx_device_name" btree (name)
    "uq_device_unit_name" UNIQUE CONSTRAINT, btree (name)
Foreign-key constraints:
    "fk_attachment" FOREIGN KEY (image) REFERENCES sysfile.file(id) ON DELETE SET NULL
Referenced by:
    TABLE "_timescaledb_internal._hyper_5_471_chunk" CONSTRAINT "471_471_fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE

simpel=> \d device.device_activity;
                             Table "device.device_activity"
  Column   |  Type  | Collation | Nullable |                   Default
-----------+--------+-----------+----------+---------------------------------------------
 id        | bigint |           | not null | nextval('device.activity_id_seq'::regclass)
 unit_id   | bigint |           | not null |
 actor     | bigint |           |          |
 activity  | text   |           | not null |
 timestamp | bigint |           | not null | EXTRACT(epoch FROM now())::bigint
 before    | jsonb  |           |          |
 after     | jsonb  |           |          |
Indexes:
    "activity_pkey" PRIMARY KEY, btree (id)
Foreign-key constraints:
    "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
    "fk_user" FOREIGN KEY (actor) REFERENCES sysuser."user"(id) ON DELETE SET NULL



simpel=> \d sysfile.file;
                                         Table "sysfile.file"
  Column   |          Type          | Collation | Nullable |                  Default
-----------+------------------------+-----------+----------+-------------------------------------------
 id        | bigint                 |           | not null | nextval('sysfile.image_id_seq'::regclass)
 timestamp | bigint                 |           | not null | EXTRACT(epoch FROM now())::bigint
 data      | text                   |           | not null |
 name      | character varying(255) |           | not null |
Indexes:
    "image_pkey" PRIMARY KEY, btree (id)
Referenced by:
    TABLE "device.unit" CONSTRAINT "fk_attachment" FOREIGN KEY (image) REFERENCES sysfile.file(id) ON DELETE SET NULL


*/

type DeviceHandler struct {
	Hub *pubsub.WebSocketHub
}

var newDeviceCredentials = map[string]any{
	"name":     "",
	"password": "",
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

	// Tambah last_timestamp
	changeFields["last_timestamp"] = time.Now().Unix()

	// check if password exist in change_fields

	if newDevicePassword, ok := changeFields["password"].(string); ok && newDevicePassword != "" && len(newDevicePassword) < 8 {
		success := updateDevicePassword(referenceId, tx, deviceIdInt, newDevicePassword, beforeData, afterData, changeFields)

		if success {

			newDeviceCredentials["password"] = newDevicePassword

		} else {
			result.ErrorCode = "500001"
			result.ErrorMessage = "Failed to update device data Passsword"
			return result
		}
	}

	if newDeviceName, ok := changeFields["name"].(string); ok && newDeviceName != "" {

		newDeviceCredentials["password"] = newDeviceName
	}

	/////////////  HANDLE UDPATE DATA FIELD ///////////

	// Update field data
	if dataField, ok := changeFields["data"].(map[string]any); ok && len(dataField) > 0 {
		// Pass beforeData dan afterData sebagai parameter
		success := updateDeviceDataField(referenceId, tx, deviceIdInt, dataField, beforeData, afterData)
		if !success {
			result.ErrorCode = "500002"
			result.ErrorMessage = "Failed to update device data field"
			return result
		}
	}
	/////////////  HANDLE UDPATE IMAGE FIELD ///////////
	// Main function usage
	if rawImage, ok := changeFields["image"].(map[string]any); ok {
		success, err := handleUpdateDeviceImage(referenceId, tx, rawImage, deviceIdInt, deviceName, beforeData, afterData)
		if !success {
			logger.Info(referenceId, "INFO - Error while updating image")
			result.ErrorCode = "500003"
			result.ErrorMessage = err.Error()
			return result
		}
		logger.Info(referenceId, "INFO - image updated successfully")
	}

	// Build update query
	updateFields := []string{}
	updateValues := []any{}
	fieldNames := []string{}
	paramIndex := 1

	for key, value := range changeFields {
		if key != "data" && key != "image" && key != "salt" && key != "salted_password" && key != "last_timestamp" {
			updateFields = append(updateFields, fmt.Sprintf("%s = $%d", key, paramIndex))
			updateValues = append(updateValues, value)
			fieldNames = append(fieldNames, key) // hanya yang ini dimasukkan ke before/after log
			paramIndex++
		} else if key == "salt" || key == "salted_password" || key == "last_timestamp" {
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

	// Tambahkan last_timestamp
	// updateFields = append(updateFields, fmt.Sprintf("last_timestamp = $%d", paramIndex))
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
		"update",    // activity
		string(beforeJson),
		string(afterJson),
	)

	if err != nil {
		logger.Error(referenceId, "ERROR - Failed to insert activity log:", err)
		result.ErrorCode = "500005"
		result.ErrorMessage = "Internal server error"
		return result
	}

	
	if deviceSt == 1 {
		hub, err := pubsub.GetWebSocketHub(referenceId)
		if err != nil {
			logger.Error(referenceId, "ERROR - Update_Device_Data - Failed to get WebSocketHub:", err)
			result.ErrorCode = "500006"
			result.ErrorMessage = "Internal server error"
			return result
		}

		// periksa jika ada peerubahan password atau name maka masukkan ke new_credentials

		// Create new_credentials object if name or password is being updated
		newCredentials := make(map[string]any)

		// Check for password update
		if newPassword, ok := changeFields["password"].(string); ok && newPassword != "" {
			newCredentials["password"] = newPassword
		}

		// Check for name update
		if newName, ok := changeFields["name"].(string); ok && newName != "" {
			newCredentials["name"] = newName
		}

		// Prepare action message
		action := map[string]any{
			"type":      "update",
			"device_id": deviceIdInt,
		}

		// Only include new_credentials if there are actual changes
		if len(newCredentials) > 0 {
			action["new_credentials"] = newCredentials
		}

		hub.SetDeviceAction(referenceId, deviceIdInt, action)

		logger.Debug(referenceId, "DEBUG - Update_Device_Data - Device action set to update for device ID:", deviceIdInt)
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

func updateDevicePassword(
	referenceId string,
	tx *sqlx.Tx,
	deviceId int64,
	newDevicePassword string,
	beforeData, afterData map[string]any,
	changeFields map[string]any,
) bool {
	// Ambil key dari environment
	key := os.Getenv("KEY")
	logger.Debug(referenceId, "DEBUG - Update_Device_Data - key:", key)

	if key == "" {
		logger.Error(referenceId, "ERROR - Update_Device_Data - KEY is not set")
		return false
	}

	afterData["password"] = newDevicePassword

	// Enkripsi password baru
	chiperPassword, iv, err := crypto.EncryptAES256(newDevicePassword, key)
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_Device_Data - Failed to encrypt password:", err)
		return false
	}

	// Update field perubahan
	changeFields["salt"] = iv
	changeFields["salted_password"] = chiperPassword
	delete(changeFields, "password") // Hapus password plaintext dari update langsung

	// Ambil data lama
	var oldSalt sql.NullString
	var oldSaltedPassword sql.NullString
	querySelect := `SELECT salt, salted_password FROM device.unit WHERE id = $1;`
	err = tx.QueryRow(querySelect, deviceId).Scan(&oldSalt, &oldSaltedPassword)
	if err != nil {
		logger.Error(referenceId, "ERROR - Update_Device_Data - Failed to fetch original device data:", err)
		return false
	}

	// Dekripsi password lama (jika valid)
	if oldSalt.Valid && oldSaltedPassword.Valid {
		oldPlainPassword, err := crypto.DecryptAES256(oldSaltedPassword.String, oldSalt.String, key)
		if err != nil {
			logger.Error(referenceId, "ERROR - Update_Device_Data - Failed to decrypt old password:", err)
			return false
		}
		beforeData["password"] = oldPlainPassword
	} else {
		logger.Warning(referenceId, "WARN - Update_Device_Data - Old password data is not valid")
		beforeData["password"] = nil
	}

	logger.Debug(referenceId, "DEBUG - Update_Device_Data - changeFields after password handled:", changeFields)

	return true
}

// updateDeviceDataField updates the data field of the device
func updateDeviceDataField(referenceId string, tx *sqlx.Tx, deviceId int64, dataField map[string]any,
	beforeData, afterData map[string]any) bool {

	var err error

	// Get existing data before changes
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

	// Prepare before/after snapshot
	originalBefore := make(map[string]any)
	updatedAfter := maps.Clone(currentData)

	// Step 1: Handle delete fields - changed to expect an array
	if deleteData, ok := dataField["delete"].([]any); ok && len(deleteData) > 0 {
		for _, key := range deleteData {
			if k, ok := key.(string); ok {
				// Save value before deletion
				if val, exists := currentData[k]; exists {
					originalBefore[k] = val
					delete(updatedAfter, k)
				}
			}
		}

		// Build delete expression
		expr := ""
		for _, key := range deleteData {
			if k, ok := key.(string); ok {
				expr += " - '" + k + "'"
			}
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

	// Step 4: Save final results to database
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

	// Step 5: Save to beforeData and afterData global
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
 id     | bigint                 |           | not null | nextval('sysfile.file_id_seq'::regclass)
 timestamp | bigint                 |           | not null | EXTRACT(epoch FROM now())::bigint
 data   | text                   |           | not null |
 name   | character varying(255) |           | not null |
Indexes:
    "image_pkey" PRIMARY KEY, btree (id)
Referenced by:
    TABLE "device.unit" CONSTRAINT "fk_image" FOREIGN KEY (image) REFERENCES sysfile.file(id) ON DELETE SET NULL
*/

// handleUpdateDeviceImage updates the image information for a device
func handleUpdateDeviceImage(referenceId string, tx *sqlx.Tx, rawImage map[string]any, deviceIdInt int64, deviceName string, beforeData, afterData map[string]any) (bool, error) {
	if insertData, ok := rawImage["insert"].(map[string]any); ok {
		logger.Debug(referenceId, "DEBUG - Update_Device_Data - image.insert:", insertData)

		// INSERT image (new image)
		rawImageData, hasData := insertData["file_data"]
		if !hasData {
			return false, fmt.Errorf("missing file_data for insert")
		}

		imageData, ok := rawImageData.(string)
		if !ok {
			return false, fmt.Errorf("invalid file_data for insert")
		}

		imageName := fmt.Sprintf("%s_%s", deviceName, time.Now().Format("20060102150405"))

		// Insert new image
		insertQuery := `
            INSERT INTO sysfile.file (data, name, timestamp)
            VALUES ($1, $2, EXTRACT(epoch FROM now())::bigint)
            RETURNING id;
        `

		var newFileId int64
		err := tx.QueryRow(insertQuery, imageData, imageName).Scan(&newFileId)
		if err != nil {
			return false, fmt.Errorf("failed to insert image: %v", err)
		}

		logger.Info(referenceId, "INFO - Inserted new image with ID:", newFileId)

		// Update device with new file ID
		updateDeviceQuery := `UPDATE device.unit SET image = $1 WHERE id = $2;`
		_, err = tx.Exec(updateDeviceQuery, newFileId, deviceIdInt)
		if err != nil {
			return false, fmt.Errorf("failed to update device with new image: %v", err)
		}
		// set beforedata
		beforeData["image"] = map[string]any{}

		// Set afterData for insert
		afterData["image"] = map[string]any{
			"name": imageName,
		}

	} else if updateData, ok := rawImage["update"].(map[string]any); ok {
		// UPDATE image
		logger.Debug(referenceId, "DEBUG - Update_Device_Data - image.update:", updateData)

		rawImageId, hasId := updateData["file_id"]
		rawImageData, hasData := updateData["file_data"]

		if !hasId || !hasData {
			return false, fmt.Errorf("missing file_id or data for update")
		}

		imageIdFloat, ok := rawImageId.(float64)
		if !ok {
			return false, fmt.Errorf("invalid file_id for update")
		}

		imageData, ok := rawImageData.(string)
		if !ok {
			return false, fmt.Errorf("invalid file_data for update")
		}

		imageId := sql.NullInt64{Int64: int64(imageIdFloat), Valid: true}
		imageName := fmt.Sprintf("%s_%s", deviceName, time.Now().Format("20060102150405"))

		// Get existing image data
		var existingimage struct {
			Name string `db:"name"`
			Data string `db:"data"`
		}
		selectQuery := `SELECT name, data FROM sysfile.file WHERE id = $1`
		err := tx.Get(&existingimage, selectQuery, imageId.Int64)
		if err != nil {
			return false, fmt.Errorf("failed to fetch existing image data: %v", err)
		}

		// Set beforeData
		beforeData["image"] = map[string]any{}
		if existingimage.Name != "" {
			beforeData["image"] = map[string]any{
				"name": existingimage.Name,
			}
		}

		// Update image
		updateQuery := `
            UPDATE sysfile.file 
            SET data = $1, name = $2, timestamp = EXTRACT(epoch FROM now())::bigint 
            WHERE id = $3;
        `
		_, err = tx.Exec(updateQuery, imageData, imageName, imageId.Int64)
		if err != nil {
			return false, fmt.Errorf("failed to update image: %v", err)
		}

		// Set afterData
		afterData["image"] = map[string]any{
			"name": imageName,
		}

	} else if deleteData, ok := rawImage["delete"].(map[string]any); ok {
		// DELETE image
		logger.Debug(referenceId, "DEBUG - Update_Device_Data - image.delete:", deleteData)

		rawImageId, hasId := deleteData["file_id"]
		if !hasId {
			return false, fmt.Errorf("missing file_id for delete")
		}

		imageIdFloat, ok := rawImageId.(float64)
		if !ok {
			return false, fmt.Errorf("invalid file_id for delete")
		}

		// Get existing image data for beforeData
		var existingimage struct {
			Name string `db:"name"`
			Data string `db:"data"`
		}
		selectQuery := `SELECT name, data FROM sysfile.file WHERE id = $1`
		err := tx.Get(&existingimage, selectQuery, int64(imageIdFloat))
		if err != nil {
			return false, fmt.Errorf("failed to fetch existing image data: %v", err)
		}

		// Set beforeData
		beforeData["image"] = map[string]any{}
		if existingimage.Name != "" {
			beforeData["image"] = map[string]any{
				"name": existingimage.Name,
			}
		}

		// Delete image
		deleteQuery := `DELETE FROM sysfile.file WHERE id = $1;`
		_, err = tx.Exec(deleteQuery, int64(imageIdFloat))
		if err != nil {
			return false, fmt.Errorf("failed to delete image: %v", err)
		}

		// Set afterData for delete
		afterData["image"] = map[string]any{}

		logger.Info(referenceId, "INFO - image deleted successfully")
	} else {
		logger.Info(referenceId, "INFO - No image changes detected")
		return true, nil
	}

	return true, nil
}
