// simpel=> \d device.data;

// 											 Table "device.data"
// 	Column    |            Type             | Collation | Nullable |                 Default

// --------------+-----------------------------+-----------+----------+------------------------------------------

// 	id           | bigint                      |           | not null | nextval('device.data2_id_seq'::regclass)
// 	unit_id      | bigint                      |           | not null |
// 	timestamp    | timestamp without time zone |           | not null | now()
// 	voltage      | double precision            |           | not null |
// 	current      | double precision            |           | not null |
// 	power        | double precision            |           | not null |
// 	energy       | double precision            |           | not null |
// 	frequency    | double precision            |           | not null |
// 	power_factor | double precision            |           | not null |

// Indexes:

// 	"data_tstamp_idx" btree ("timestamp" DESC)
// 	"data_unique_idx" UNIQUE, btree (id, "timestamp")

// Foreign-key constraints:

// 	"fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE

// Triggers:

// 		ts_insert_blocker BEFORE INSERT ON device.data FOR EACH ROW EXECUTE FUNCTION _timescaledb_functions.insert_blocker()

// 	  exp get years with pagination

// SELECT

// 	EXTRACT(YEAR FROM timestamp) AS year,
// 	MIN(timestamp) AS first_timestamp,
// 	MAX(timestamp) AS last_timestamp,
// 	COUNT(*) AS total_data

// FROM device.data
// GROUP BY year
// ORDER BY year DESC
// LIMIT 5 OFFSET 0;

// // total years
// SELECT COUNT(DISTINCT EXTRACT(YEAR FROM timestamp)) AS total_years
// FROM device.data;
// */
package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

type YearList struct {
	Year                 int     `db:"year" json:"year"`
	FirstRecordTimestamp string  `db:"first_record_timestamp" json:"first_record_timestamp"`
	LastRecordTimestamp  string  `db:"last_record_timestamp" json:"last_record_timestamp"`
	EnergyConsumedCount  float64 `db:"max_energy" json:"energy_consumed_count"`
	TotalData            int     `db:"total_data" json:"total_data"`
	DataInterval         float64 `db:"data_interval" json:"data_interval"`
	TotalSize            float64 `db:"total_size" json:"total_size_bytes"`

	Voltage     YearVoltageListSummary     `json:"voltage"`
	Current     YearCurrentListSummary     `json:"current"`
	Power       YearPowerListSummary       `json:"power"`
	Frequency   YearFrequencyListSummary   `json:"frequency"`
	PowerFactor YearPowerFactorListSummary `json:"power_factor"`
}

