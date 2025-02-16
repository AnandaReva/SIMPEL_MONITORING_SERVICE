package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

/*
 \d device.data ;
                                       Table "device.data"
    Column    |       Type       | Collation | Nullable |                 Default
--------------+------------------+-----------+----------+-----------------------------------------
 id           | bigint           |           | not null | nextval('device.data_id_seq'::regclass)
 unit_id      | bigint           |           | not null |
 tstamp	      | bigint           |           | not null | EXTRACT(epoch FROM now())::bigint
 voltage      | double precision |           | not null |
 current      | double precision |           | not null |
 power        | double precision |           | not null |
 energy       | double precision |           | not null |
 frequency    | double precision |           | not null |
 power_factor | double precision |           | not null |
Indexes:
    "data_new_pkey1" PRIMARY KEY, btree (id)
Foreign-key constraints:
    "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE */

type MonthlyData struct {
	Id           int64   `db:"id"`
	Tstamp       float64 `db:"tstamp"`
	Voltage      float64 `db:"voltage"`
	Current      float64 `db:"current"`
	Power        float64 `db:"power"`
	Energy       float64 `db:"energy"`
	Frequency    float64 `db:"frequency"`
	PowerFactor  float64 `db:"power_factor"`
	TotalRecords float64 `db:"total_records"`
}

/////////!!! DONT SPECIFY ERROR MESSAGE TO CLIENT////////////////////////////

func GetMonthlyData(reference_id string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	// Validasi parameter
	deviceID, ok := param["device_id"].(int64)
	if !ok || deviceID <= 0 {
		logger.Error(reference_id, "ERROR - GetMonthlyData - Invalid device_id")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	year, ok := param["year"].(int16)
	if !ok || year < 2000 {
		logger.Error(reference_id, "ERROR - GetMonthlyData - Invalid year")
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request"
		return result
	}

	function, ok := param["function"].(string)
	if !ok || (function != "sum" && function != "avg") {
		logger.Error(reference_id, "ERROR - GetMonthlyData - Invalid function")
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request"
		return result
	}

	var query string
	switch function {
	case "sum":
		query = `SELECT 
			DATE_TRUNC('month', TO_TIMESTAMP(tstamp))::DATE AS month, 
			COUNT(*) AS total_records, 
			AVG(voltage) AS voltage, 
			AVG(current) AS current, 
			SUM(power) AS power, 
			SUM(energy) AS energy, 
			AVG(frequency) AS frequency, 
			AVG(power_factor) AS power_factor 
		FROM device.data 
		WHERE tstamp >= EXTRACT(EPOCH FROM TIMESTAMP $1) 
		AND tstamp < EXTRACT(EPOCH FROM TIMESTAMP $2) 
		AND unit_id = $3 
		GROUP BY month ORDER BY month;`

	case "avg":
		query = `SELECT 
			DATE_TRUNC('month', TO_TIMESTAMP(tstamp))::DATE AS month, 
			COUNT(*) AS total_records, 
			AVG(voltage) AS voltage, 
			AVG(current) AS current, 
			AVG(power) AS power, 
			AVG(energy) AS energy, 
			AVG(frequency) AS frequency, 
			AVG(power_factor) AS power_factor 
		FROM device.data 
		WHERE tstamp >= EXTRACT(EPOCH FROM TIMESTAMP $1) 
		AND tstamp < EXTRACT(EPOCH FROM TIMESTAMP $2) 
		AND unit_id = $3 
		GROUP BY month ORDER BY month;`
	}

	// Rentang tanggal
	startDate := fmt.Sprintf("%d-01-01 00:00:00", year)
	endDate := fmt.Sprintf("%d-01-01 00:00:00", year+1)

	// Query total data setiap bulan
	var monthlyData []MonthlyData
	err := conn.Select(&monthlyData, query, startDate, endDate, deviceID)
	if err != nil {
		logger.Error(reference_id, fmt.Sprintf("ERROR - GetMonthlyData - Query execution failed: %v", err))
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Query total semua data dalam satu tahun
	var totalRecords int64
	totalQuery := `SELECT COUNT(*) FROM device.data WHERE tstamp >= EXTRACT(EPOCH FROM TIMESTAMP $1) 
	AND tstamp < EXTRACT(EPOCH FROM TIMESTAMP $2) AND unit_id = $3;`
	err = conn.Get(&totalRecords, totalQuery, startDate, endDate, deviceID)
	if err != nil {
		logger.Error(reference_id, fmt.Sprintf("ERROR - GetMonthlyData - Total records query failed: %v", err))
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error"
		return result
	}

	// Menyimpan hasil ke dalam response
	result.Payload["monthly_data"] = monthlyData
	result.Payload["total_records"] = totalRecords

	logger.Info(reference_id, fmt.Sprintf("INFO - Found %d monthly records, total %d records", len(monthlyData), totalRecords))

	return result
}
