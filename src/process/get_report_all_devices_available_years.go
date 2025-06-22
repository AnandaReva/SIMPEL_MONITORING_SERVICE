package process

import (
	"database/sql"
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

type AllDevicesAvailableYears struct {
	Year   int     `db:"year" json:"year"`
	Energy float64 `db:"energy" json:"total_energy"`
}

func Get_Report_All_Devices_Available_Years(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	sortType, ok := param["sort_type"].(string)
	if !ok || (sortType != "asc" && sortType != "desc") {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Invalid sort_type: %+v", param["sort_type"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request: sort_type must be 'asc' or 'desc'"
		return result
	}

	pageSizeFloat, ok := param["page_size"].(float64)
	if !ok || pageSizeFloat <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Invalid page_size: %+v", param["page_size"]))
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request: page_size"
		return result
	}
	pageSize := int(pageSizeFloat)

	orderBy, ok := param["order_by"].(string)
	if !ok || (orderBy != "year" && orderBy != "energy") {
		orderBy = "year"
	}

	var (
		minYearNull, maxYearNull, totalYearNull sql.NullInt64
	)
	yearInfoQuery := `
		SELECT 
			MIN(EXTRACT(YEAR FROM timestamp)::int) AS min_year,
			MAX(EXTRACT(YEAR FROM timestamp)::int) AS max_year,
			COUNT(DISTINCT EXTRACT(YEAR FROM timestamp)::int) AS total_year
		FROM device.data`
	err := conn.QueryRowx(yearInfoQuery).Scan(&minYearNull, &maxYearNull, &totalYearNull)
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Failed to fetch year pagination info: %v", err))
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	minYear, maxYear, totalYear := 0, 0, 0
	if minYearNull.Valid {
		minYear = int(minYearNull.Int64)
	}
	if maxYearNull.Valid {
		maxYear = int(maxYearNull.Int64)
	}
	if totalYearNull.Valid {
		totalYear = int(totalYearNull.Int64)
	}

	startYearFloat, hasStartYear := param["start_year"].(float64)
	var startYear int
	if hasStartYear && startYearFloat > 0 {
		startYear = int(startYearFloat)
	} else {
		startYear = map[bool]int{true: minYear, false: maxYear}[sortType == "asc"]
	}

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

	var yearListQuery string
	var args []any

	if hasStartYear {
		yearListQuery = fmt.Sprintf(`
		SELECT 
			year,
			SUM(max_energy)::numeric AS energy
		FROM (
			SELECT
				EXTRACT(YEAR FROM timestamp)::int AS year,
				EXTRACT(MONTH FROM timestamp)::int AS month,
				MAX(energy) AS max_energy
			FROM device.data
			GROUP BY year, month
		) monthly_energy
		WHERE year %s $1
		GROUP BY year
		ORDER BY %s %s
		LIMIT $2`, op, orderBy, order)
		args = []any{startYear, pageSize}
	} else {
		yearListQuery = fmt.Sprintf(`
		SELECT 
			year,
			SUM(max_energy)::numeric AS energy
		FROM (
			SELECT
				EXTRACT(YEAR FROM timestamp)::int AS year,
				EXTRACT(MONTH FROM timestamp)::int AS month,
				MAX(energy) AS max_energy
			FROM device.data
			GROUP BY year, month
		) monthly_energy
		GROUP BY year
		ORDER BY %s %s
		LIMIT $1`, orderBy, sortType)
		args = []any{pageSize}
	}

	var yearList []AllDevicesAvailableYears
	err = conn.Select(&yearList, yearListQuery, args...)
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf(
			"ERROR - Failed to fetch year+energy list: query=%q, args=%v, error=%v",
			yearListQuery, args, err))
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error"
		return result
	}

	result.Payload = map[string]any{
		"year_list":  yearList,
		"min_year":   minYear,
		"max_year":   maxYear,
		"total_year": totalYear,
		"status":     "success",
	}

	logger.Info(referenceId, fmt.Sprintf(
		"INFO - Success fetch all devices available years | start_year=%d | sort=%s | direction=%s | page_size=%d | years_fetched=%v",
		startYear, sortType, direction, pageSize, len(yearList),
	))

	return result
}
