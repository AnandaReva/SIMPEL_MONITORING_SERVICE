package pubsub

import (
	"context"
	"fmt"
	"monitoring_service/logger"
	"sort"

	"github.com/gorilla/websocket"
)

func (hub *WebSocketHub) AddUserToWebsocket(reference_id string, conn *websocket.Conn, userId int64, username string, role string) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	// Cek apakah userId sudah memiliki koneksi aktif
	if oldConn, exists := hub.UserConn[userId]; exists {
		logger.Warning(reference_id, fmt.Sprintf("WARNING - User %s reconnecting. Closing old connection.", username))
		oldConn.Close()
		delete(hub.Users, oldConn)
		delete(hub.UserConn, userId)
	}

	// Tambahkan user baru ke daftar WebSocket
	hub.Users[conn] = &UserClient{
		UserID:   userId,
		Username: username,
		Role:     role,
		Conn:     conn,
	}

	// Simpan referensi userId ke koneksi baru
	hub.UserConn[userId] = conn

	logger.Info(reference_id, fmt.Sprintf("INFO - NEW USER Connected - userId: %d, username: %s, role: %s", userId, username, role))
	logger.Info(reference_id, fmt.Sprintf("INFO - Total users connected: %d", len(hub.Users)))
}

// RemoveUserFromWebSocket menghapus user dari hub dan unsubscribe dari channel jika ada
func (hub *WebSocketHub) RemoveUserFromWebSocket(reference_id string, conn *websocket.Conn) {
	hub.mu.Lock()
	user, exists := hub.Users[conn]
	hub.mu.Unlock()

	if !exists {
		logger.Warning(reference_id, "WARNING - Attempted to remove non-existent user")
		return
	}

	logger.Info(reference_id, fmt.Sprintf("INFO - Removing user ID: %d, Username: %s", user.UserID, user.Username))

	if user.ChannelSubscribed != "" {
		hub.UnsubscribeUserFromChannel(reference_id, conn)
	}

	hub.mu.Lock()
	delete(hub.Users, conn)
	delete(hub.UserConn, user.UserID) // Hapus referensi dari UserConn
	hub.mu.Unlock()

	conn.Close()

	logger.Info(reference_id, fmt.Sprintf("INFO - Successfully removed user ID: %d", user.UserID))
}

// Subscribe user ke channel Redis
func (hub *WebSocketHub) SubscribeUserToChannel(reference_id string, userConn *websocket.Conn, deviceID string) {
	hub.mu.Lock()
	userClient, exists := hub.Users[userConn]
	hub.mu.Unlock()

	if !exists {
		logger.Error(reference_id, "ERROR - User not found, cannot subscribe")
		return
	}

	if userClient.PubSub != nil {
		logger.Info(reference_id, fmt.Sprintf("INFO - User %s is already subscribed. Unsubscribing first.", userClient.Username))
		_ = userClient.PubSub.Close() // Tutup subscription sebelumnya
	}

	channelName := fmt.Sprintf("device:%s", deviceID)
	ctx := context.Background()
	userClient.PubSub = hub.redis.Subscribe(ctx, channelName)
	userClient.ChannelSubscribed = deviceID

	logger.Info(reference_id, fmt.Sprintf("INFO - User %s subscribed to channel: %s", userClient.Username, channelName))

	go func() {
		defer func() {
			hub.UnsubscribeUserFromChannel(reference_id, userConn)
		}()

		ch := userClient.PubSub.Channel()
		for msg := range ch {
			if err := userConn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
				logger.Error(reference_id, fmt.Sprintf("ERROR - Failed to send message to user %s: %v", userClient.Username, err))
				hub.RemoveUserFromWebSocket(reference_id, userConn)
				return
			}
		}
	}()
}

// Unsubscribe user dari channel Redis dan hapus langganan di sistem
func (hub *WebSocketHub) UnsubscribeUserFromChannel(reference_id string, userConn *websocket.Conn) {
	hub.mu.Lock()
	user, exists := hub.Users[userConn]
	hub.mu.Unlock()

	if !exists || user.ChannelSubscribed == "" {
		return
	}

	if user.PubSub != nil {
		_ = user.PubSub.Close() // Pastikan koneksi PubSub ditutup
		user.PubSub = nil
	}

	logger.Info(reference_id, fmt.Sprintf("INFO - User %s unsubscribed from channel: %s", user.Username, user.ChannelSubscribed))

	hub.mu.Lock()
	user.ChannelSubscribed = ""
	hub.mu.Unlock()
}

// GetActiveDevices mengembalikan daftar perangkat yang sedang terhubung dengan pagination
func (hub *WebSocketHub) GetActiveDevices(reference_id string, pageNumber int64, pageSize int64) []*DeviceClient {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	logger.Info(reference_id, fmt.Sprintf("INFO - GetActiveDevices , page_number: %d, page_size: %d", pageNumber, pageSize))

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
func GetTotalChannelSubscribers(reference_id string, deviceID int64) (int64, error) {
	redisClient := GetRedisClient()
	if redisClient == nil {
		return 0, fmt.Errorf("redis client is not initialized or failed to reconnect")
	}

	// Nama channel berdasarkan deviceID
	channelName := fmt.Sprintf("device:%d", deviceID)
	logger.Info(reference_id, fmt.Sprintf("INFO - get total subscribers channel: %s", channelName))

	// Menggunakan perintah Redis PUBSUB NUMSUB
	ctx := context.Background()
	result, err := redisClient.PubSubNumSub(ctx, channelName).Result()
	if err != nil {
		logger.Error(reference_id, fmt.Sprintf("ERROR - Failed to get subscribers for channel: %s, error: %v", channelName, err))
		return 0, err
	}

	// Hasil dari PubSubNumSub adalah map[string]int64, ambil nilai dari channel yang diminta
	totalSubscribers, ok := result[channelName]
	if !ok {
		totalSubscribers = 0
	}

	logger.Info(reference_id, fmt.Sprintf("INFO - Total subscribers for %s: %d", channelName, totalSubscribers))
	return totalSubscribers, nil
}
