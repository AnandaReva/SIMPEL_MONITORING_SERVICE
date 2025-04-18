package process

import (
	"fmt"
	"monitoring_service/logger"

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
*/

type DeviceDataInfo struct {
	DeviceId           int64 `db:"id" json:"device_id"`
	DeviceReadInterval int16 `db:"read_interval" json:"device_read_interval"`
}

func Device_Get_Data(referenceId string, conn *sqlx.DB, deviceId int64, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Device_Get_Data param: ", param)

	// Query untuk mendapatkan data perangkat
	queryTogetDeviceData := `SELECT read_interval FROM device.unit WHERE id = $1`

	var deviceData DeviceDataInfo
	err := conn.QueryRow(queryTogetDeviceData, deviceId).Scan(
		&deviceData.DeviceReadInterval,
	)
	if err != nil {
		logger.Error(referenceId, "ERROR - Device_Get_Data - Failed to get device data:", err)
		result.ErrorCode = "500000"
		result.ErrorMessage = "Internal Server Error"
		return result
	}
	logger.Debug(referenceId, "DEBUG - Device_Get_Data - Device data retrieved:", fmt.Sprintf("Device ID: %d , Device Read Interval: %d", deviceData.DeviceId, deviceData.DeviceReadInterval))

	result.Payload["status"] = "success"
	result.Payload["device_data"] = map[string]any{
		"device_id":            deviceData.DeviceId,
		"device_read_interval": deviceData.DeviceReadInterval,
	}
	return result

}
