package process

import (
	"database/sql"
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

type AvailableYears struct {
	Year   int     `db:"year" json:"year"`
	Energy float64 `db:"energy" json:"total_energy"`
}

func Get_Report_Available_Years(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	// ========== PARAMETER VALIDATION ==========
	deviceIdFloat, ok := param["device_id"].(float64)
	if !ok || deviceIdFloat <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Available_Years - Invalid device_id: %+v", param["device_id"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request: device_id"
		return result
	}
	deviceId := int(deviceIdFloat)

	sortType, ok := param["sort_type"].(string)
	if !ok || (sortType != "asc" && sortType != "desc") {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Available_Years - Invalid sort_type: %+v", param["sort_type"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request: sort_type must be 'asc' or 'desc'"
		return result
	}

	pageSizeFloat, ok := param["page_size"].(float64)
	if !ok || pageSizeFloat <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Available_Years - Invalid page_size: %+v", param["page_size"]))
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request: page_size"
		return result
	}
	pageSize := int(pageSizeFloat)

	orderBy, ok := param["order_by"].(string)
	if !ok || (orderBy != "year" && orderBy != "energy") {

	}

	// ========== FETCH MIN/MAX/TOTAL YEAR ==========
	// add total ener
	var (
		minYearNull, maxYearNull, totalYearNull sql.NullInt64
	)
	yearInfoQuery := `
		SELECT 
			MIN(EXTRACT(YEAR FROM timestamp)::int) AS min_year,
			MAX(EXTRACT(YEAR FROM timestamp)::int) AS max_year,
			COUNT(DISTINCT EXTRACT(YEAR FROM timestamp)::int) AS total_year
		FROM device.data
		WHERE unit_id = $1`
	err := conn.QueryRowx(yearInfoQuery, deviceId).Scan(&minYearNull, &maxYearNull, &totalYearNull)
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Report_Available_Years - Failed to fetch year pagination info for device_id=%d: %v", deviceId, err))
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Validasi jika NULL
	minYear := 0
	if minYearNull.Valid {
		minYear = int(minYearNull.Int64)
	}

	maxYear := 0
	if maxYearNull.Valid {
		maxYear = int(maxYearNull.Int64)
	}

	totalYear := 0
	if totalYearNull.Valid {
		totalYear = int(totalYearNull.Int64)
	}

	// ========== DETERMINE START YEAR ==========
	startYearFloat, hasStartYear := param["start_year"].(float64)
	var startYear int
	if hasStartYear && startYearFloat > 0 {
		startYear = int(startYearFloat)
	} else {
		if sortType == "asc" {
			startYear = minYear
		} else {
			startYear = maxYear
		}
	}

	// ========== DETERMINE DIRECTION ==========
	direction, _ := param["direction"].(string)
	var op, order string
	if direction == "prev" {
		if sortType == "asc" {
			op, order = "<", "DESC"
		} else {
			op, order = ">", "ASC"
		}
	} else {
		if sortType == "asc" {
			op, order = ">", "ASC"
		} else {
			op, order = "<", "DESC"
		}
	}

	// ========== BUILD YEAR QUERY DENGAN TOTAL ENERGI ==========
	var yearListQuery string
	var args []any

	if hasStartYear {
		yearListQuery = `
		SELECT 
			year,
			SUM(max_energy)::numeric AS energy
		FROM (
			SELECT
				EXTRACT(YEAR FROM timestamp)::int AS year,
				EXTRACT(MONTH FROM timestamp)::int AS month,
				MAX(energy) AS max_energy
			FROM device.data
			WHERE unit_id = $1
			GROUP BY year, month
		) monthly_energy
		WHERE year ` + op + ` $2
		GROUP BY year
		ORDER BY year ` + order + `
		LIMIT $3
	`
		args = []any{deviceId, startYear, pageSize}
	} else {
		yearListQuery = `
		SELECT 
			year,
			SUM(max_energy)::numeric AS energy
		FROM (
			SELECT
				EXTRACT(YEAR FROM timestamp)::int AS year,
				EXTRACT(MONTH FROM timestamp)::int AS month,
				MAX(energy) AS max_energy
			FROM device.data
			WHERE unit_id = $1
			GROUP BY year, month
		) monthly_energy
		GROUP BY year
		ORDER BY year ` + sortType + `
		LIMIT $2
	`
		args = []any{deviceId, pageSize}
	}

	// ========== EXECUTE QUERY ==========
	var yearList []AvailableYears
	err = conn.Select(&yearList, yearListQuery, args...)
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf(
			"ERROR - Get_Report_Available_Years - Failed to fetch year+energy list for device_id=%d, query=%q, args=%v, error=%v",
			deviceId, yearListQuery, args, err))
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// ========== BUILD PAYLOAD ==========
	result.Payload["year_list"] = yearList
	result.Payload["min_year"] = minYear
	result.Payload["max_year"] = maxYear
	result.Payload["total_year"] = totalYear
	result.Payload["device_id"] = deviceId
	result.Payload["status"] = "success"

	logger.Info(referenceId, fmt.Sprintf(
		"ERROR - Get_Report_Available_Years - Success fetch available years for device_id=%d | start_year=%d | sort=%s | direction=%s | page_size=%d | years_fetched=%v",
		deviceId, startYear, sortType, direction, pageSize, yearList,
	))

	return result
}