type YearVoltageListSummary struct {
	Avg float64 `json:"avg"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

type YearCurrentListSummary struct {
	Avg float64 `json:"avg"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

type YearPowerListSummary struct {
	Avg float64 `json:"avg"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

type YearFrequencyListSummary struct {
	Avg float64 `json:"avg"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

type YearPowerFactorListSummary struct {
	Avg float64 `json:"avg"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

type yearRaw struct {
	YearList

	AvgVoltage float64 `db:"avg_voltage"`
	MinVoltage float64 `db:"min_voltage"`
	MaxVoltage float64 `db:"max_voltage"`

	AvgCurrent float64 `db:"avg_current"`
	MinCurrent float64 `db:"min_current"`
	MaxCurrent float64 `db:"max_current"`

	AvgPower float64 `db:"avg_power"`
	MinPower float64 `db:"min_power"`
	MaxPower float64 `db:"max_power"`

	AvgFrequency float64 `db:"avg_frequency"`
	MinFrequency float64 `db:"min_frequency"`
	MaxFrequency float64 `db:"max_frequency"`

	AvgPowerFactor float64 `db:"avg_power_factor"`
	MinPowerFactor float64 `db:"min_power_factor"`
	MaxPowerFactor float64 `db:"max_power_factor"`
}

/*
exp case : data years available (2000- 2025)
*/

type yearPaginationVar struct {
	MaxYear   int    `db:"max_year" json:"max_year"`
	MinYear   int    `db:"min_year" json:"min_year"`
	TotalYear int    `db:"total_year" json:"total_year"`
	Direction string `json:"direction"`
	SortType  string `json:"sort_type"`
	StartYear int    `json:"start_year"`
	OrderBy   string `json:"order_by"`
	PageSize  int    `json:"page_size"`
}

func Get_Report_Year_List(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	// Validate device_id
	deviceId, ok := param["device_id"].(float64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, "Invalid device_id: ", param["device_id"])
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid device ID"
		return result
	}

	// Validate sort_type
	sortType, ok := param["sort_type"].(string)
	if !ok || (sortType != "asc" && sortType != "desc") {
		logger.Error(referenceId, "Invalid sort_type: ", param["sort_type"])
		result.ErrorCode = "400005"
		result.ErrorMessage = "Invalid sort type"
		return result
	}

	// Validate page_size
	pageSize, ok := param["page_size"].(float64)
	if !ok || pageSize <= 0 {
		logger.Error(referenceId, "Invalid page_size: ", param["page_size"])
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid page size"
		return result
	}

	var yearPageVar yearPaginationVar

	deviceIdInt := int(deviceId)
	yearPageVar.PageSize = int(pageSize)

	// Base query without pagination filter

	baseQuery := `
    WITH monthly_energy AS (
        SELECT EXTRACT(YEAR FROM timestamp)::int AS year,
               EXTRACT(MONTH FROM timestamp)::int AS month,
               MAX(energy) AS max_energy
        FROM device.data
        WHERE unit_id = $1
        GROUP BY year, month
    ),
    yearly_summary AS (
        SELECT EXTRACT(YEAR FROM d.timestamp)::int AS year,
               d.unit_id,
               TO_CHAR(MIN(d.timestamp), 'YYYY-MM-DD HH24:MI:SS') AS first_record_timestamp,
               TO_CHAR(MAX(d.timestamp), 'YYYY-MM-DD HH24:MI:SS') AS last_record_timestamp,
               COUNT(*)::int AS total_data,
               (EXTRACT(EPOCH FROM MAX(d.timestamp) - MIN(d.timestamp)) / NULLIF(COUNT(*) - 1, 0)) AS data_interval,
               SUM(pg_column_size(d.*))::float AS total_size,
               AVG(voltage) AS avg_voltage,
               MIN(voltage) AS min_voltage,
               MAX(voltage) AS max_voltage,
               AVG(current) AS avg_current,
               MIN(current) AS min_current,
               MAX(current) AS max_current,
               AVG(power) AS avg_power,
               MIN(power) AS min_power,
               MAX(power) AS max_power,
               AVG(frequency) AS avg_frequency,
               MIN(frequency) AS min_frequency,
               MAX(frequency) AS max_frequency,
               AVG(power_factor) AS avg_power_factor,
               MIN(power_factor) AS min_power_factor,
               MAX(power_factor) AS max_power_factor
        FROM device.data d
        WHERE unit_id = $1
        GROUP BY year, d.unit_id
    )
    SELECT ys.year, 
           COALESCE(ys.first_record_timestamp, '') AS first_record_timestamp,
           COALESCE(ys.last_record_timestamp, '') AS last_record_timestamp,
           COALESCE(ys.total_data, 0) AS total_data,
           COALESCE(ys.total_size, 0) AS total_size,
           COALESCE(ys.data_interval, 0) AS data_interval,
           COALESCE(SUM(me.max_energy), 0) AS max_energy,
           COALESCE(ys.avg_voltage, 0) AS avg_voltage,
           COALESCE(ys.min_voltage, 0) AS min_voltage,
           COALESCE(ys.max_voltage, 0) AS max_voltage,
           COALESCE(ys.avg_current, 0) AS avg_current,
           COALESCE(ys.min_current, 0) AS min_current,
           COALESCE(ys.max_current, 0) AS max_current,
           COALESCE(ys.avg_power, 0) AS avg_power,
           COALESCE(ys.min_power, 0) AS min_power,
           COALESCE(ys.max_power, 0) AS max_power,
           COALESCE(ys.avg_frequency, 0) AS avg_frequency,
           COALESCE(ys.min_frequency, 0) AS min_frequency,
           COALESCE(ys.max_frequency, 0) AS max_frequency,
           COALESCE(ys.avg_power_factor, 0) AS avg_power_factor,
           COALESCE(ys.min_power_factor, 0) AS min_power_factor,
           COALESCE(ys.max_power_factor, 0) AS max_power_factor
    FROM yearly_summary ys
    LEFT JOIN monthly_energy me ON ys.year = me.year
    WHERE ys.unit_id = $1`

	// Handle pagination
	startYearParam, hasStartYear := param["start_year"].(float64)

	var rawList []yearRaw
	var err error

	if !hasStartYear || startYearParam <= 0 {
		// First page - no year filter
		query := baseQuery + `
        GROUP BY ys.year, ys.first_record_timestamp, ys.last_record_timestamp, ys.total_data, ys.total_size, ys.data_interval,
                 ys.avg_voltage, ys.min_voltage, ys.max_voltage,
                 ys.avg_current, ys.min_current, ys.max_current,
                 ys.avg_power, ys.min_power, ys.max_power,
                 ys.avg_frequency, ys.min_frequency, ys.max_frequency,
                 ys.avg_power_factor, ys.min_power_factor, ys.max_power_factor
        ORDER BY ys.year ` + sortType + `
        LIMIT $2`

		logger.Debug(referenceId, "Executing SQL (no year filter):", query)

		err = conn.Select(&rawList, query, deviceIdInt, yearPageVar.PageSize)

	} else {
		// Subsequent pages - with year filter
		yearPageVar.StartYear = int(startYearParam)
		direction, _ := param["direction"].(string)
		if direction != "next" && direction != "prev" {
			direction = "next"
		}

		comparisonOp, orderClause := getPaginationOperators(direction, sortType)

		query := baseQuery + ` AND ys.year ` + comparisonOp + ` $2
        GROUP BY ys.year, ys.first_record_timestamp, ys.last_record_timestamp, ys.total_data, ys.total_size, ys.data_interval,
                 ys.avg_voltage, ys.min_voltage, ys.max_voltage,
                 ys.avg_current, ys.min_current, ys.max_current,
                 ys.avg_power, ys.min_power, ys.max_power,
                 ys.avg_frequency, ys.min_frequency, ys.max_frequency,
                 ys.avg_power_factor, ys.min_power_factor, ys.max_power_factor
        ORDER BY ys.year ` + orderClause + `
        LIMIT $3`

		logger.Debug(referenceId, "Final SQL query and params:", map[string]interface{}{
			"sql": query,
			"params": []any{
				deviceIdInt,
				startYearParam, // atau abaikan jika halaman pertama
				yearPageVar.PageSize,
			},
		})

		err = conn.Select(&rawList, query, deviceIdInt, yearPageVar.StartYear, yearPageVar.PageSize)

		if direction == "prev" {
			reverseYearRawList(rawList)
		}

	}

	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("Query failed for device_id=%d, start_year=%v: %v", deviceIdInt, startYearParam, err))

		result.ErrorCode = "500002"
		result.ErrorMessage = "Failed to fetch year data"
		return result
	}

	// Transform data and get total years count
	yearList := transformRawToYearList(rawList)

	// Ambil info min, max, dan total year dalam 1 query
	yearPageVar, err = getYearPaginationInfo(conn, deviceIdInt)
	if err != nil {
		logger.Error(referenceId, "Failed to fetch pagination info: ", err.Error())
		result.ErrorCode = "500003"
		result.ErrorMessage = "Failed to fetch pagination metadata"
		return result
	}

	logger.Debug(referenceId, "Get_Report_Year_List - yearPageVar : ", yearPageVar)

	// Persiapkan response
	result.Payload["year_list"] = yearList
	result.Payload["max_year"] = yearPageVar.MaxYear
	result.Payload["min_year"] = yearPageVar.MinYear
	result.Payload["total_year"] = yearPageVar.TotalYear

	return result
}

func getPaginationOperators(direction, sortType string) (comparisonOp string, orderClause string) {
	if direction == "next" {
		if sortType == "asc" {
			return ">=", "ASC"
		}
		return "<=", "DESC"
	} else {
		// direction == "prev"
		if sortType == "asc" {
			return "<=", "DESC" // reverse
		}
		return ">=", "ASC" // reverse
	}
}

func reverseYearRawList(list []yearRaw) {
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
}

// Fungsi helper untuk transform data raw ke struktur response
func transformRawToYearList(rawList []yearRaw) []YearList {
	var yearList []YearList
	for _, raw := range rawList {
		raw.Voltage = YearVoltageListSummary{
			Avg: raw.AvgVoltage,
			Min: raw.MinVoltage,
			Max: raw.MaxVoltage,
		}
		raw.Current = YearCurrentListSummary{
			Avg: raw.AvgCurrent,
			Min: raw.MinCurrent,
			Max: raw.MaxCurrent,
		}
		raw.Power = YearPowerListSummary{
			Avg: raw.AvgPower,
			Min: raw.MinPower,
			Max: raw.MaxPower,
		}
		raw.Frequency = YearFrequencyListSummary{
			Avg: raw.AvgFrequency,
			Min: raw.MinFrequency,
			Max: raw.MaxFrequency,
		}
		raw.PowerFactor = YearPowerFactorListSummary{
			Avg: raw.AvgPowerFactor,
			Min: raw.MinPowerFactor,
			Max: raw.MaxPowerFactor,
		}
		yearList = append(yearList, raw.YearList)
	}
	return yearList
}

func getYearPaginationInfo(conn *sqlx.DB, deviceId int) (yearPaginationVar, error) {
	var info yearPaginationVar
	err := conn.Get(&info, `
		SELECT 
			MIN(EXTRACT(YEAR FROM timestamp))::int AS min_year,
			MAX(EXTRACT(YEAR FROM timestamp))::int AS max_year,
			COUNT(DISTINCT EXTRACT(YEAR FROM timestamp)::int) AS total_year
		FROM device.data
		WHERE unit_id = $1
	`, deviceId)
	return info, err
}
