package pubsub

import (
	"context"
	"errors"
	"fmt"
	"monitoring_service/logger"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Menambahkan Device Baru
func (hub *WebSocketHub) AddDeviceToWebSocket(referenceID string, conn *websocket.Conn, deviceID int64, deviceName string) error {
	hub.Mu.Lock()

	// Jika sudah ada koneksi sebelumnya, tutup dulu koneksi lama
	if oldConn, exists := hub.DeviceConn[deviceID]; exists {
		logger.Warning(referenceID, fmt.Sprintf("WARNING - AddDeviceToWebSocket - Device %s already connected, closing old connection.", deviceName))
		// Close dulu, biar goroutine lama stop
		oldConn.Close()
		hub.Mu.Unlock()

		// Beri jeda sebentar agar goroutine lama sempat menyadari
		time.Sleep(100 * time.Millisecond)
		_ = hub.RemoveDeviceFromWebSocket(referenceID, oldConn)

		// Re-lock untuk proses pendaftaran baru
		hub.Mu.Lock()
	}

	defer hub.Mu.Unlock()

	if conn == nil {
		errMsg := fmt.Sprintf("ERROR - AddDeviceToWebSocket - WebSocket connection is nil for device: %s", deviceName)
		logger.Error(referenceID, errMsg)
		return errors.New(errMsg)
	}

	device := &DeviceClient{
		DeviceID:         deviceID,
		DeviceName:       deviceName,
		WSConn:           conn,
		ChannelToPublish: fmt.Sprintf("device:%d", deviceID),
	}

	hub.Devices[conn] = device
	hub.DeviceConn[deviceID] = conn

	logger.Info(referenceID, fmt.Sprintf("INFO - AddDeviceToWebSocket - New device connected - DeviceID: %d, DeviceName: %s", deviceID, deviceName))
	return nil
}

func (deviceClient *DeviceClient) SafeWriteJSON(data any) error {
	deviceClient.WriteMutex.Lock()
	defer deviceClient.WriteMutex.Unlock()
	return deviceClient.WSConn.WriteJSON(data)
}

func (hub *WebSocketHub) RemoveDeviceFromWebSocket(referenceId string, conn *websocket.Conn) error {
	hub.Mu.Lock()
	device, exists := hub.Devices[conn]
	if !exists {
		hub.Mu.Unlock()
		logger.Warning(referenceId, "WARNING - RemoveDeviceFromWebSocket - Attempted to remove non-existent device")
		return errors.New("attempted to remove non-existent device")
	}

	// Ambil channel sebelum menghapus device
	channel := device.ChannelToPublish

	// Hapus device dari map sebelum menutup koneksi
	delete(hub.Devices, conn)
	delete(hub.DeviceConn, device.DeviceID)
	hub.Mu.Unlock()

	// Tutup koneksi WebSocket
	conn.Close()

	logger.Info(referenceId, fmt.Sprintf("INFO - RemoveDeviceFromWebSocket - Successfully removed device ID: %d", device.DeviceID))

	// Jika device memiliki channel, putuskan semua user yang subscribe ke channel itu
	if channel != "" {
		hub.UnsubscribeAllUsersFromChannel(referenceId, channel)
	}

	return nil
}

// DisconnectAllUsersFromDevice menutup semua koneksi user yang terhubung ke device tertentu
func (hub *WebSocketHub) UnsubscribeAllUsersFromChannel(referenceId string, channel string) error {
	hub.Mu.Lock()
	userConns, exists := hub.ChannelUsers[channel]
	if !exists {
		hub.Mu.Unlock()
		logger.Warning(referenceId, fmt.Sprintf("WARNING - UnsubscribeAllUsersFromChannel - No users found for channel: %s", channel))
		return errors.New("no users found for this channel")
	}
	logger.Debug(referenceId, fmt.Sprintf("DEBUG - UnsubscribeAllUsersFromChannel - Found %d users for channel: %s", len(userConns), channel))

	// Buat salinan koneksi yang perlu di-unsubscribe
	connsToUnsub := make([]*websocket.Conn, 0, len(userConns))
	for conn := range userConns {
		connsToUnsub = append(connsToUnsub, conn)
	}
	hub.Mu.Unlock()

	// Unsubscribe user satu per satu
	for _, conn := range connsToUnsub {
		if err := hub.UnsubscribeUserFromChannel(referenceId, conn, channel); err != nil {
			logger.Warning(referenceId, fmt.Sprintf("WARNING - UnsubscribeAllUsersFromChannel - Error unsubscribing user: %v", err))
		}
	}

	logger.Info(referenceId, fmt.Sprintf("INFO - UnsubscribeAllUsersFromChannel - Unsubscribed %d users from channel: %s", len(connsToUnsub), channel))
	return nil
}

// Publish data dari device ke Redis

func (hub *WebSocketHub) DevicePublishToChannel(referenceId string, deviceID int64, data string) error {
	redisClient := GetRedisClient()
	if redisClient == nil {
		return fmt.Errorf("redis client is not initialized or failed to reconnect")
	}

	ctx := context.Background()
	channelName := fmt.Sprintf("device:%d", deviceID)

	var err error
	for i := range 3 { // Retry maksimal 3 kali
		err = redisClient.Publish(ctx, channelName, data).Err()
		if err == nil {
			break
		}
		logger.Warning(referenceId, fmt.Sprintf("WARNING - Retrying publish to Redis (%d/3)", i+1))
	}

	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - DevicePublishToChannel - Failed to publish to Redis after retries: %v", err))
		return err
	}

	logger.Info(referenceId, fmt.Sprintf("INFO - DevicePublishToChannel - Successfully published to channel: %s", channelName))
	return nil
}

