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
	"bytes"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/xuri/excelize/v2"

	"monitoring_service/logger"
	"monitoring_service/utils"
)

type DataExportDay struct {
	Timestamp   time.Time `db:"timestamp"`
	Voltage     float64   `db:"voltage"`
	Current     float64   `db:"current"`
	Power       float64   `db:"power"`
	Energy      float64   `db:"energy"`
	Frequency   float64   `db:"frequency"`
	PowerFactor float64   `db:"power_factor"`
}

func Get_Report_Day_Export(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	// Validate inputs
	deviceId, ok := param["device_id"].(float64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, "Invalid device_id")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid device_id"
		return result
	}

	year, ok := param["year"].(float64)
	if !ok || year <= 0 {
		logger.Error(referenceId, "Invalid year")
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid year"
		return result
	}

	month, ok := param["month"].(float64)
	if !ok || month <= 0 || month > 12 {
		logger.Error(referenceId, "Invalid month")
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid month"
		return result
	}
	// Optional: Validate day
	var day int64 = 0
	if dateDayParam, ok := param["date_day"].(float64); ok {
		if dateDayParam < 1 || dateDayParam > 31 {
			logger.Error(referenceId, fmt.Sprintf("Invalid date day: %v", param["date_day"]))
			result.ErrorCode = "400005"
			result.ErrorMessage = "Invalid date_day"
			return result
		}
		day = int64(dateDayParam)
	}

	fileType, ok := param["file_type"].(string)
	if !ok || (fileType != "csv" && fileType != "excel" && fileType != "sql") {
		logger.Error(referenceId, fmt.Sprintf("Invalid file_type: %v", fileType))
		result.ErrorCode = "400004"
		result.ErrorMessage = "Invalid file_type"
		return result
	}

	var data []DataExport
	query := `
		SELECT timestamp, voltage, current, power, energy, frequency, power_factor
		FROM device.data
		WHERE unit_id = $1 AND EXTRACT(YEAR FROM timestamp) = $2 AND EXTRACT(MONTH FROM timestamp) = $3`
	args := []any{int64(deviceId), int64(year), int64(month)}

	if day > 0 {
		query += ` AND EXTRACT(DAY FROM timestamp) = $4`
		args = append(args, day)
	}

	query += ` ORDER BY timestamp ASC`

	err := conn.Select(&data, query, args...)
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("Failed to query data: %v", err))
		result.ErrorCode = "500001"
		result.ErrorMessage = "Failed to get data"
		return result
	}

	// Create export file
	var fileName string
	if day > 0 {
		fileName = fmt.Sprintf("device_%d_%04.0f_%02.0f_%02d_export.%s", int64(deviceId), year, month, day, fileType)
	} else {
		fileName = fmt.Sprintf("device_%d_%04.0f_%02.0f_export.%s", int64(deviceId), year, month, fileType)
	}

	outputPath := filepath.Join("/tmp", fileName)

	switch fileType {
	case "csv":
		file, err := os.Create(outputPath)
		if err != nil {
			logger.Error(referenceId, err.Error())
			result.ErrorCode = "500002"
			result.ErrorMessage = "Failed to create file"
			return result
		}
		writer := csv.NewWriter(file)
		writer.Write([]string{"timestamp", "voltage", "current", "power", "energy", "frequency", "power_factor"})
		for _, row := range data {
			writer.Write([]string{
				row.Timestamp.Format(time.RFC3339),
				fmt.Sprintf("%.2f", row.Voltage),
				fmt.Sprintf("%.2f", row.Current),
				fmt.Sprintf("%.2f", row.Power),
				fmt.Sprintf("%.2f", row.Energy),
				fmt.Sprintf("%.2f", row.Frequency),
				fmt.Sprintf("%.2f", row.PowerFactor),
			})
		}
		writer.Flush()
		file.Close()

	case "excel":
		f := excelize.NewFile()
		sheet := "Sheet1"
		headers := []string{"Timestamp", "Voltage", "Current", "Power", "Energy", "Frequency", "Power Factor"}
		for i, h := range headers {
			cell := fmt.Sprintf("%c1", 'A'+i)
			f.SetCellValue(sheet, cell, h)
		}
		for i, row := range data {
			f.SetCellValue(sheet, fmt.Sprintf("A%d", i+2), row.Timestamp.Format(time.RFC3339))
			f.SetCellValue(sheet, fmt.Sprintf("B%d", i+2), row.Voltage)
			f.SetCellValue(sheet, fmt.Sprintf("C%d", i+2), row.Current)
			f.SetCellValue(sheet, fmt.Sprintf("D%d", i+2), row.Power)
			f.SetCellValue(sheet, fmt.Sprintf("E%d", i+2), row.Energy)
			f.SetCellValue(sheet, fmt.Sprintf("F%d", i+2), row.Frequency)
			f.SetCellValue(sheet, fmt.Sprintf("G%d", i+2), row.PowerFactor)
		}
		if err := f.SaveAs(outputPath); err != nil {
			logger.Error(referenceId, err.Error())
			result.ErrorCode = "500003"
			result.ErrorMessage = "Failed to save Excel file"
			return result
		}

	case "sql":
		var buffer bytes.Buffer
		for _, row := range data {
			sqlLine := fmt.Sprintf(
				`INSERT INTO device.data (unit_id, timestamp, voltage, current, power, energy, frequency, power_factor) VALUES (%d, '%s', %.2f, %.2f, %.2f, %.2f, %.2f, %.2f);`+"\n",
				int64(deviceId),
				row.Timestamp.Format("2006-01-02 15:04:05"),
				row.Voltage, row.Current, row.Power, row.Energy, row.Frequency, row.PowerFactor,
			)
			buffer.WriteString(sqlLine)
		}
		err := os.WriteFile(outputPath, buffer.Bytes(), 0644)
		if err != nil {
			logger.Error(referenceId, err.Error())
			result.ErrorCode = "500004"
			result.ErrorMessage = "Failed to write SQL file"
			return result
		}
	}

	// Return path or filename
	result.Payload["file_path"] = outputPath
	result.Payload["device_id"] = deviceId
	result.Payload["year"] = year
	result.Payload["month"] = month
	result.Payload["day"] = day
	result.Payload["status"] = "sucess"
	return result
}
