/*
simpel=> \d device.data;

											 Table "device.data"
	Column    |            Type             | Collation | Nullable |                 Default

--------------+-----------------------------+-----------+----------+------------------------------------------

	id           | bigint                      |           | not null | nextval('device.data2_id_seq'::regclass)
	unit_id      | bigint                      |           | not null |
	timestamp    | timestamp without time zone |           | not null | now()
	voltage      | double precision            |           | not null |
	current      | double precision            |           | not null |
	power        | double precision            |           | not null |
	energy       | double precision            |           | not null |
	frequency    | double precision            |           | not null |
	power_factor | double precision            |           | not null |

Indexes:

	"data_tstamp_idx" btree ("timestamp" DESC)
	"data_unique_idx" UNIQUE, btree (id, "timestamp")

Foreign-key constraints:

	"fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE

Triggers:

	ts_insert_blocker BEFORE INSERT ON device.data FOR EACH ROW EXECUTE FUNCTION _timescaledb_functions.insert_blocker()

exp get Montoh data :  SELECT

	EXTRACT(MONTH FROM timestamp) AS month_number,
	TO_CHAR(timestamp, 'Month') AS month_name,
	COUNT(*) AS total_data,
	MIN(timestamp) AS first_record_timestamp,
	MAX(timestamp) AS last_record_timestamp

FROM device.data
WHERE EXTRACT(YEAR FROM timestamp) = 2025
GROUP BY month_number, month_name
ORDER BY month_number;



{
  "error_code": "000000",
  "error_message": "",
  "payload": {
	"year" : 2025,
  "month" : 5
  "date_day": "15",
  "day_num" "1" // senin
    "device_id": 123,
	"status": "success",
      "first_record_timestamp": "2025-05-15 00:00:12",
      "last_record_timestamp": "2025-05-15 23:59:58",
      "energy_consumption": 45.78,
      "total_data": 8640,
      "total_size_bytes": 3456000,
      "avg_voltage": 220.45,
      "min_voltage": 215.32,
      "max_voltage": 225.12,
      "avg_current": 12.34,
      "min_current": 10.21,
      "max_current": 15.67,
      "avg_power": 2720.56,
      "min_power": 2200.45,
      "max_power": 3200.78,
      "avg_frequency": 49.98,
      "min_frequency": 49.75,
      "max_frequency": 50.12,
      "avg_power_factor": 0.92,
      "min_power_factor": 0.85,
      "max_power_factor": 0.98,
      "hour_list": [
        {
          "hour": 24,
          "first_timestamp": "2025-05-15 00:00:12",
          "last_timestamp": "2025-05-15 00:59:58",
          "total_data": 360,
          "total_size_bytes": 144000,
          "energy_consumed": 1.23,
          "avg_voltage": 219.87,
          "min_voltage": 215.32,
          "max_voltage": 221.45,
          "avg_current": 11.23,
          "min_current": 10.21,
          "max_current": 12.45,
          "avg_power": 2460.78,
          "min_power": 2200.45,
          "max_power": 2730.12,
          "avg_frequency": 49.92,
          "min_frequency": 49.75,
          "max_frequency": 50.05,
          "avg_power_factor": 0.91,
          "min_power_factor": 0.85,
          "max_power_factor": 0.95
        },
        {
          "hour": 1,
          "first_timestamp": "2025-05-15 01:00:05",
          "last_timestamp": "2025-05-15 01:59:59",
          "total_data": 360,
          "total_size_bytes": 144000,
          "energy_consumed": 1.45,
          "avg_voltage": 220.12,
          "min_voltage": 216.45,
          "max_voltage": 222.78,
          "avg_current": 11.78,
          "min_current": 10.45,
          "max_current": 12.89,
          "avg_power": 2590.45,
          "min_power": 2300.12,
          "max_power": 2850.67,
          "avg_frequency": 49.95,
          "min_frequency": 49.82,
          "max_frequency": 50.08,
          "avg_power_factor": 0.92,
          "min_power_factor": 0.87,
          "max_power_factor": 0.96
        },
        // Data untuk jam 2-23 akan mengikuti pola yang sama
      ]
    }
} */

package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"
	"time"

	"github.com/jmoiron/sqlx"
)

type DayDetail struct {
	FirstRecordTimestamp string  `db:"first_record_timestamp" json:"first_record_timestamp"`
	LastRecordTimestamp  string  `db:"last_record_timestamp" json:"last_record_timestamp"`
	EnergyConsumption    float64 `db:"energy_consumed" json:"energy_consumed_count"`
	TotalData            int64   `db:"total_data" json:"total_data"`
	DataInterval         float64 `db:"data_interval" json:"data_interval"`
	TotalSizeBytes       float64 `db:"total_size_bytes" json:"total_size_bytes"`

	Voltage     DayVoltageSummary     `json:"voltage"`
	Current     DayCurrentSummary     `json:"current"`
	Power       DayPowerSummary       `json:"power"`
	Frequency   DayFrequencySummary   `json:"frequency"`
	PowerFactor DayPowerFactorSummary `json:"power_factor"`
	HourList    []HourDetail          `json:"hour_list"`
}

