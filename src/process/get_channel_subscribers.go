package process

import (
	"monitoring_service/logger"
	pubsub "monitoring_service/pubsub"
	"monitoring_service/utils"

	"github.com/jmoiron/sqlx"
)

func Get_Channel_Subscribers(referenceId string, conn *sqlx.DB, userID int64, role string, param map[string]any) utils.ResultFormat {
	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	// Validasi parameter pagination
	deviceId, ok := param["device_id"].(int64)
	if !ok || deviceId <= 0 {
		logger.Error(referenceId, "ERROR - Get_Channel_Subscribers - Invalid page_size: ", deviceId)
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid device id"
		return result
	}

	// Ambil total perangkat yang terhubung
	totalSubscribers, err := pubsub.GetTotalChannelSubscribers(referenceId, deviceId)
	if err != nil {
		logger.Error(referenceId, "ERROR - Get_Channel_Subscribers err:  ", err)
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal server error"
		return result

	}

	// Format hasil ke dalam payload
	result.Payload["total_data"] = totalSubscribers
	return result
}
