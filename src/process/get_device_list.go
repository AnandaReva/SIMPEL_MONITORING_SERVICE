/* simpel=*> \d device.unit
										 Table "device.unit"
	 Column      |          Type          | Collation | Nullable |              Default
-----------------+------------------------+-----------+----------+-----------------------------------
 id              | bigint                 |           | not null | nextval('device_id_sq'::regclass)
 name            | character varying(255) |           | not null |
 st          | integer                |           | not null |
 salt            | character varying(64)  |           | not null |
 salted_password | character varying(128) |           | not null |
 data            | jsonb                  |           | not null |
 create_tstamp   | bigint                 |           |          | EXTRACT(epoch FROM now())::bigint
 last_tstamp     | bigint                 |           |          | EXTRACT(epoch FROM now())::bigint
Indexes:
	"unit_pkey" PRIMARY KEY, btree (id)
Referenced by:
	TABLE "_timescaledb_internal._hyper_5_3_chunk" CONSTRAINT "3_3_fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
	TABLE "device.data" CONSTRAINT "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
	TABLE "device.activity" CONSTRAINT "fk_unit" FOREIGN KEY (unit_id) REFERENCES device.unit(id) ON DELETE CASCADE
*/

/*
exp query :
SELECT DISTINCT c.id, c.customer_id, c.nama, LOWER(c.nama) AS nama_lower
FROM contacts.contact c
LEFT JOIN contacts.contact_detail cd ON c.id = cd.contact_id
JOIN contacts.customer cust ON c.customer_id = cust.id
WHERE cust.organization_id = 2

	  AND (
		  c.nama ILIKE '%' || 'ak' || '%'
		  OR cd.contact_value ILIKE '%' || 'ak' || '%'
		  OR c.data->>'data' ILIKE '%' || 'ak' || '%'
		  OR c.data->>'address' ILIKE '%' || 'ak' || '%'
	  )

ORDER BY nama_lower ASC
LIMIT 10 OFFSET 0;
*/
package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"
	"strings"

	"github.com/jmoiron/sqlx"
)

type DeviceList struct {
	DeviceID           int    `db:"id" json:"device_id"`
	DeviceName         string `db:"name" json:"device_name"`
	DeviceSt           int    `db:"st" json:"device_st"`
	DeviceLastTstamp   int64  `db:"last_tstamp" json:"device_last_tstamp"`
	DeviceCreateTstamp int64  `db:"create_tstamp" json:"device_create_tstamp"`
	DeviceNameLower    string `db:"name_lower" json:"device_name_lower"`
}

func Get_Device_List(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	//logger.Info(referenceId, "INFO - Get_Device_List param: ", param)

	// Validasi parameter pagination
	pageSize, ok := param["page_size"].(float64)
	if !ok || pageSize <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Device_List - Invalid page_size: %v", param["page_size"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	pageNumber, ok := param["page_number"].(float64)
	if !ok || pageNumber < 1 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_Device_List - Invalid page_number: %v", param["page_number"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request"
		return result
	}

	offset := (int(pageNumber) - 1) * int(pageSize)
	var totalData int
	var devices []DeviceList

	// Base query
	baseQuery := `SELECT DISTINCT id, name, st, LOWER(name) AS name_lower, last_tstamp, create_tstamp FROM device.unit WHERE 1=1`
	countQuery := `SELECT COUNT(DISTINCT id) FROM device.unit WHERE 1=1`

	// Filter untuk search dengan LIKE query (case-insensitive)
	if filter, ok := param["filter"].(string); ok && filter != "" {
		//logger.Info(referenceId, fmt.Sprintf("INFO - Applying Filter: %v", filter))
		filterClause := fmt.Sprintf(" AND (name ILIKE '%%%s%%' OR data::text ILIKE '%%%s%%')", filter, filter)
		baseQuery += filterClause
		countQuery += filterClause
	}

	// Filter berdasarkan st
	if st, ok := param["st"].(float64); ok {
		if st == 0 || st == 1 {
			stClause := fmt.Sprintf(" AND st = %d", int(st))
			baseQuery += stClause
			countQuery += stClause
		}
	}

	// Hitung total data setelah filter diterapkan
	err := conn.Get(&totalData, countQuery)
	if err != nil {
		//logger.Error(referenceId, "ERROR - Get_Device_List - Failed to get total data: ", err)
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error"
		return result
	}

	orderBy := "last_tstamp"
	if val, ok := param["order_by"].(string); ok {
		lower := strings.ToLower(val)
		if lower == "last_tstamp" || lower == "create_tstamp" || lower == "name" {
			orderBy = lower
		}
	}

	sortType := "DESC"
	if val, ok := param["sort_type"].(string); ok {
		lower := strings.ToLower(val)
		if lower == "asc" || lower == "desc" {
			sortType = strings.ToUpper(lower)
		}
	}

	// Query utama dengan pagination
	finalQuery := fmt.Sprintf("%s ORDER BY %s %s LIMIT %d OFFSET %d;", baseQuery, orderBy, sortType, int(pageSize), offset)
	//logger.Info(referenceId, "INFO - Get_Device_List - Final Query: ", finalQuery)

	err = conn.Select(&devices, finalQuery)
	if err != nil {
		//logger.Error(referenceId, "ERROR - Query Execution Failed: ", err)
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal server error"
		return result
	}

	totalPage := (totalData + int(pageSize) - 1) / int(pageSize)
	// //logger.Info(referenceId, "INFO - Get_Device_List total data : ", totalData)
	// //logger.Info(referenceId, "INFO - Get_Device_List total page : ", totalPage)
	// //logger.Info(referenceId, "INFO - Get_Device_List devices : ", devices)

	// Hitung total halaman berdasarkan total data yang sudah difilter

	result.Payload["devices"] = devices
	result.Payload["total_data"] = totalData
	result.Payload["total_page"] = totalPage
	result.Payload["status"] = "success"
	//logger.Info(referenceId, "INFO - Get_Device_List completed successfully")
	return result
}
