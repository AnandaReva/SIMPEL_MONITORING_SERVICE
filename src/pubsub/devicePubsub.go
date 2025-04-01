package pubsub

import (
	"context"
	"errors"
	"fmt"
	"monitoring_service/logger"

	"github.com/gorilla/websocket"
)

// Menambahkan Device Baru
func (hub *WebSocketHub) AddDeviceToWebSocket(referenceID string, conn *websocket.Conn, deviceID int64, deviceName string) error {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	// Pastikan tidak ada koneksi lain yang sedang ditambahkan
	if _, exists := hub.DeviceConn[deviceID]; exists {
		logger.Warning(referenceID, fmt.Sprintf("WARNING - AddDeviceToWebSocket - Device %s already connected, closing old connection.", deviceName))
		hub.RemoveDeviceFromWebSocket(referenceID, hub.DeviceConn[deviceID])
	}

	// Validasi koneksi baru
	if conn == nil {
		errMsg := fmt.Sprintf("ERROR - AddDeviceToWebSocket - WebSocket connection is nil for device: %s", deviceName)
		logger.Error(referenceID, errMsg)
		return errors.New(errMsg)
	}

	// Tambahkan device baru ke daftar WebSocket secara atomik
	device := &DeviceClient{
		DeviceID:         deviceID,
		DeviceName:       deviceName,
		Conn:             conn,
		ChannelToPublish: fmt.Sprintf("device:%d", deviceID),
	}

	hub.Devices[conn] = device
	hub.DeviceConn[deviceID] = conn

	logger.Info(referenceID, fmt.Sprintf("INFO - AddDeviceToWebSocket - New device connected - DeviceID: %d, DeviceName: %s", deviceID, deviceName))
	return nil
}

func (hub *WebSocketHub) RemoveDeviceFromWebSocket(referenceId string, conn *websocket.Conn) error {
	hub.mu.Lock()
	device, exists := hub.Devices[conn]
	if !exists {
		hub.mu.Unlock()
		logger.Warning(referenceId, "WARNING - RemoveDeviceFromWebSocket - Attempted to remove non-existent device")
		return errors.New("attempted to remove non-existent device")
	}

	// Ambil channel sebelum menghapus device
	channel := device.ChannelToPublish

	// Hapus device dari map sebelum menutup koneksi
	delete(hub.Devices, conn)
	delete(hub.DeviceConn, device.DeviceID)
	hub.mu.Unlock()

	// Tutup koneksi WebSocket
	conn.Close()

	logger.Info(referenceId, fmt.Sprintf("INFO - RemoveDeviceFromWebSocket - Successfully removed device ID: %d", device.DeviceID))

	// Jika device memiliki channel, putuskan semua user yang subscribe ke channel itu
	if channel != "" {
		hub.DisconnectAllUsersFromDevice(referenceId, device.DeviceID)
	}

	return nil
}

// DisconnectAllUsersFromDevice menutup semua koneksi user yang terhubung ke device tertentu
func (hub *WebSocketHub) DisconnectAllUsersFromDevice(referenceId string, deviceID int64) error {
	channel := fmt.Sprintf("device:%d", deviceID)

	hub.mu.Lock()
	defer hub.mu.Unlock()

	// Dapatkan semua koneksi user yang subscribe ke channel ini
	userConns, exists := hub.ChannelUsers[channel]
	if !exists {
		logger.Warning(referenceId, fmt.Sprintf("WARNING - DisconnectAllUsersFromDevice - No users found for device ID: %d", deviceID))
		return errors.New("no users found for this device")
	}
	logger.Debug(referenceId, fmt.Sprintf("DEBUG - DisconnectAllUsersFromDevice - Found %d users for device ID: %d", len(userConns), deviceID))

	// Hapus channel dari mapping
	delete(hub.ChannelUsers, channel)

	// Tutup semua koneksi user yang terkait
	for _, conn := range userConns {
		if user, ok := hub.Users[conn]; ok {
			// Panggil cancel function jika ada
			if user.PubSubCancel != nil {
				user.PubSubCancel()
			}

			// Tutup PubSub Redis
			if user.PubSub != nil {
				_ = user.PubSub.Close()
			}

			// Hapus dari mapping UserConn
			delete(hub.UserConn, user.UserID)

			// Hapus dari mapping Users
			delete(hub.Users, conn)

			// Tutup koneksi WebSocket
			conn.Close()
		}
	}

	logger.Info(referenceId, fmt.Sprintf("INFO - DisconnectAllUsersFromDevice - Disconnected %d users from device ID: %d", len(userConns), deviceID))
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
	for i := 0; i < 3; i++ { // Retry maksimal 3 kali
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

func (hub *WebSocketHub) SendMessageToDevice(referenceId string, deviceID int64, message string) error {
	hub.mu.Lock()
	conn, exists := hub.DeviceConn[deviceID]
	hub.mu.Unlock()

	if !exists {
		logger.Warning(referenceId, fmt.Sprintf("WARNING - SendMessageToDevice - Device ID %d is not connected", deviceID))
		return errors.New("device is not connected")
	}

	// Kirim data ke perangkat melalui WebSocket
	err := conn.WriteMessage(websocket.TextMessage, []byte(message))
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - SendMessageToDevice - Failed to send data to device %d: %v", deviceID, err))
		return err
	}

	logger.Info(referenceId, fmt.Sprintf("INFO - SendMessageToDevice - Successfully sent data to device %d", deviceID))
	return nil
}
