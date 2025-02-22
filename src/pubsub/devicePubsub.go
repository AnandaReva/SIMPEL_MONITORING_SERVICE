package pubsub

import (
	"context"
	"fmt"
	"monitoring_service/logger"

	"github.com/gorilla/websocket"
)

// Menambahkan Device Baru
func (hub *WebSocketHub) AddDeviceToWebSocket(reference_id string, conn *websocket.Conn, deviceID int64, deviceName string) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	// Cek apakah deviceID sudah memiliki koneksi aktif
	if oldConn, exists := hub.DeviceConn[deviceID]; exists {
		logger.Warning(reference_id, fmt.Sprintf("WARNING - Device %s reconnecting. Closing old connection.", deviceName))
		oldConn.Close()
		delete(hub.Devices, oldConn)
		delete(hub.DeviceConn, deviceID)
	}

	// Tambahkan device baru ke daftar WebSocket
	hub.Devices[conn] = &DeviceClient{
		DeviceID:   deviceID,
		DeviceName: deviceName,
		Conn:       conn,
	}

	// Simpan referensi deviceID ke koneksi baru
	hub.DeviceConn[deviceID] = conn

	logger.Info(reference_id, fmt.Sprintf("INFO - New device connected - DeviceID: %d, DeviceName: %s", deviceID, deviceName))
	logger.Info(reference_id, fmt.Sprintf("INFO - Total devices connected: %d", len(hub.Devices)))
}

// Menghapus Device
func (hub *WebSocketHub) RemoveDeviceFromWebSocket(reference_id string, conn *websocket.Conn) {
	hub.mu.Lock()
	device, exists := hub.Devices[conn]
	hub.mu.Unlock()

	if !exists {
		logger.Warning(reference_id, "WARNING - Attempted to remove non-existent device")
		return
	}

	logger.Info(reference_id, fmt.Sprintf("INFO - Removing device ID: %d, DeviceName: %s", device.DeviceID, device.DeviceName))

	hub.mu.Lock()
	delete(hub.Devices, conn)
	delete(hub.DeviceConn, device.DeviceID) // Hapus referensi dari DeviceConn
	hub.mu.Unlock()

	conn.Close()

	logger.Info(reference_id, fmt.Sprintf("INFO - Successfully removed device ID: %d", device.DeviceID))
}

// Publish data dari device ke Redis

func (hub *WebSocketHub) DevicePublishToChannel(reference_id string, deviceID int64, data string) error {
	redisClient := GetRedisClient()
	if redisClient == nil {
		return fmt.Errorf("redis client is not initialized or failed to reconnect")
	}

	ctx := context.Background()
	channelName := fmt.Sprintf("device:%d", deviceID)
	logger.Info(reference_id, fmt.Sprintf("INFO - Publishing to channel: %s", channelName))

	if err := redisClient.Publish(ctx, channelName, data).Err(); err != nil {
		logger.Error(reference_id, fmt.Sprintf("ERROR - Failed to publish to Redis: %v", err))
		return err
	}

	return nil
}

// PushDataToBuffer menyimpan data ke buffer di Redis
func PushDataToBuffer(ctx context.Context, data string, reference_id string) error {
	redisClient := GetRedisClient()
	if redisClient == nil {
		logger.Error(reference_id, "ERROR - Redis client is nil, cannot push data to buffer")
		return fmt.Errorf("redis client is not initialized or failed to reconnect")
	}

	logger.Info(reference_id, fmt.Sprintf("INFO - Pushing data to buffer: %s", data))

	redisBufferName := "buffer:device_data"

	err := redisClient.RPush(ctx, redisBufferName, data).Err()
	if err != nil {
		logger.Error(reference_id, fmt.Sprintf("ERROR - Failed to push data to buffer: %v", err))
		return err
	}

	logger.Info(reference_id, "INFO - Data successfully pushed to buffer with name : ", redisBufferName)
	return nil
}
