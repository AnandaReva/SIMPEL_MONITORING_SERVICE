/*
Exp response:

	payload: {
		year_detail: {
			device_id: 123,
			year: 2023,
			total_energy: 1234.56,
			first_timstamp: "2025-01-01 00:00:00",
			last_timestamp: "2025-12-30 00:00:00",
			total_data: 220000,
			avg_data_interval: 5, //s
			power: {
				avg: 123.45,
				max: 200.00,
				min: 50.00
			},
			Current: {
				avg: 10.00,
				max: 20.00,
				min: 5.00
			},
			Voltage: {
				avg: 220.00,
				max: 240.00,
				min: 200.00
			}
		}
	}
*/
package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

type YearMetric struct {
	Avg float64 `json:"avg"`
	Max float64 `json:"max"`
	Min float64 `json:"min"`
}

type YearDetail struct {
	Year                 int        `json:"year"`
	TotalEnergy          float64    `json:"total_energy"`
	FirstTimestamp       string     `json:"first_timestamp"`
	LastTimestamp        string     `json:"last_timestamp"`
	TotalData            int64      `json:"total_data"`
	AvgDataIntervalInSec float64    `json:"avg_data_interval"` // seconds
	Power                YearMetric `json:"power"`
	Current              YearMetric `json:"current"`
	Voltage              YearMetric `json:"voltage"`
}

func Get_Report_Year_Detail(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	// ========= PARAMETER VALIDATION =========
	deviceIdFloat, ok := param["device_id"].(float64)
	if !ok || deviceIdFloat <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Year_Detail - Invalid device_id: %+v", param["device_id"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request: device_id"
		return result
	}
	deviceId := int(deviceIdFloat)

	yearFloat, ok := param["year"].(float64)
	if !ok || yearFloat <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Year_Detail - Invalid year: %+v", param["year"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request: year"
		return result
	}
	year := int(yearFloat)

	// ========= QUERY EXECUTION =========
	logger.Info(referenceId, fmt.Sprintf("INFO - Get_Report_Year_Detail - Fetching data for device_id=%d year=%d", deviceId, year))
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
    WHERE unit_id = $2
      AND EXTRACT(YEAR FROM timestamp)::int = $1
),
monthly_stats AS (
    SELECT
        DATE_TRUNC('month', timestamp) AS month,
        MAX(energy) AS max_energy,
        MIN(timestamp) AS first_timestamp,
        MAX(timestamp) AS last_timestamp,
        COUNT(*) AS total_data,
        AVG(interval_seconds)::numeric AS avg_data_interval,

        AVG(power)::numeric AS power_avg,
        MAX(power) AS power_max,
        MIN(power) AS power_min,

        AVG(current)::numeric AS current_avg,
        MAX(current) AS current_max,
        MIN(current) AS current_min,

        AVG(voltage)::numeric AS voltage_avg,
        MAX(voltage) AS voltage_max,
        MIN(voltage) AS voltage_min
    FROM base_data
    GROUP BY DATE_TRUNC('month', timestamp)
)

SELECT 
    $1::int AS year,
    COALESCE(SUM(max_energy), 0)::numeric AS total_energy,
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

	var detail YearDetail
	err := conn.QueryRowx(query, year, deviceId).Scan(
		&detail.Year,
		&detail.TotalEnergy,
		&detail.FirstTimestamp,
		&detail.LastTimestamp,
		&detail.TotalData,
		&detail.AvgDataIntervalInSec,

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
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Year_Detail - Failed to query data for device_id=%d year=%d: %v", deviceId, year, err))
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// ========= SET PAYLOAD =========
	result.Payload["year_detail"] = detail
	result.Payload["device_id"] = deviceId
	result.Payload["year"] = year
	result.Payload["status"] = "success"
	return result
}
