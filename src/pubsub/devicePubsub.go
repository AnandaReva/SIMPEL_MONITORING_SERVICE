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

	// Cek apakah deviceID sudah memiliki koneksi aktif
	if oldConn, exists := hub.DeviceConn[deviceID]; exists {
		if oldConn != nil {
			logger.Warning(referenceID, fmt.Sprintf("WARNING - Device %s reconnecting. Closing old connection.", deviceName))
			_ = oldConn.Close() // Pastikan koneksi lama ditutup dengan aman
		}
		delete(hub.Devices, oldConn)
		delete(hub.DeviceConn, deviceID)
	}

	// Validasi koneksi baru sebelum menambahkannya
	if conn == nil {
		errMsg := fmt.Sprintf("ERROR - WebSocket connection is nil for device: %s", deviceName)
		logger.Error(referenceID, errMsg)
		return errors.New(errMsg)
	}

	// Tambahkan device baru ke daftar WebSocket
	hub.Devices[conn] = &DeviceClient{
		DeviceID:   deviceID,
		DeviceName: deviceName,
		Conn:       conn,
	}

	// Simpan referensi deviceID ke koneksi baru
	hub.DeviceConn[deviceID] = conn

	logger.Info(referenceID, fmt.Sprintf("INFO - New device connected - DeviceID: %d, DeviceName: %s", deviceID, deviceName))
	logger.Info(referenceID, fmt.Sprintf("INFO - Total devices connected: %d", len(hub.Devices)))

	return nil
}


// Menghapus Device
func (hub *WebSocketHub) RemoveDeviceFromWebSocket(referenceId string, conn *websocket.Conn) error {
	hub.mu.Lock()
	device, exists := hub.Devices[conn]
	hub.mu.Unlock()

	if !exists {
		logger.Warning(referenceId, "WARNING - Attempted to remove non-existent device")
		return errors.New("attempted to remove non-existent device")
	}

	logger.Info(referenceId, fmt.Sprintf("INFO - Removing device ID: %d, DeviceName: %s", device.DeviceID, device.DeviceName))

	hub.mu.Lock()
	delete(hub.Devices, conn)
	delete(hub.DeviceConn, device.DeviceID) // Hapus referensi dari DeviceConn
	hub.mu.Unlock()

	conn.Close()

	logger.Info(referenceId, fmt.Sprintf("INFO - Successfully removed device ID: %d", device.DeviceID))
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
	logger.Info(referenceId, fmt.Sprintf("INFO - Publishing to channel: %s", channelName))

	if err := redisClient.Publish(ctx, channelName, data).Err(); err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Failed to publish to Redis: %v", err))
		return err
	}

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