type HourDetail struct {
	Hour                 int     `db:"hour" json:"hour"`
	FirstRecordTimestamp string  `db:"first_record_timestamp" json:"first_record_timestamp"`
	LastRecordTimestamp  string  `db:"last_record_timestamp" json:"last_record_timestamp"`
	EnergyConsumption    float64 `db:"energy_consumed" json:"energy_consumed_count"`
	TotalData            int64   `db:"total_data" json:"total_data"`
	DataInterval         float64 `db:"data_interval" json:"data_interval"`
	TotalSizeBytes       float64 `db:"total_size_bytes" json:"total_size_bytes"`

	Voltage     HourVoltageSummary     `json:"voltage"`
	Current     HourCurrentSummary     `json:"current"`
	Power       HourPowerSummary       `json:"power"`
	Frequency   HourFrequencySummary   `json:"frequency"`
	PowerFactor HourPowerFactorSummary `json:"power_factor"`
}

type HourVoltageSummary struct {
	Avg float64 `db:"avg_voltage" json:"avg"`
	Min float64 `db:"min_voltage" json:"min"`
	Max float64 `db:"max_voltage" json:"max"`
}

type HourCurrentSummary struct {
	Avg float64 `db:"avg_current" json:"avg"`
	Min float64 `db:"min_current" json:"min"`
	Max float64 `db:"max_current" json:"max"`
}

type HourPowerSummary struct {
	Avg float64 `db:"avg_power" json:"avg"`
	Min float64 `db:"min_power" json:"min"`
	Max float64 `db:"max_power" json:"max"`
}

type HourFrequencySummary struct {
	Avg float64 `db:"avg_frequency" json:"avg"`
	Min float64 `db:"min_frequency" json:"min"`
	Max float64 `db:"max_frequency" json:"max"`
}

type HourPowerFactorSummary struct {
	Avg float64 `db:"avg_power_factor" json:"avg"`
	Min float64 `db:"min_power_factor" json:"min"`
	Max float64 `db:"max_power_factor" json:"max"`
}

type HourRaw struct {
	Hour                 int     `db:"hour"`
	FirstRecordTimestamp string  `db:"first_record_timestamp"`
	LastRecordTimestamp  string  `db:"last_record_timestamp"`
	EnergyConsumption    float64 `db:"energy_consumed"`
	TotalData            int64   `db:"total_data"`
	DataInterval         float64 `db:"data_interval"`
	TotalSizeBytes       float64 `db:"total_size_bytes"`
	AvgVoltage           float64 `db:"avg_voltage"`
	MinVoltage           float64 `db:"min_voltage"`
	MaxVoltage           float64 `db:"max_voltage"`
	AvgCurrent           float64 `db:"avg_current"`
	MinCurrent           float64 `db:"min_current"`
	MaxCurrent           float64 `db:"max_current"`
	AvgPower             float64 `db:"avg_power"`
	MinPower             float64 `db:"min_power"`
	MaxPower             float64 `db:"max_power"`
	AvgFrequency         float64 `db:"avg_frequency"`
	MinFrequency         float64 `db:"min_frequency"`
	MaxFrequency         float64 `db:"max_frequency"`
	AvgPowerFactor       float64 `db:"avg_power_factor"`
	MinPowerFactor       float64 `db:"min_power_factor"`
	MaxPowerFactor       float64 `db:"max_power_factor"`
}

type DayDetailRaw struct {
	FirstRecordTimestamp string  `db:"first_record_timestamp"`
	LastRecordTimestamp  string  `db:"last_record_timestamp"`
	EnergyConsumption    float64 `db:"energy_consumed"`
	TotalData            int64   `db:"total_data"`
	DataInterval         float64 `db:"data_interval"`
	TotalSizeBytes       float64 `db:"total_size_bytes"`
	AvgVoltage           float64 `db:"avg_voltage"`
	MinVoltage           float64 `db:"min_voltage"`
	MaxVoltage           float64 `db:"max_voltage"`
	AvgCurrent           float64 `db:"avg_current"`
	MinCurrent           float64 `db:"min_current"`
	MaxCurrent           float64 `db:"max_current"`
	AvgPower             float64 `db:"avg_power"`
	MinPower             float64 `db:"min_power"`
	MaxPower             float64 `db:"max_power"`
	AvgFrequency         float64 `db:"avg_frequency"`
	MinFrequency         float64 `db:"min_frequency"`
	MaxFrequency         float64 `db:"max_frequency"`
	AvgPowerFactor       float64 `db:"avg_power_factor"`
	MinPowerFactor       float64 `db:"min_power_factor"`
	MaxPowerFactor       float64 `db:"max_power_factor"`
}

