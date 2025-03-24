package process

import (
	"fmt"
	"monitoring_service/logger"

	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

/* simpel=> \d  device.unit ;
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
Indexes:
    "activity_pkey" PRIMARY KEY, btree (id)
Foreign-key constraints:
    "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
    "fk_user" FOREIGN KEY (actor) REFERENCES sysuser."user"(id) ON DELETE SET NULL


*/

type DeviceData struct {
	DeviceId     int64  `db:"device_id" json:"device_id"`
	DeviceName   string `db:"device_name" json:"device_name"`
	CreateTstamp int64  `db:"create_tstamp" json:"create_tstamp"`
	LastTstamp   int64  `db:"last_tstamp" json:"last_tstamp"`
	Attachment   string `db:"attachment" json:"attachment"`
	ReadInterval int16  `db:"read_interval" json:"read_interval"`
}

type DeviceActivity struct {
	ActivityId          int64  `db:"activity_id" json:"activity_id"`
	ActivityActor       int64  `db:"activity_actor" json:"activity_actor"`
	ActorFullName       string `db:"actor_full_name" json:"actor_full_name"`
	ActivityDescription string `db:"activity_description" json:"activity_description"`
	ActivityTstamp      int64  `db:"activity_tstamp" json:"activity_tstamp"`
}

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

	// Query untuk mengambil data perangkat
	queryDevice := `
		SELECT 
			du.id AS device_id, 
			du.name AS device_name,
			du.create_tstamp, 
			du.last_tstamp, 
			du.attachment, 
			du.read_interval
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

	// Query untuk mengambil aktivitas perangkat dengan full_name dari aktor
	queryActivities := `
		SELECT 
			da.id AS activity_id, 
			da.actor AS activity_actor, 
			COALESCE(su.full_name, '') AS actor_full_name,
			da.activity AS activity_description,
			da.tstamp AS activity_tstamp
		FROM 
			device.device_activity da
		LEFT JOIN 
			sysuser."user" su 
			ON da.actor = su.id
		WHERE 
			da.unit_id = $1;
	`

	var deviceActivities []DeviceActivity
	if err := conn.Select(&deviceActivities, queryActivities, int64(deviceId)); err != nil {
		logger.Error(referenceId, "ERROR - Get_Device_Data - Failed to query device activities: ", err)
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal Server Error"
		return result
	}

	// Format respons
	result.Payload["status"] = "success"
	result.Payload["device_data"] = map[string]any{
		"device_id":         deviceData.DeviceId,
		"device_name":       deviceData.DeviceName,
		"create_tstamp":     deviceData.CreateTstamp,
		"last_tstamp":       deviceData.LastTstamp,
		"attachment":        deviceData.Attachment,
		"read_interval":     deviceData.ReadInterval,
		"device_activities": deviceActivities,
	}

	logger.Info(referenceId, fmt.Sprintf("INFO - Get_Device_Data - Found device_id %d with %d activities", deviceData.DeviceId, len(deviceActivities)))
	return result
}
