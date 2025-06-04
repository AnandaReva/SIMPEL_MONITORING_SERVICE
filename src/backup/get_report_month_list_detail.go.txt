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
*/

package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

type YearDetail struct {
	FirstRecordTimestamp string  `db:"first_record_timestamp" json:"first_record_timestamp"`
	LastRecordTimestamp  string  `db:"last_record_timestamp" json:"last_record_timestamp"`
	EnergyConsumption    float64 `db:"energy_consumed" json:"energy_consumed_count"`
	TotalData            int64   `db:"total_data" json:"total_data"`
	DataInterval         float64 `db:"data_interval" json:"data_interval"`
	TotalSizeBytes       float64 `db:"total_size_bytes" json:"total_size_bytes"`

	Voltage     YearVoltageSummary     `json:"voltage"`
	Current     YearCurrentSummary     `json:"current"`
	Power       YearPowerSummary       `json:"power"`
	Frequency   YearFrequencySummary   `json:"frequency"`
	PowerFactor YearPowerFactorSummary `json:"power_factor"`
	MonthList   []MonthList            `json:"month_list"`
}

type YearDetailRaw struct {
	FirstRecordTimestamp string  `db:"first_record_timestamp"`
	LastRecordTimestamp  string  `db:"last_record_timestamp"`
	EnergyConsumption    float64 `db:"energy_consumed"`
	TotalData            int64   `db:"total_data"`
	DataInterval         float64 `db:"data_interval"`
	TotalSizeBytes       float64 `db:"total_size_bytes"`

	AvgVoltage     float64 `db:"avg_voltage"`
	MinVoltage     float64 `db:"min_voltage"`
	MaxVoltage     float64 `db:"max_voltage"`
	AvgCurrent     float64 `db:"avg_current"`
	MinCurrent     float64 `db:"min_current"`
	MaxCurrent     float64 `db:"max_current"`
	AvgPower       float64 `db:"avg_power"`
	MinPower       float64 `db:"min_power"`
	MaxPower       float64 `db:"max_power"`
	AvgFrequency   float64 `db:"avg_frequency"`
	MinFrequency   float64 `db:"min_frequency"`
	MaxFrequency   float64 `db:"max_frequency"`
	AvgPowerFactor float64 `db:"avg_power_factor"`
	MinPowerFactor float64 `db:"min_power_factor"`
	MaxPowerFactor float64 `db:"max_power_factor"`
}

type YearVoltageSummary struct {
	Avg float64 `db:"avg_voltage" json:"avg"`
	Min float64 `db:"min_voltage" json:"min"`
	Max float64 `db:"max_voltage" json:"max"`
}

type YearCurrentSummary struct {
	Avg float64 `db:"avg_current" json:"avg"`
	Min float64 `db:"min_current" json:"min"`
	Max float64 `db:"max_current" json:"max"`
}

type YearPowerSummary struct {
	Avg float64 `db:"avg_power" json:"avg"`
	Min float64 `db:"min_power" json:"min"`
	Max float64 `db:"max_power" json:"max"`
}

type YearFrequencySummary struct {
	Avg float64 `db:"avg_frequency" json:"avg"`
	Min float64 `db:"min_frequency" json:"min"`
	Max float64 `db:"max_frequency" json:"max"`
}

type YearPowerFactorSummary struct {
	Avg float64 `db:"avg_power_factor" json:"avg"`
	Min float64 `db:"min_power_factor" json:"min"`
	Max float64 `db:"max_power_factor" json:"max"`
}

type MonthList struct {
	MonthNumber          int64                   `db:"month_number" json:"month_number"`
	MonthName            string                  `db:"month_name" json:"month_name"`
	FirstRecordTimestamp string                  `db:"first_record_timestamp" json:"first_record_timestamp"`
	LastRecordTimestamp  string                  `db:"last_record_timestamp" json:"last_record_timestamp"`
	EnergyConsumedCount  float64                 `db:"max_energy" json:"energy_consumed_count"`
	TotalData            int64                   `db:"total_data" json:"total_data"`
	DataInterval         float64                 `db:"data_interval" json:"data_interval"`
	TotalSize            float64                 `db:"total_size" json:"total_size_bytes"`
	Voltage              MonthVoltageSummary     `json:"voltage"`
	Current              MonthCurrentSummary     `json:"current"`
	Power                MonthPowerSummary       `json:"power"`
	Frequency            MonthFrequencySummary   `json:"frequency"`
	PowerFactor          MonthPowerFactorSummary `json:"power_factor"`
}