func Get_Report_Day_Detail(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	// Validate device_id
	deviceId, ok := param["device_id"].(float64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, fmt.Sprintf("Invalid device_id: %v", param["device_id"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Validate year
	yearParam, ok := param["year"].(float64)
	if !ok || yearParam <= 0 {
		logger.Error(referenceId, fmt.Sprintf("Invalid year: %v", param["year"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Validate month
	monthParam, ok := param["month"].(float64)
	if !ok || monthParam < 1 || monthParam > 12 {
		logger.Error(referenceId, fmt.Sprintf("Invalid month: %v", param["month"]))
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Validate day
	dateDayParam, ok := param["date_day"].(float64)
	if !ok || dateDayParam < 1 || dateDayParam > 31 {
		logger.Error(referenceId, fmt.Sprintf("Invalid date day: %v", param["date_day"]))
		result.ErrorCode = "400004"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Query hourly data dengan penambahan filter bulan dan tahun
	queryHourList := `
SELECT
	EXTRACT(HOUR FROM d.timestamp) AS hour,
	TO_CHAR(MIN(d.timestamp), 'YYYY-MM-DD HH24:MI:SS') AS first_record_timestamp,
	TO_CHAR(MAX(d.timestamp), 'YYYY-MM-DD HH24:MI:SS') AS last_record_timestamp,
	MAX(d.energy) - MIN(d.energy) AS energy_consumed,
	(EXTRACT(EPOCH FROM MAX(d.timestamp) - MIN(d.timestamp)) / NULLIF(COUNT(*) - 1, 0)) AS data_interval,
	SUM(pg_column_size(d.*))::float AS total_size_bytes,
	COUNT(*) AS total_data,
	AVG(d.voltage) AS avg_voltage,
	MIN(d.voltage) AS min_voltage,
	MAX(d.voltage) AS max_voltage,
	AVG(d.current) AS avg_current,
	MIN(d.current) AS min_current,
	MAX(d.current) AS max_current,
	AVG(d.power) AS avg_power,
	MIN(d.power) AS min_power,
	MAX(d.power) AS max_power,
	AVG(d.frequency) AS avg_frequency,
	MIN(d.frequency) AS min_frequency,
	MAX(d.frequency) AS max_frequency,
	AVG(d.power_factor) AS avg_power_factor,
	MIN(d.power_factor) AS min_power_factor,
	MAX(d.power_factor) AS max_power_factor
FROM device.data d
WHERE d.unit_id = $1
  AND EXTRACT(DAY FROM d.timestamp) = $2
  AND EXTRACT(MONTH FROM d.timestamp) = $3
  AND EXTRACT(YEAR FROM d.timestamp) = $4
GROUP BY hour
ORDER BY hour ASC
`

	var rawHours []HourRaw
	err := conn.Select(&rawHours, queryHourList, int64(deviceId), dateDayParam, monthParam, yearParam)
	if err != nil {
		logger.Error(referenceId, "queryHourList Failed:", err.Error())
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Convert hour data to HourDetail format
	var hourList []HourDetail
	for _, row := range rawHours {
		hourList = append(hourList, HourDetail{
			Hour:                 row.Hour,
			FirstRecordTimestamp: row.FirstRecordTimestamp,
			LastRecordTimestamp:  row.LastRecordTimestamp,
			EnergyConsumption:    row.EnergyConsumption,
			TotalData:            row.TotalData,
			DataInterval:         row.DataInterval,
			TotalSizeBytes:       row.TotalSizeBytes,
			Voltage: HourVoltageSummary{
				Avg: row.AvgVoltage,
				Min: row.MinVoltage,
				Max: row.MaxVoltage,
			},
			Current: HourCurrentSummary{
				Avg: row.AvgCurrent,
				Min: row.MinCurrent,
				Max: row.MaxCurrent,
			},
			Power: HourPowerSummary{
				Avg: row.AvgPower,
				Min: row.MinPower,
				Max: row.MaxPower,
			},
			Frequency: HourFrequencySummary{
				Avg: row.AvgFrequency,
				Min: row.MinFrequency,
				Max: row.MaxFrequency,
			},
			PowerFactor: HourPowerFactorSummary{
				Avg: row.AvgPowerFactor,
				Min: row.MinPowerFactor,
				Max: row.MaxPowerFactor,
			},
		})
	}

	// Day summary query
	dayQuery := `
SELECT
	TO_CHAR(MIN(d.timestamp), 'YYYY-MM-DD HH24:MI:SS') AS first_record_timestamp,
	TO_CHAR(MAX(d.timestamp), 'YYYY-MM-DD HH24:MI:SS') AS last_record_timestamp,
	SUM(d.energy) AS energy_consumed,
	COUNT(*) AS total_data,
	EXTRACT(EPOCH FROM MAX(d.timestamp) - MIN(d.timestamp)) / NULLIF(COUNT(*) - 1, 0) AS data_interval,
	SUM(pg_column_size(d.*))::float AS total_size_bytes,
	AVG(d.voltage) AS avg_voltage,
	MIN(d.voltage) AS min_voltage,
	MAX(d.voltage) AS max_voltage,
	AVG(d.current) AS avg_current,
	MIN(d.current) AS min_current,
	MAX(d.current) AS max_current,
	AVG(d.power) AS avg_power,
	MIN(d.power) AS min_power,
	MAX(d.power) AS max_power,
	AVG(d.frequency) AS avg_frequency,
	MIN(d.frequency) AS min_frequency,
	MAX(d.frequency) AS max_frequency,
	AVG(d.power_factor) AS avg_power_factor,
	MIN(d.power_factor) AS min_power_factor,
	MAX(d.power_factor) AS max_power_factor
FROM device.data d
WHERE d.unit_id = $1
  AND EXTRACT(DAY FROM d.timestamp) = $2
  AND EXTRACT(MONTH FROM d.timestamp) = $3
  AND EXTRACT(YEAR FROM d.timestamp) = $4
`

	var dayRaw DayDetailRaw
	err = conn.Get(&dayRaw, dayQuery, int64(deviceId), dateDayParam, monthParam, yearParam)
	if err != nil {
		logger.Error(referenceId, "dayQuery Failed:", err.Error())
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error"
		return result
	}

	logger.Debug(referenceId, fmt.Sprintf("DayDetailRaw Dump: %+v", dayRaw))

	// Gabungkan dateDayParam dengan monthParam dan yearParam untuk membentuk tanggal lengkap
	fullDate := fmt.Sprintf("%04d-%02d-%02d", int(yearParam), int(monthParam), int(dateDayParam))
	// Get day number (Monday=1, Sunday=7)
	dayTime, err := time.Parse("2006-01-02", fullDate)
	if err != nil {
		logger.Error(referenceId, "Date parse failed: "+err.Error())
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Convert to DayDetail format
	daySummary := DayDetail{
		FirstRecordTimestamp: dayRaw.FirstRecordTimestamp,
		LastRecordTimestamp:  dayRaw.LastRecordTimestamp,
		EnergyConsumption:    dayRaw.EnergyConsumption,
		TotalData:            dayRaw.TotalData,
		DataInterval:         dayRaw.DataInterval,
		TotalSizeBytes:       dayRaw.TotalSizeBytes,
		Voltage: DayVoltageSummary{
			Avg: dayRaw.AvgVoltage,
			Min: dayRaw.MinVoltage,
			Max: dayRaw.MaxVoltage,
		},
		Current: DayCurrentSummary{
			Avg: dayRaw.AvgCurrent,
			Min: dayRaw.MinCurrent,
			Max: dayRaw.MaxCurrent,
		},
		Power: DayPowerSummary{
			Avg: dayRaw.AvgPower,
			Min: dayRaw.MinPower,
			Max: dayRaw.MaxPower,
		},
		Frequency: DayFrequencySummary{
			Avg: dayRaw.AvgFrequency,
			Min: dayRaw.MinFrequency,
			Max: dayRaw.MaxFrequency,
		},
		PowerFactor: DayPowerFactorSummary{
			Avg: dayRaw.AvgPowerFactor,
			Min: dayRaw.MinPowerFactor,
			Max: dayRaw.MaxPowerFactor,
		},
		HourList: hourList,
	}

	result.Payload = map[string]any{
		"year":                   int(yearParam),
		"month":                  int(monthParam),
		"date_day":               int(dateDayParam),
		"day_num":                int(dayTime.Weekday()+6)%7 + 1, // Convert to Monday=1, Sunday=7
		"day_date_num":           dayTime.Day(),
		"first_record_timestamp": daySummary.FirstRecordTimestamp,
		"last_record_timestamp":  daySummary.LastRecordTimestamp,
		"energy_consumption":     daySummary.EnergyConsumption,
		"total_data":             daySummary.TotalData,
		"data_interval":          daySummary.DataInterval,
		"total_size_bytes":       daySummary.TotalSizeBytes,
		"voltage":                daySummary.Voltage,
		"current":                daySummary.Current,
		"power":                  daySummary.Power,
		"frequency":              daySummary.Frequency,
		"power_factor":           daySummary.PowerFactor,
		"hour_list":              daySummary.HourList,
	}

	return result
}
