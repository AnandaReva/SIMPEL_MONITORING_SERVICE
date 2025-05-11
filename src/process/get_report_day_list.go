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
	TO_CHAR(timestamp, 'Day') AS month_name,
	COUNT(*) AS total_data,
	MIN(timestamp) AS first_record_timestamp,
	MAX(timestamp) AS last_record_timestamp

FROM device.data
WHERE EXTRACT(YEAR FROM timestamp) = 2025
GROUP BY month_number, month_name
ORDER BY month_number;
*/
package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

type MonthDetail struct {
	FirstRecordTimestamp string  `db:"first_record_timestamp" json:"first_record_timestamp"`
	LastRecordTimestamp  string  `db:"last_record_timestamp" json:"last_record_timestamp"`
	EnergyConsumption    float64 `db:"energy_consumed" json:"energy_consumed_count"`
	TotalData            int64   `db:"total_data" json:"total_data"`
	DataInterval         float64 `db:"data_interval" json:"data_interval"`
	TotalSizeBytes       float64 `db:"total_size_bytes" json:"total_size_bytes"`

	Voltage     MonthVoltageSummary     `json:"voltage"`
	Current     MonthCurrentSummary     `json:"current"`
	Power       MonthPowerSummary       `json:"power"`
	Frequency   MonthFrequencySummary   `json:"frequency"`
	PowerFactor MonthPowerFactorSummary `json:"power_factor"`
	DayList     []DayListDetail             `json:"day_list"`
}

type DayListDetail struct {
	DayDateNum           int64   `db:"day_date_num" json:"day_date_num"`
	DayNumber            int64   `db:"day_num" json:"day_num"`
	DayName              string  `db:"day_name" json:"day_name"`
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
}

type DayVoltageSummary struct {
	Avg float64 `db:"avg_voltage" json:"avg"`
	Min float64 `db:"min_voltage" json:"min"`
	Max float64 `db:"max_voltage" json:"max"`
}

type DayCurrentSummary struct {
	Avg float64 `db:"avg_current" json:"avg"`
	Min float64 `db:"min_current" json:"min"`
	Max float64 `db:"max_current" json:"max"`
}

type DayPowerSummary struct {
	Avg float64 `db:"avg_power" json:"avg"`
	Min float64 `db:"min_power" json:"min"`
	Max float64 `db:"max_power" json:"max"`
}

type DayFrequencySummary struct {
	Avg float64 `db:"avg_frequency" json:"avg"`
	Min float64 `db:"min_frequency" json:"min"`
	Max float64 `db:"max_frequency" json:"max"`
}

type DayPowerFactorSummary struct {
	Avg float64 `db:"avg_power_factor" json:"avg"`
	Min float64 `db:"min_power_factor" json:"min"`
	Max float64 `db:"max_power_factor" json:"max"`
}