type MonthVoltageSummary struct {
	Avg float64 `db:"avg_voltage" json:"avg"`
	Min float64 `db:"min_voltage" json:"min"`
	Max float64 `db:"max_voltage" json:"max"`
}

type MonthCurrentSummary struct {
	Avg float64 `db:"avg_current" json:"avg"`
	Min float64 `db:"min_current" json:"min"`
	Max float64 `db:"max_current" json:"max"`
}

type MonthPowerSummary struct {
	Avg float64 `db:"avg_power" json:"avg"`
	Min float64 `db:"min_power" json:"min"`
	Max float64 `db:"max_power" json:"max"`
}

type MonthFrequencySummary struct {
	Avg float64 `db:"avg_frequency" json:"avg"`
	Min float64 `db:"min_frequency" json:"min"`
	Max float64 `db:"max_frequency" json:"max"`
}

type MonthPowerFactorSummary struct {
	Avg float64 `db:"avg_power_factor" json:"avg"`
	Min float64 `db:"min_power_factor" json:"min"`
	Max float64 `db:"max_power_factor" json:"max"`
}

type MonthListRaw struct {
	MonthNumber          int64   `db:"month_number"`
	MonthName            string  `db:"month_name"`
	FirstRecordTimestamp string  `db:"first_record_timestamp"`
	LastRecordTimestamp  string  `db:"last_record_timestamp"`
	EnergyConsumedCount  float64 `db:"max_energy"`
	TotalData            int64   `db:"total_data"`
	DataInterval         float64 `db:"data_interval"`
	TotalSize            float64 `db:"total_size"`
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

func Get_Report_Month_List(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	// Validate inputs
	deviceId, ok := param["device_id"].(float64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, fmt.Sprintf("Invalid device_id: %v", param["device_id"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	yearSelected, ok := param["year"].(float64)
	if !ok || yearSelected <= 0 {
		logger.Error(referenceId, fmt.Sprintf("Invalid year: %v", param["year"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Query monthly data
	queryMonthList := `SELECT
        EXTRACT(MONTH FROM d.timestamp) AS month_number,
        TO_CHAR(d.timestamp, 'Month') AS month_name,
        TO_CHAR(MIN(d.timestamp), 'YYYY-MM-DD HH24:MI:SS') AS first_record_timestamp,
        TO_CHAR(MAX(d.timestamp), 'YYYY-MM-DD HH24:MI:SS') AS last_record_timestamp,
        MAX(d.energy) - MIN(d.energy) AS max_energy,
        (EXTRACT(EPOCH FROM MAX(d.timestamp) - MIN(d.timestamp)) / NULLIF(COUNT(*) - 1, 0)) AS data_interval,
        SUM(pg_column_size(d.*))::float AS total_size,
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
    WHERE d.unit_id = $1 AND EXTRACT(YEAR FROM d.timestamp) = $2
    GROUP BY month_number, month_name
    ORDER BY month_number;`

	var rawData []MonthListRaw
	err := conn.Select(&rawData, queryMonthList, int64(deviceId), int(yearSelected))
	if err != nil {
		logger.Error(referenceId, "queryMonthList Failed:", err.Error())
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Convert monthly data to MonthList format
	var monthList []MonthList
	for _, row := range rawData {
		monthList = append(monthList, MonthList{
			MonthNumber:          row.MonthNumber,
			MonthName:            row.MonthName,
			FirstRecordTimestamp: row.FirstRecordTimestamp,
			LastRecordTimestamp:  row.LastRecordTimestamp,
			EnergyConsumedCount:  row.EnergyConsumedCount,
			TotalData:            row.TotalData,
			DataInterval:         row.DataInterval,
			TotalSize:            row.TotalSize,
			Voltage:              MonthVoltageSummary{Avg: row.AvgVoltage, Min: row.MinVoltage, Max: row.MaxVoltage},
			Current:              MonthCurrentSummary{Avg: row.AvgCurrent, Min: row.MinCurrent, Max: row.MaxCurrent},
			Power:                MonthPowerSummary{Avg: row.AvgPower, Min: row.MinPower, Max: row.MaxPower},
			Frequency:            MonthFrequencySummary{Avg: row.AvgFrequency, Min: row.MinFrequency, Max: row.MaxFrequency},
			PowerFactor:          MonthPowerFactorSummary{Avg: row.AvgPowerFactor, Min: row.MinPowerFactor, Max: row.MaxPowerFactor},
		})
	}

	// Year summary query
	yearQuery := `SELECT
        TO_CHAR(MIN(d.timestamp), 'YYYY-MM-DD HH24:MI:SS') AS first_record_timestamp,
        TO_CHAR(MAX(d.timestamp), 'YYYY-MM-DD HH24:MI:SS') AS last_record_timestamp,
        MAX(d.energy) - MIN(d.energy) AS energy_consumed,
        COUNT(*) AS total_data,
        (EXTRACT(EPOCH FROM MAX(d.timestamp) - MIN(d.timestamp)) / NULLIF(COUNT(*) - 1, 0)) AS data_interval,
        COUNT(*) * 80 AS total_size_bytes,
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
    WHERE EXTRACT(YEAR FROM d.timestamp) = $1 AND d.unit_id = $2`

	logger.Info(referenceId, "Running yearQuery:", yearQuery)
	logger.Info(referenceId, fmt.Sprintf("Params: year=%d, device_id=%d", int(yearSelected), int64(deviceId)))

	var yearRaw YearDetailRaw
	err = conn.Get(&yearRaw, yearQuery, int(yearSelected), int64(deviceId)) // Fixed parameter order
	if err != nil {
		logger.Error(referenceId, "Year Detail Query Failed:", err.Error())
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error"
		return result
	}

	logger.Debug(referenceId, fmt.Sprintf("YearDetailRaw Dump: %+v", yearRaw))

	// Convert to YearDetail format
	yearSummary := YearDetail{
		FirstRecordTimestamp: yearRaw.FirstRecordTimestamp,
		LastRecordTimestamp:  yearRaw.LastRecordTimestamp,
		EnergyConsumption:    yearRaw.EnergyConsumption,
		TotalData:            yearRaw.TotalData,
		DataInterval:         yearRaw.DataInterval,
		TotalSizeBytes:       yearRaw.TotalSizeBytes,
		Voltage: YearVoltageSummary{
			Avg: yearRaw.AvgVoltage,
			Min: yearRaw.MinVoltage,
			Max: yearRaw.MaxVoltage,
		},
		Current: YearCurrentSummary{
			Avg: yearRaw.AvgCurrent,
			Min: yearRaw.MinCurrent,
			Max: yearRaw.MaxCurrent,
		},
		Power: YearPowerSummary{
			Avg: yearRaw.AvgPower,
			Min: yearRaw.MinPower,
			Max: yearRaw.MaxPower,
		},
		Frequency: YearFrequencySummary{
			Avg: yearRaw.AvgFrequency,
			Min: yearRaw.MinFrequency,
			Max: yearRaw.MaxFrequency,
		},
		PowerFactor: YearPowerFactorSummary{
			Avg: yearRaw.AvgPowerFactor,
			Min: yearRaw.MinPowerFactor,
			Max: yearRaw.MaxPowerFactor,
		},
		MonthList: monthList,
	}

	result.Payload = map[string]any{
		"year":                   int(yearSelected),
		"first_record_timestamp": yearSummary.FirstRecordTimestamp,
		"last_record_timestamp":  yearSummary.LastRecordTimestamp,
		"energy_consumption":     yearSummary.EnergyConsumption,
		"total_data":             yearSummary.TotalData,
		"data_interval":          yearSummary.DataInterval,
		"total_size_bytes":       yearSummary.TotalSizeBytes,
		"voltage":                yearSummary.Voltage,
		"current":                yearSummary.Current,
		"power":                  yearSummary.Power,
		"frequency":              yearSummary.Frequency,
		"power_factor":           yearSummary.PowerFactor,
		"month_list":             yearSummary.MonthList,
	}

	return result
}
