package pubsub

import (
	"context"
	"fmt"
	"monitoring_service/logger"
	"sort"

	"github.com/gorilla/websocket"
)

func (hub *WebSocketHub) AddUserToWebsocket(referenceId string, conn *websocket.Conn, userId int64, username string, role string, deviceId int64) error {
	hub.mu.Lock()

	// Cek apakah userId sudah memiliki koneksi aktif
	if oldConn, exists := hub.UserConn[userId]; exists {
		logger.Warning(referenceId, fmt.Sprintf("WARNING - AddUserToWebsocket - User %s reconnecting. Closing old connection.", username))
		oldConn.Close()
		delete(hub.Users, oldConn)
		delete(hub.UserConn, userId)
	}

	deviceConn, exists := hub.DeviceConn[deviceId]
	if !exists {
		logger.Error(referenceId, fmt.Sprintf("ERROR - AddUserToWebsocket - Device connection not found for device ID: %d", deviceId))
		hub.mu.Unlock() // Release lock early
		return fmt.Errorf("device connection not found")
	}

	logger.Debug(referenceId, fmt.Sprintf("DEBUG - AddUserToWebsocket -  deviceConn found: %v", deviceConn))

	channel := fmt.Sprintf("device:%d", deviceId)

	// Simpan koneksi baru
	hub.Users[conn] = &UserClient{
		UserID:            userId,
		Username:          username,
		Conn:              conn,
		ChannelSubscribed: channel,
	}
	logger.Debug(referenceId, fmt.Sprintf("DEBUG - AddUserToWebsocket - User %d added to WebSocket hub", userId))

	// Simpan referensi userId ke koneksi baru
	hub.UserConn[userId] = conn

	hub.mu.Unlock() // Release lock before subscribing to Redis

	hub.mu.Lock() // Re-acquire lock to update ChannelUsers
	hub.ChannelUsers[channel] = append(hub.ChannelUsers[channel], conn)
	hub.mu.Unlock()

	logger.Info(referenceId, fmt.Sprintf("INFO - AddUserToWebsocket - NEW USER Connected - userId: %d, username: %s, role: %s, channel: %s", userId, username, role, channel))
	logger.Info(referenceId, fmt.Sprintf("INFO - AddUserToWebsocket - Total users connected: %d", len(hub.Users)))

	return nil
}

// RemoveUserFromWebSocket menghapus user dari hub dan unsubscribe dari channel jika ada

func (hub *WebSocketHub) RemoveUserFromWebSocket(referenceId string, conn *websocket.Conn) error {
	hub.mu.Lock()
	user, exists := hub.Users[conn]
	if !exists {
		hub.mu.Unlock()
		logger.Warning(referenceId, "Attempted to remove non-existent user")
		return fmt.Errorf("attempted to remove non-existent user")
	}
	delete(hub.Users, conn)
	delete(hub.UserConn, user.UserID)
	hub.mu.Unlock()

	if user.ChannelSubscribed != "" {
		logger.Debug(referenceId, fmt.Sprintf("Unsubscribing user ID: %d from channel: %s", user.UserID, user.ChannelSubscribed))
		hub.UnsubscribeUserFromChannel(referenceId, conn)

	}
	conn.Close()
	logger.Info(referenceId, fmt.Sprintf("Successfully removed user ID: %d", user.UserID))
	return nil
}

// Subscribe user ke channel Redis
func (hub *WebSocketHub) SubscribeUserToChannel(referenceId string, userConn *websocket.Conn, deviceID string) error {
	hub.mu.Lock()
	userClient, exists := hub.Users[userConn]
	hub.mu.Unlock()

	if !exists {
		logger.Error(referenceId, "User not found, cannot subscribe")
		return fmt.Errorf("user not found")
	}

	if hub.redis == nil {
		logger.Error(referenceId, "Redis client is nil")
		return fmt.Errorf("redis client is not initialized or failed to reconnect")
	}

	channelName := fmt.Sprintf("device:%s", deviceID)
	ctx, cancel := context.WithCancel(context.Background())
	pubSub := hub.redis.Subscribe(ctx, channelName)

	userClient.PubSub = pubSub
	userClient.ChannelSubscribed = deviceID
	userClient.PubSubCancel = cancel

	go func() {
		defer hub.UnsubscribeUserFromChannel(referenceId, userConn)
		ch := pubSub.Channel()
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					logger.Error(referenceId, "Redis channel closed")
					return
				}
				if err := userConn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
					logger.Error(referenceId, fmt.Sprintf("Failed to send message to user %s: %v", userClient.Username, err))
					hub.RemoveUserFromWebSocket(referenceId, userConn)
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	logger.Info(referenceId, fmt.Sprintf("INFO - User %s subscribed to channel: %s", userClient.Username, deviceID))
	return nil
}

// Unsubscribe user dari channel Redis dan hapus langganan di sistem
func (hub *WebSocketHub) UnsubscribeUserFromChannel(referenceId string, userConn *websocket.Conn) error {
	hub.mu.Lock()
	user, exists := hub.Users[userConn]
	hub.mu.Unlock()

	if !exists || user.ChannelSubscribed == "" {
		return fmt.Errorf("user not found or not subscribed to any channel")
	}

	if user.PubSub != nil {
		user.PubSub.Close()
		if user.PubSubCancel != nil {
			user.PubSubCancel()     // Hentikan goroutine
			user.PubSubCancel = nil // Reset cancel function

		}
		user.PubSub = nil
	}

	logger.Info(referenceId, fmt.Sprintf("INFO - User %s unsubscribed from channel: %s", user.Username, user.ChannelSubscribed))

	hub.mu.Lock()
	user.ChannelSubscribed = ""
	hub.mu.Unlock()
	return nil
}

// GetActiveDevices mengembalikan daftar perangkat yang sedang terhubung dengan pagination
func (hub *WebSocketHub) GetActiveDevices(referenceId string, pageNumber int64, pageSize int64) []*DeviceClient {

	hub.mu.Lock()
	defer hub.mu.Unlock()

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

// GetTotalChannelSubscribers mengembalikan jumlah total subscriber untuk device tertentu
func GetTotalChannelSubscribers(referenceId string, deviceID int64) (int64, error) {
	redisClient := GetRedisClient()
	if redisClient == nil {
		return 0, fmt.Errorf("redis client is not initialized or failed to reconnect")
	}

	// Nama channel berdasarkan deviceID
	channelName := fmt.Sprintf("device:%d", deviceID)
	logger.Info(referenceId, fmt.Sprintf("INFO - get total subscribers channel: %s", channelName))

	// Menggunakan perintah Redis PUBSUB NUMSUB
	ctx := context.Background()
	result, err := redisClient.PubSubNumSub(ctx, channelName).Result()
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - Failed to get subscribers for channel: %s, error: %v", channelName, err))
		return 0, err
	}

	// Hasil dari PubSubNumSub adalah map[string]int64, ambil nilai dari channel yang diminta
	totalSubscribers, ok := result[channelName]
	if !ok {
		totalSubscribers = 0
	}

	logger.Info(referenceId, fmt.Sprintf("INFO - Total subscribers for %s: %d", channelName, totalSubscribers))
	return totalSubscribers, nil
}