// PushDataToBuffer menyimpan data ke buffer di Redis
func PushDataToBuffer(ctx context.Context, data string, referenceId string) error {
	redisClient := GetRedisClient()
	if redisClient == nil {
		logger.Error(referenceId, "ERROR - Redis client is nil, cannot push data to buffer")
		return fmt.Errorf("redis client is not initialized or failed to reconnect")
	}
	logger.Info(referenceId, fmt.Sprintf("INFO - Pushing data to buffer: %s", data))

	redisBufferName := "buffer:device_data"

	err := redisClient.RPush(ctx, redisBufferName, data).Err()
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Failed to push data to buffer: %v", err))
		return err
	}

	logger.Info(referenceId, "INFO - Data successfully pushed to buffer with name : ", redisBufferName)
	return nil
}

/*exp newDeviceCredentials:
{
	"name": "new device_name",
	"password" : "new device_password"

}

!!note : noly send updated creadential

exp action map :
1. update =
 {
	"type" : "update",
	"new_device_credentials" : {
		"name" : "new_name" //if provided
		"password " : "new password"		//if provided
	}

}
2. restart =
{
	"type" : "restart"
},
2. deep_sleep =
{
	"type" : "deep_sleep"
}
*/
// GetDeviceAction retrieves the action of a device based on its DeviceID.
func (hub *WebSocketHub) GetDeviceAction(referenceId string, deviceID int64) (map[string]any, error) {
	hub.Mu.Lock()
	defer hub.Mu.Unlock()

	//logger.Debug(referenceId, fmt.Sprintf("DEBUG - GetDeviceAction - Device ID: %d", deviceID))

	if _, exists := hub.DeviceConn[deviceID]; exists {
		for _, client := range hub.Devices {
			if client.DeviceID == deviceID {
				return client.Action, nil
			}
		}
	}
	return nil, fmt.Errorf("device with ID %d not found", deviceID)
}

// SetDeviceAction sets an action for a connected device.
func (hub *WebSocketHub) SetDeviceAction(referenceId string, deviceId int64, newDeviceCredentials map[string]any) error {
	hub.Mu.Lock()
	defer hub.Mu.Unlock()

	logger.Debug(referenceId, fmt.Sprintf("DEBUG - SetDeviceAction - Device ID: %d", deviceId))

	// Cek apakah device terhubung
	if conn, exists := hub.DeviceConn[deviceId]; exists {
		client, ok := hub.Devices[conn]
		if !ok {
			return fmt.Errorf("device client not found in Devices map for device ID %d", deviceId)
		}

		// Susun payload action map
		actionMap := map[string]any{
			"type": "update", // default action type
		}

		// Tambahkan credential baru jika ada
		newCreds := make(map[string]any)
		if newName, ok := newDeviceCredentials["name"].(string); ok && strings.TrimSpace(newName) != "" {
			newCreds["name"] = newName
			logger.Debug(referenceId, fmt.Sprintf("DEBUG - SetDeviceAction - new name: %s", newName))
		}
		if newPwd, ok := newDeviceCredentials["password"].(string); ok && strings.TrimSpace(newPwd) != "" {
			newCreds["password"] = newPwd
			logger.Debug(referenceId, fmt.Sprintf("DEBUG - SetDeviceAction - new password: %s", newPwd))
		}
		if len(newCreds) > 0 {
			actionMap["new_device_credentials"] = newCreds
		}

		// Set action map ke client
		client.Action = actionMap
		return nil
	}

	return fmt.Errorf("device with ID %d not found", deviceId)
}

// GetActiveDevices mengembalikan daftar perangkat yang sedang terhubung dengan pagination
func (hub *WebSocketHub) GetActiveDevices(referenceId string, pageNumber int64, pageSize int64) []*DeviceClient {

	hub.Mu.Lock()
	defer hub.Mu.Unlock()

	logger.Info(referenceId, fmt.Sprintf("INFO - GetActiveDevices , page_number: %d, page_size: %d", pageNumber, pageSize))

	// Konversi map ke slice
	devices := make([]*DeviceClient, 0, len(hub.Devices))
	for _, device := range hub.Devices {
		devices = append(devices, device)
	}

	// Sorting berdasarkan DeviceID agar konsisten
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].DeviceID < devices[j].DeviceID
	})

	// Pagination menggunakan offset seperti SQL
	offset := (pageNumber - 1) * pageSize
	if offset >= int64(len(devices)) {
		return []*DeviceClient{} // Jika offset melebihi jumlah data
	}

	endIndex := offset + pageSize
	if endIndex > int64(len(devices)) {
		endIndex = int64(len(devices))
	}

	return devices[offset:endIndex]
}
