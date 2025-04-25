package process

import (
	"fmt"
	"monitoring_service/logger"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

// Mapping struct hasil query
type UserList struct {
	UserId       int64  `db:"id" json:"user_id"`
	UserFullName string `db:"full_name" json:"full_name"`
	UserRole     string `db:"role" json:"user_role"`
	UserSt       int    `db:"st" json:"user_st"`
}

func Get_User_List(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Get_User_List param: ", param)

	pageSize, ok := param["page_size"].(float64)
	if !ok || pageSize <= 0 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_User_List - Invalid page_size: %v", param["page_size"]))
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	pageNumber, ok := param["page_number"].(float64)
	if !ok || pageNumber < 1 {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_User_List - Invalid page_number: %v", param["page_number"]))
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request"
		return result
	}

	offset := (int(pageNumber) - 1) * int(pageSize)
	var totalData int
	var users []UserList

	baseQuery := `SELECT id, full_name, role, st FROM sysuser.user WHERE 1=1`
	countQuery := `SELECT COUNT(*) FROM sysuser.user WHERE 1=1`

	if filter, ok := param["filter"].(string); ok && filter != "" {
		logger.Info(referenceId, fmt.Sprintf("INFO - Applying Filter: %v", filter))
		filterClause := fmt.Sprintf(" AND (full_name ILIKE '%%%s%%' OR data::text ILIKE '%%%s%%')", filter, filter)
		baseQuery += filterClause
		countQuery += filterClause
	}

	if st, ok := param["st"].(float64); ok {
		if st == 0 || st == 1 {
			stClause := fmt.Sprintf(" AND st = %d", int(st))
			baseQuery += stClause
			countQuery += stClause
		}
	}

	err := conn.Get(&totalData, countQuery)
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_User_List - Failed to get total data: %v", err))
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal server error"
		return result
	}

	orderBy, ok := param["order_by"].(string)
	if !ok || orderBy == "" {
		orderBy = "id"
	}
	sortType, ok := param["sort_type"].(string)
	if !ok || sortType == "" {
		sortType = "ASC"
	}

	finalQuery := fmt.Sprintf("%s ORDER BY %s %s LIMIT %d OFFSET %d", baseQuery, orderBy, sortType, int(pageSize), offset)
	logger.Info(referenceId, "INFO - Get_User_List - Final Query: ", finalQuery)

	err = conn.Select(&users, finalQuery)
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Get_User_List - Query execution failed: %v", err))
		result.ErrorCode = "500003"
		result.ErrorMessage = "Internal server error"
		return result
	}

	totalPage := (totalData + int(pageSize) - 1) / int(pageSize)

	result.Payload["users"] = users
	result.Payload["total_data"] = totalData
	result.Payload["total_page"] = totalPage
	result.Payload["status"] = "success"

	logger.Info(referenceId, "INFO - Get_User_List completed successfully")
	return result
}
