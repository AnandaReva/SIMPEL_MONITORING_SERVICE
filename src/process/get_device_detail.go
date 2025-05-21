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
 create_timestamp   | bigint                 |           |          | EXTRACT(epoch FROM now())::bigint
 last_timestamp     | bigint                 |           |          | EXTRACT(epoch FROM now())::bigint
 image           | bigint                 |           |          |
 read_interval   | integer                |           | not null |
 salted_password | character varying(128) |           | not null |
 salt            | character varying(32)  |           | not null |
Indexes:
    "unit_pkey" PRIMARY KEY, btree (id)
    "idx_device_name" btree (name)
    "uq_device_unit_name" UNIQUE CONSTRAINT, btree (name)
Foreign-key constraints:
    "fk_attachment" FOREIGN KEY (image) REFERENCES sysfile.file(id) ON DELETE SET NULL
Referenced by:
    TABLE "_timescaledb_internal._hyper_5_471_chunk" CONSTRAINT "471_471_fk_unit" FOREIGN KEY (unit_id) REFERENCES de
vice.unit(id) ON DELETE CASCADE

:

\d: extra argument ";" ignored

simpel=> \d device.device_activity
                             Table "device.device_activity"
  Column  |  Type  | Collation | Nullable |                   Default
----------+--------+-----------+----------+---------------------------------------------
 id       | bigint |           | not null | nextval('device.activity_id_seq'::regclass)
 unit_id  | bigint |           | not null |
 actor    | bigint |           |          |
 activity | text   |           | not null |
 timestamp   | bigint |           | not null | EXTRACT(epoch FROM now())::bigint
 before   | jsonb  |           |          |
 after    | jsonb  |           |          |
Indexes:
    "activity_pkey" PRIMARY KEY, btree (id)
Foreign-key constraints:
    "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
    "fk_user" FOREIGN KEY (actor) REFERENCES sysuser."user"(id) ON DELETE SET NULL


*/

type DeviceData struct {
	DeviceId        int64           `db:"id" json:"device_id"`
	DeviceName      string          `db:"name" json:"device_name"`
	DeviceStatus    int64           `db:"st" json:"device_status"`
	CreateTimeStamp int64           `db:"create_timestamp" json:"device_create_timestamp"`
	LastTimeStamp   int64           `db:"last_timestamp" json:"device_last_timestamp"`
	ReadInterval    int16           `db:"read_interval" json:"device_read_interval"`
	DeviceData      json.RawMessage `db:"data" json:"device_data"`
	Image           *int64          `db:"image"`
}

type DeviceImage struct {
	FileId   int64  `db:"file_id" json:"file_id"`
	FileName string `db:"file_name" json:"file_name"`
	FileData string `db:"file_data" json:"file_data"`
}

func Get_Device_Detail(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Get_Device_Detail param: ", param)

	// Validasi parameter device_id
	deviceId, ok := param["device_id"].(float64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Device_Detail - Invalid device_id: %v", param["device_id"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// handle password
	var salt string
	var saltedPassword string

	query := `SELECT salt, salted_password FROM device.unit WHERE id = $1`
	if err := conn.QueryRow(query, int64(deviceId)).Scan(&salt, &saltedPassword); err != nil {
		logger.Error(referenceId, "ERROR - Get_Device_Detail - Failed to get device salt and salted_password: ", err)
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
		du.st,
		du.create_timestamp, 
		du.last_timestamp, 
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
		logger.Error(referenceId, "ERROR - Get_Device_Detail - Failed to query device data: ", err)
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
			logger.Error(referenceId, "ERROR - Get_Device_Detail - Failed to query device image: ", err)
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

	// Format respons
	result.Payload["status"] = "success"
	result.Payload["device_id"] = deviceData.DeviceId
	result.Payload["device_name"] = deviceData.DeviceName
	result.Payload["device_password"] = plainTextPassword
	result.Payload["device_status"] = deviceData.DeviceStatus
	result.Payload["device_create_timestamp"] = deviceData.CreateTimeStamp
	result.Payload["device_last_timestamp"] = deviceData.LastTimeStamp
	result.Payload["device_read_interval"] = deviceData.ReadInterval
	result.Payload["device_data"] = deviceData.DeviceData // misalnya berisi Lokasi, Sensor, dll
	result.Payload["device_image"] = deviceImage

	return result
}
