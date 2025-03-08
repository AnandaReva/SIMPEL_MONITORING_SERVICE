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
		hub.DisconnectUsersByChannel(referenceId, channel)
	}

	return nil
}

func (hub *WebSocketHub) DisconnectUsersByChannel(referenceId, channel string) error {
	hub.mu.Lock()
	conns, exists := hub.ChannelUsers[channel]
	if !exists {
		hub.mu.Unlock()
		logger.Warning(referenceId, fmt.Sprintf("WARNING - DisconnectUsersByChannel - No users found for channel: %s", channel))
		return errors.New("no users found for channel")
	}
	delete(hub.ChannelUsers, channel) // Hapus channel dari daftar
	hub.mu.Unlock()

	// Unsubscribe dari Redis di luar blok yang terkunci
	if hub.redis != nil {
		pubsub := hub.redis.Subscribe(context.Background(), channel)
		if err := pubsub.Unsubscribe(context.Background(), channel); err != nil {
			logger.Error(referenceId, fmt.Sprintf("ERROR - Failed to unsubscribe from Redis: %v", err))
		}
		_ = pubsub.Close()
	}

	// Tutup semua koneksi WebSocket user
	for _, conn := range conns {
		hub.mu.Lock()
		delete(hub.Users, conn)
		hub.mu.Unlock()
		conn.Close()
	}

	logger.Info(referenceId, fmt.Sprintf("INFO - DisconnectUsersByChannel - Disconnected all users from channel: %s", channel))
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
