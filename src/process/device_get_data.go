package process

import (
	"fmt"
	"monitoring_service/logger"
	"time"

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

/* Proses Menjaga Integritas Data Energy Reset
Pengecekan Bulan dan Tahun

Setiap kali data baru akan diproses, sistem membandingkan bulan dan tahun dari timestamp data sensor terakhir dengan waktu saat ini.

Jika bulan atau tahun berubah, maka sistem mereset nilai energy menjadi 0 untuk menghindari akumulasi energi lintas bulan yang tidak valid.

Pengaturan Flag ResetEnergy

Flag ResetEnergy digunakan untuk menandai apakah nilai energy saat ini adalah hasil reset atau melanjutkan dari bulan sebelumnya.

Ini penting agar proses perhitungan lanjutan (misal akumulasi energi bulanan) bisa mempertimbangkan konteks reset dengan benar.

Penanganan Kondisi Tanpa Data Sebelumnya

Jika tidak ditemukan data sensor sebelumnya untuk suatu perangkat (contoh: perangkat baru, atau data dihapus), maka sistem menganggap data pertama adalah awal bulan dan otomatis melakukan reset energy ke 0.

Penggunaan Timestamp dan Energy Terakhir

Data timestamp dan energy terakhir selalu digunakan sebagai referensi konsistensi supaya tidak terjadi mismatch antara bulan data sensor dengan bulan operasional sistem.

Audit Trail dan Logging

Semua proses keputusan (reset atau tidak) dicatat melalui logger.

Ini penting untuk audit trail jika ada keperluan menelusuri mengapa nilai energy direset atau tidak pada bulan tertentu.

Menghindari Double-Reset

Dengan mengecek hanya perubahan bulan, sistem menghindari kasus reset berkali-kali dalam satu bulan, walaupun perangkat sering restart atau kirim data dengan jeda panjang. */

type DeviceDataInfo struct {
	DeviceReadInterval   int16   `db:"read_interval" json:"device_read_interval"`
	DeviceLastEnergyData float64 `json:"device_last_energy_data"`
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
	logger.Debug(referenceId, "DEBUG - Device_Get_Data - Device data retrieved:", fmt.Sprintf("Device ID: %d , Device Read Interval: %d", deviceId, deviceData.DeviceReadInterval))

	// Cek data sensor terakhir
	var lastTimestamp time.Time
	var lastEnergy float64

	queryLastSensorData := `
		SELECT timestamp, energy 
		FROM device.data 
		WHERE unit_id = $1
		ORDER BY timestamp DESC 
		LIMIT 1
	`
	err = conn.QueryRow(queryLastSensorData, deviceId).Scan(
		&lastTimestamp,
		&lastEnergy,
	)

	if err != nil {
		// JIka tidak ada data sensor sebelumnya, anggap reset
		deviceData.DeviceLastEnergyData = 0
		logger.Warning(referenceId, "WARN - Device_Get_Data - No previous sensor data found, energy reset to 0")
	} else {
		// Bandingkan bulan & tahun
		now := time.Now()

		if lastTimestamp.Month() != now.Month() || lastTimestamp.Year() != now.Year() {
			// Bulan atau Tahun berbeda, reset energy
			deviceData.DeviceLastEnergyData = 0
			logger.Info(referenceId, "INFO - Device_Get_Data - Month changed, reset energy to 0")
		} else {
			// Bulan sama, lanjutkan energy terakhir
			deviceData.DeviceLastEnergyData = lastEnergy
			logger.Info(referenceId, "INFO - Device_Get_Data - Same month, continue energy")
		}
	}

	// Menambahkan data ke payload
	result.Payload["device_data"] = map[string]any{
		"device_id":               deviceId,
		"device_read_interval":    deviceData.DeviceReadInterval,
		"device_last_energy_data": deviceData.DeviceLastEnergyData, // Energy data yang benar
	}

	result.Payload["status"] = "success"

	return result
}