type DayRaw struct {
	DayDateNum           int64   `db:"day_date_num"`
	DayNumber            int64   `db:"day_num"`
	DayName              string  `db:"day_name"`
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

type MonthDetailRaw struct {
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

func Get_Report_Day_List(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
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

	// Query day list
	queryDayList := `
SELECT
	EXTRACT(DAY FROM d.timestamp) AS day_date_num,
	EXTRACT(DOW FROM d.timestamp) + 1 AS day_num,
	TRIM(TO_CHAR(d.timestamp, 'Day')) AS day_name,
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
  AND EXTRACT(YEAR FROM d.timestamp) = $2
  AND EXTRACT(MONTH FROM d.timestamp) = $3
GROUP BY day_date_num, day_num, day_name
ORDER BY day_date_num ASC
`

	var rawDays []DayRaw
	err := conn.Select(&rawDays, queryDayList, int64(deviceId), int(yearParam), int(monthParam))
	if err != nil {
		logger.Error(referenceId, "queryDayList Failed:", err.Error())
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Convert day data to DayListDetail format
	var dayList []DayListDetail
	for _, row := range rawDays {
		dayList = append(dayList, DayListDetail{
			DayDateNum:           row.DayDateNum,
			DayNumber:            row.DayNumber,
			DayName:              row.DayName,
			FirstRecordTimestamp: row.FirstRecordTimestamp,
			LastRecordTimestamp:  row.LastRecordTimestamp,
			EnergyConsumption:    row.EnergyConsumption,
			TotalData:            row.TotalData,
			DataInterval:         row.DataInterval,
			TotalSizeBytes:       row.TotalSizeBytes,
			Voltage:              DayVoltageSummary{Avg: row.AvgVoltage, Min: row.MinVoltage, Max: row.MaxVoltage},
			Current:              DayCurrentSummary{Avg: row.AvgCurrent, Min: row.MinCurrent, Max: row.MaxCurrent},
			Power:                DayPowerSummary{Avg: row.AvgPower, Min: row.MinPower, Max: row.MaxPower},
			Frequency:            DayFrequencySummary{Avg: row.AvgFrequency, Min: row.MinFrequency, Max: row.MaxFrequency},
			PowerFactor:          DayPowerFactorSummary{Avg: row.AvgPowerFactor, Min: row.MinPowerFactor, Max: row.MaxPowerFactor},
		})
	}

	// Month summary query
	monthQuery := `
SELECT
	TO_CHAR(MIN(d.timestamp), 'YYYY-MM-DD HH24:MI:SS') AS first_record_timestamp,
	TO_CHAR(MAX(d.timestamp), 'YYYY-MM-DD HH24:MI:SS') AS last_record_timestamp,
	MAX(d.energy) - MIN(d.energy) AS energy_consumed,
	COUNT(*) AS total_data,
	(EXTRACT(EPOCH FROM MAX(d.timestamp) - MIN(d.timestamp)) / NULLIF(COUNT(*) - 1, 0)) AS data_interval,
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
  AND EXTRACT(YEAR FROM d.timestamp) = $2
  AND EXTRACT(MONTH FROM d.timestamp) = $3
`

	logger.Info(referenceId, "Running monthQuery:", monthQuery)
	logger.Info(referenceId, fmt.Sprintf("Params: device_id=%d, year=%d, month=%d", int64(deviceId), int(yearParam), int(monthParam)))

	var monthRaw MonthDetailRaw
	err = conn.Get(&monthRaw, monthQuery, int64(deviceId), int(yearParam), int(monthParam))
	if err != nil {
		logger.Error(referenceId, "Month Detail Query Failed:", err.Error())
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error"
		return result
	}

	logger.Debug(referenceId, fmt.Sprintf("MonthDetailRaw Dump: %+v", monthRaw))

	// Convert to MonthDetail format
	monthSummary := MonthDetail{
		FirstRecordTimestamp: monthRaw.FirstRecordTimestamp,
		LastRecordTimestamp:  monthRaw.LastRecordTimestamp,
		EnergyConsumption:    monthRaw.EnergyConsumption,
		TotalData:            monthRaw.TotalData,
		DataInterval:         monthRaw.DataInterval,
		TotalSizeBytes:       monthRaw.TotalSizeBytes,
		Voltage: MonthVoltageSummary{
			Avg: monthRaw.AvgVoltage,
			Min: monthRaw.MinVoltage,
			Max: monthRaw.MaxVoltage,
		},
		Current: MonthCurrentSummary{
			Avg: monthRaw.AvgCurrent,
			Min: monthRaw.MinCurrent,
			Max: monthRaw.MaxCurrent,
		},
		Power: MonthPowerSummary{
			Avg: monthRaw.AvgPower,
			Min: monthRaw.MinPower,
			Max: monthRaw.MaxPower,
		},
		Frequency: MonthFrequencySummary{
			Avg: monthRaw.AvgFrequency,
			Min: monthRaw.MinFrequency,
			Max: monthRaw.MaxFrequency,
		},
		PowerFactor: MonthPowerFactorSummary{
			Avg: monthRaw.AvgPowerFactor,
			Min: monthRaw.MinPowerFactor,
			Max: monthRaw.MaxPowerFactor,
		},
		DayList: dayList,
	}

	result.Payload = map[string]any{
		"year":                   int(yearParam),
		"month":                  int(monthParam),
		"first_record_timestamp": monthSummary.FirstRecordTimestamp,
		"last_record_timestamp":  monthSummary.LastRecordTimestamp,
		"energy_consumption":     monthSummary.EnergyConsumption,
		"total_data":             monthSummary.TotalData,
		"data_interval":          monthSummary.DataInterval,
		"total_size_bytes":       monthSummary.TotalSizeBytes,
		"voltage":                monthSummary.Voltage,
		"current":                monthSummary.Current,
		"power":                  monthSummary.Power,
		"frequency":              monthSummary.Frequency,
		"power_factor":           monthSummary.PowerFactor,
		"day_list":               monthSummary.DayList,
	}

	return result
}