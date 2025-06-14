package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

type MonthMetric struct {
	Avg float64 `json:"avg"`
	Max float64 `json:"max"`
	Min float64 `json:"min"`
}

type MonthDetail struct {
	Year            int         `json:"year"`
	Month           int         `json:"month"`
	TotalEnergy     float64     `json:"total_energy"`
	FirstTimestamp  string      `json:"first_timestamp"`
	LastTimestamp   string      `json:"last_timestamp"`
	TotalData       int64       `json:"total_data"`
	AvgDataInterval float64     `json:"avg_data_interval"` // seconds
	Power           MonthMetric `json:"power"`
	Current         MonthMetric `json:"current"`
	Voltage         MonthMetric `json:"voltage"`
}

func Get_Report_Month_Detail(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]interface{}) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]interface{}),
	}

	// ===== PARAMETER VALIDATION =====
	deviceIdRaw, ok := param["device_id"]
	if !ok {
		logger.Error(referenceId, "ERROR - Get_Report_Month_Detail - device_id missing")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request: device_id missing"
		return result
	}
	deviceId, ok := convertToInt(deviceIdRaw)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Month_Detail - Invalid device_id: %+v", deviceIdRaw))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request: device_id"
		return result
	}

	yearRaw, ok := param["year"]
	if !ok {
		logger.Error(referenceId, "ERROR - Get_Report_Month_Detail - year missing")
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request: year missing"
		return result
	}
	year, ok := convertToInt(yearRaw)
	if !ok || year <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Month_Detail - Invalid year: %+v", yearRaw))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request: year"
		return result
	}

	monthRaw, ok := param["month"]
	if !ok {
		logger.Error(referenceId, "ERROR - Get_Report_Month_Detail - month missing")
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request: month missing"
		return result
	}
	month, ok := convertToInt(monthRaw)
	if !ok || month < 1 || month > 12 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Month_Detail - Invalid month: %+v", monthRaw))
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request: month"
		return result
	}

	// ========= QUERY EXECUTION =========
	logger.Info(referenceId, fmt.Sprintf("INFO - Get_Report_Year_Detail - Fetching data for device_id=%d year=%d", deviceId, year))

	// Cek eksistensi device
	var exists bool
	checkQuery := `SELECT EXISTS (SELECT 1 FROM device.unit WHERE id = $1)`
	err := conn.Get(&exists, checkQuery, deviceId)
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Year_Detail - Failed to check device existence: %v", err))
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error saat memeriksa perangkat"
		return result
	}
	if !exists {
		logger.Warning(referenceId, fmt.Sprintf("WARN - Get_Report_Year_Detail - Device ID %d tidak ditemukan", deviceId))
		result.ErrorCode = "404001"
		result.ErrorMessage = "Perangkat tidak ditemukan"
		return result
	}

	// ===== QUERY EXECUTION =====
	logger.Info(referenceId, fmt.Sprintf("INFO - Get_Report_Month_Detail - Fetching data for device_id=%d year=%d month=%d", deviceId, year, month))

	query := `
WITH base_data AS (
    SELECT
        timestamp,
        energy,
        power,
        current,
        voltage,
        EXTRACT(EPOCH FROM timestamp - LAG(timestamp) OVER (ORDER BY timestamp)) AS interval_seconds
    FROM device.data
    WHERE unit_id = $3
      AND EXTRACT(YEAR FROM timestamp)::int = $1
      AND EXTRACT(MONTH FROM timestamp)::int = $2
),
monthly_stats AS (
    SELECT
        DATE_TRUNC('month', timestamp) AS month,
        MAX(energy) AS max_energy,
        MIN(timestamp) AS first_timestamp,
        MAX(timestamp) AS last_timestamp,
        COUNT(*) AS total_data,
        AVG(interval_seconds) AS avg_data_interval,

        AVG(power) AS power_avg,
        MAX(power) AS power_max,
        MIN(power) AS power_min,

        AVG(current) AS current_avg,
        MAX(current) AS current_max,
        MIN(current) AS current_min,

        AVG(voltage) AS voltage_avg,
        MAX(voltage) AS voltage_max,
        MIN(voltage) AS voltage_min
    FROM base_data
    GROUP BY DATE_TRUNC('month', timestamp)
)

SELECT 
    $1::int AS year,
    $2::int AS month,
    COALESCE(MAX(max_energy), 0)::numeric AS total_energy,
    COALESCE(MIN(first_timestamp), '1970-01-01')::text AS first_timestamp,
    COALESCE(MAX(last_timestamp), '1970-01-01')::text AS last_timestamp,
    COALESCE(SUM(total_data), 0) AS total_data,
    COALESCE(AVG(avg_data_interval), 0)::numeric AS avg_data_interval,

    COALESCE(AVG(power_avg), 0)::numeric AS power_avg,
    COALESCE(MAX(power_max), 0) AS power_max,
    COALESCE(MIN(power_min), 0) AS power_min,

    COALESCE(AVG(current_avg), 0)::numeric AS current_avg,
    COALESCE(MAX(current_max), 0) AS current_max,
    COALESCE(MIN(current_min), 0) AS current_min,

    COALESCE(AVG(voltage_avg), 0)::numeric AS voltage_avg,
    COALESCE(MAX(voltage_max), 0) AS voltage_max,
    COALESCE(MIN(voltage_min), 0) AS voltage_min
FROM monthly_stats;

`

	var detail MonthDetail
	err = conn.QueryRowx(query, year, month, deviceId).Scan(
		&detail.Year,
		&detail.Month,
		&detail.TotalEnergy,
		&detail.FirstTimestamp,
		&detail.LastTimestamp,
		&detail.TotalData,
		&detail.AvgDataInterval,

		&detail.Power.Avg,
		&detail.Power.Max,
		&detail.Power.Min,

		&detail.Current.Avg,
		&detail.Current.Max,
		&detail.Current.Min,

		&detail.Voltage.Avg,
		&detail.Voltage.Max,
		&detail.Voltage.Min,
	)
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Month_Detail - Failed to query data for device_id=%d year=%d month=%d: %v", deviceId, year, month, err))
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	result.Payload["device_id"] = deviceId
	result.Payload["year"] = year
	result.Payload["month"] = month
	result.Payload["month_detail"] = detail
	result.Payload["status"] = "success"
	return result
}

// Helper function to convert interface{} to int
func convertToInt(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}
