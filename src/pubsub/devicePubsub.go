package pubsub

import (
	"context"
	"errors"
	"fmt"
	"monitoring_service/logger"
	"strconv"

	"github.com/gorilla/websocket"
)

// Menambahkan Device Baru
func (hub *WebSocketHub) AddDeviceToWebSocket(referenceID string, conn *websocket.Conn, deviceID int64, deviceName string) error {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	// Cek apakah deviceID sudah memiliki koneksi aktif
	if oldConn, exists := hub.DeviceConn[deviceID]; exists {
		if oldConn != nil {
			logger.Warning(referenceID, fmt.Sprintf("WARNING - AddDeviceToWebSocket - Device %s reconnecting. Closing old connection.", deviceName))
			_ = oldConn.Close() // Pastikan koneksi lama ditutup dengan aman
		}
		delete(hub.Devices, oldConn)
		delete(hub.DeviceConn, deviceID)
	}

	// Validasi koneksi baru sebelum menambahkannya
	if conn == nil {
		errMsg := fmt.Sprintf("ERROR - AddDeviceToWebSocket - WebSocket connection is nil for device: %s", deviceName)
		logger.Error(referenceID, errMsg)
		return errors.New(errMsg)
	}

	// Tambahkan device baru ke daftar WebSocket
	hub.Devices[conn] = &DeviceClient{
		DeviceID:         deviceID,
		DeviceName:       deviceName,
		Conn:             conn,
		ChannelToPublish: fmt.Sprintf("device:%d", deviceID), // Set channel untuk device
	}

	// Simpan referensi deviceID ke koneksi baru
	hub.DeviceConn[deviceID] = conn

	logger.Info(referenceID, fmt.Sprintf("INFO - AddDeviceToWebSocket - New device connected - DeviceID: %d, DeviceName: %s", deviceID, deviceName))
	logger.Info(referenceID, fmt.Sprintf("INFO - AddDeviceToWebSocket - Total devices connected: %d", len(hub.Devices)))

	return nil
}
func (hub *WebSocketHub) RemoveDeviceFromWebSocket(referenceId string, conn *websocket.Conn) error {
	hub.mu.Lock()
	device, exists := hub.Devices[conn]
	hub.mu.Unlock()

	if !exists {
		logger.Warning(referenceId, "WARNING - RemoveDeviceFromWebSocket- Attempted to remove non-existent device")
		return errors.New("attempted to remove non-existent device")
	}

	logger.Info(referenceId, fmt.Sprintf("INFO - RemoveDeviceFromWebSocket - Removing device ID: %d, DeviceName: %s", device.DeviceID, device.DeviceName))

	// Ambil channel yang digunakan oleh device ini sebelum menghapus device
	channel := device.ChannelToPublish

	hub.mu.Lock()
	delete(hub.Devices, conn)
	delete(hub.DeviceConn, device.DeviceID) // Hapus referensi dari DeviceConn
	hub.mu.Unlock()

	conn.Close()

	logger.Info(referenceId, fmt.Sprintf("INFO - - RemoveDeviceFromWebSocket - Successfully removed device ID: %d", device.DeviceID))

	// Jika device memiliki channel untuk publish, putuskan semua user yang subscribe ke channel itu
	if channel != "" {
		logger.Info(referenceId, fmt.Sprintf("INFO - RemoveDeviceFromWebSocket - Disconnecting users subscribed to channel: %s", channel))
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
	delete(hub.ChannelUsers, channel) // Hapus channel dari daftar setelah disconnect
	hub.mu.Unlock()

	// Unsubscribe Redis (jika ada PubSub yang terhubung)
	if hub.redis != nil {
		pubsub := hub.redis.Subscribe(context.Background(), channel)
		_ = pubsub.Unsubscribe(context.Background(), channel)
		_ = pubsub.Close()
	}

	// Tutup semua koneksi WebSocket user di channel ini
	for _, conn := range conns {
		userID := "unknown" // Default kalau tidak ada info user
		if u, ok := hub.Users[conn]; ok {
			userID = strconv.FormatInt(u.UserID, 10) // Sesuaikan dengan field user ID
		}

		logger.Info(referenceId, fmt.Sprintf("INFO - DisconnectUsersByChannel - Disconnecting user: %s from channel: %s", userID, channel))

		conn.Close()
		hub.mu.Lock()
		delete(hub.Users, conn)
		hub.mu.Unlock()
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
	logger.Info(referenceId, fmt.Sprintf("INFO - DevicePublishToChannel - Publishing to channel: %s", channelName))

	if err := redisClient.Publish(ctx, channelName, data).Err(); err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - DevicePublishToChannel - Failed to publish to Redis: %v", err))
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
