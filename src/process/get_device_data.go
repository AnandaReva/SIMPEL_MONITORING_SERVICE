package process

import (
	"encoding/json"
	"fmt"
	"monitoring_service/crypto"
	"monitoring_service/logger"
	"os"

	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

/* \d device.unit;
                                         Table "device.unit"
     Column      |          Type          | Collation | Nullable |              Default
-----------------+------------------------+-----------+----------+-----------------------------------
 id              | bigint                 |           | not null | nextval('device_id_sq'::regclass)
 name            | character varying(255) |           | not null |
 st              | integer                |           | not null |
 data            | jsonb                  |           | not null |
 create_tstamp   | bigint                 |           |          | EXTRACT(epoch FROM now())::bigint
 last_tstamp     | bigint                 |           |          | EXTRACT(epoch FROM now())::bigint
 image           | bigint                 |           |          |
 read_interval   | integer                |           | not null |
 salted_password | character varying(128) |           | not null |
 salt            | character varying(32)  |           | not null |
Indexes:
    "unit_pkey" PRIMARY KEY, btree (id)
    "idx_device_name" btree (name)
Foreign-key constraints:
    "fk_attachment" FOREIGN KEY (image) REFERENCES sysfile.file(id) ON DELETE SET NULL
Referenced by:
    TABLE "device.data" CONSTRAINT "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
    TABLE "device.device_activity" CONSTRAINT "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE


\d: extra argument ";" ignored

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


*/

type DeviceData struct {
	DeviceId     int64           `db:"id" json:"device_id"`
	DeviceName   string          `db:"name" json:"device_name"`
	CreateTstamp int64           `db:"create_tstamp" json:"device_create_tstamp"`
	LastTstamp   int64           `db:"last_tstamp" json:"device_last_tstamp"`
	ReadInterval int16           `db:"read_interval" json:"device_read_interval"`
	Data         json.RawMessage `db:"data" json:"device_data"`
	Image        *int64          `db:"image"`
}

type DeviceImage struct {
	FileId   int64  `db:"file_id" json:"file_id"`
	FileName string `db:"file_name" json:"file_name"`
	FileData string `db:"file_data" json:"file_data"`
}

// type DeviceActivity struct {
// 	ActivityId          int64  `db:"activity_id" json:"activity_id"`
// 	ActivityActor       int64  `db:"activity_actor_id" json:"activity_actor_id"`
// 	ActorFullName       string `db:"actor_full_name" json:"actor_full_name"`
// 	ActivityDescription string `db:"activity_description" json:"activity_description"`
// 	ActivityTstamp      int64  `db:"activity_tstamp" json:"activity_tstamp"`
// 	ActivityBefore      string `db:"activity_before" json:"activity_before"`
// 	ActivityAfter       string `db:"activity_after" json:"activity_after"`
// }

func Get_Device_Data(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Get_Device_Data param: ", param)

	// Validasi parameter device_id
	deviceId, ok := param["device_id"].(float64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Device_Data - Invalid device_id: %v", param["device_id"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// handle password
	var salt string
	var saltedPassword string

	query := `SELECT salt, salted_password FROM device.unit WHERE id = $1`
	if err := conn.QueryRow(query, int64(deviceId)).Scan(&salt, &saltedPassword); err != nil {
		logger.Error(referenceId, "ERROR - Get_Device_Data - Failed to get device salt and salted_password: ", err)
		result.ErrorCode = "500000"
		result.ErrorMessage = "Internal Server Error"
		return result
	}

	// get plain text
	key := os.Getenv("KEY")
	logger.Debug(referenceId, "DEBUG - Register_Device - key:", key)
	if key == "" {
		logger.Error(referenceId, "ERROR - Register_Device - KEY is not set")
		result.ErrorCode = "500000"
		result.ErrorMessage = "Internal server error"
		return result
	}

	plainTextPassword, err := crypto.DecryptAES256(saltedPassword, salt, key)
	if err != nil {
		logger.Error(referenceId, "ERROR - Register_Device - Failed to decrypt salted password: ", err)
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	logger.Debug(referenceId, "DEBUG - Register_Device - plainTextPassword:", plainTextPassword)

	// Query untuk mengambil data perangkat
	queryDevice := `
	SELECT 
		du.id,
		du.name,
		du.create_tstamp, 
		du.last_tstamp, 
		du.read_interval,
		COALESCE(du.data, '{}'::jsonb) AS data,
		COALESCE(du.image, 0) AS image
	FROM 
		device.unit du
	WHERE 
		du.id = $1;
`

	var deviceData DeviceData
	if err := conn.Get(&deviceData, queryDevice, int64(deviceId)); err != nil {
		logger.Error(referenceId, "ERROR - Get_Device_Data - Failed to query device data: ", err)
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal Server Error"
		return result
	}

	var deviceImage DeviceImage

	if deviceData.Image != nil && *deviceData.Image != 0 {
		queryImage := `
			SELECT
				sf.id AS file_id,
				sf.name AS file_name,
				sf.data AS file_data
			FROM
				sysfile.file sf
			WHERE
				sf.id = $1;
		`
		if err := conn.Get(&deviceImage, queryImage, deviceData.Image); err != nil {
			logger.Error(referenceId, "ERROR - Get_Device_Data - Failed to query device image: ", err)
			result.ErrorCode = "500004"
			result.ErrorMessage = "Internal Server Error"
			return result
		}

		if deviceImage.FileId == 0 {
			deviceImage.FileId = 0
			deviceImage.FileName = ""
			deviceImage.FileData = ""
		}
	}

	// // Query untuk mengambil aktivitas perangkat dengan full_name dari aktor
	// queryActivities := `
	// 	SELECT
	// 		da.id AS activity_id,
	// 		COALESCE(da.actor, 0) AS activity_actor_id,
	// 		COALESCE(su.full_name, '') AS actor_full_name,
	// 		da.activity AS activity_description,
	// 		da.tstamp AS activity_tstamp,
	// 		COALESCE(da.before, '{}'::jsonb) AS activity_before,
	// 		COALESCE(da.after, '{}'::jsonb) AS activity_after
	// 	FROM
	// 		device.device_activity da
	// 	LEFT JOIN
	// 		sysuser."user" su
	// 		ON da.actor = su.id
	// 	WHERE
	// 		da.unit_id = $1;
	// `

	// var deviceActivities []DeviceActivity
	// if err := conn.Select(&deviceActivities, queryActivities, int64(deviceId)); err != nil {
	// 	logger.Error(referenceId, "ERROR - Get_Device_Data - Failed to query device activities: ", err)
	// 	result.ErrorCode = "500003"
	// 	result.ErrorMessage = "Internal Server Error"
	// 	return result
	// }

	// Format respons
	result.Payload["status"] = "success"
	result.Payload["device_data"] = map[string]any{
		"device_id":            deviceData.DeviceId,
		"device_name":          deviceData.DeviceName,
		"device_password":      plainTextPassword,
		"device_create_tstamp": deviceData.CreateTstamp,
		"device_last_tstamp":   deviceData.LastTstamp,
		"device_read_interval": deviceData.ReadInterval,
		"device_data":          deviceData.Data,
		"device_image":         deviceImage,
		//"device_activities":    deviceActivities,
	}

	return result
}
