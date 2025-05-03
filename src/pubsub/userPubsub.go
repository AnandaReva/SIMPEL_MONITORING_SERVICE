package pubsub

import (
	"context"
	"fmt"
	"monitoring_service/logger"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
)

func (hub *WebSocketHub) AddUserToWebSocket(
	referenceId string,
	conn *websocket.Conn,
	userId int64,
	username, role string,

) error {
	hub.Mu.Lock()
	defer hub.Mu.Unlock()

	// Cek apakah userId sudah memiliki koneksi aktif
	if oldConn, exists := hub.UserConn[userId]; exists {
		logger.Warning(referenceId, fmt.Sprintf("User %s reconnecting", username))
		// Close dulu, lalu baru remove (agar reader-routine lama berhenti duluan)
		oldConn.Close()
		time.Sleep(100 * time.Millisecond) // (opsional) beri waktu reader-routine keluar
		_ = hub.RemoveUserFromWebSocket(referenceId, oldConn, userId)
	}

	hub.Users[conn] = &UserClient{
		UserID:             userId,
		Username:           username,
		Role:               role,
		Conn:               conn,
		ChannelsSubscribed: make(map[string]bool), // Awalnya kosong
		PubSubs:            make(map[string]*redis.PubSub),
		PubSubCancels:      make(map[string]context.CancelFunc),
	}

	logger.Info(referenceId, fmt.Sprintf("INFO - AddUserToWebsocket - NEW USER Connected - userId: %d, username: %s, role: %s ", userId, username, role))
	logger.Info(referenceId, fmt.Sprintf("INFO - AddUserToWebsocket - Total users connected: %d", len(hub.Users)))

	return nil
}

// RemoveUserFromWebSocket menghapus user dari hub dan unsubscribe dari channel jika ada
func (hub *WebSocketHub) RemoveUserFromWebSocket(referenceId string, wsConn *websocket.Conn, userID int64) error {
	hub.Mu.Lock()
	defer hub.Mu.Unlock()

	// Tentukan connection yang akan digunakan
	var ws *websocket.Conn
	var user *UserClient
	var exists bool

	// Menentukan koneksi yang digunakan, berdasarkan parameter conn atau userID
	if wsConn != nil {
		ws = wsConn
	} else if userID != 0 {
		ws, exists = hub.UserConn[userID]
		if !exists {
			logger.Warning(referenceId, fmt.Sprintf("Attempted to remove non-existent user ID: %d", userID))
			return fmt.Errorf("user not found by userID: %d", userID)
		}
	} else {
		return fmt.Errorf("no conn or userID provided")
	}

	// Ambil data user dari ws
	user, exists = hub.Users[ws]
	if !exists {
		logger.Warning(referenceId, fmt.Sprintf("Attempted to remove non-existent user for conn or userID: %d", userID))
		return fmt.Errorf("user not found for given conn or userID")
	}

	// Hapus dari UserConn dan ChannelUsers
	delete(hub.UserConn, userID)
	for channel := range user.ChannelsSubscribed {
		delete(hub.ChannelUsers[channel], wsConn)
		if len(hub.ChannelUsers[channel]) == 0 {
			delete(hub.ChannelUsers, channel)
		}
	}

	// Cancel semua context Redis subscriber
	for channel, cancel := range user.PubSubCancels {
		cancel()
		delete(user.PubSubCancels, channel)
	}

	// Jika user tidak punya subscription lagi, hapus entri user sepenuhnya
	if len(user.ChannelsSubscribed) == 0 {
		delete(hub.Users, ws)
		delete(hub.UserConn, user.UserID)
		ws.Close()
		logger.Info(referenceId, fmt.Sprintf("All channels removed; closed connection for user ID: %d", user.UserID))
	}

	delete(hub.Users, ws)
	delete(hub.UserConn, userID)

	return nil
}

// Subscribe user ke channel Redis
func (hub *WebSocketHub) SubscribeUserToChannel(referenceId string, userConn *websocket.Conn, userId int64, deviceID int64, db *sqlx.DB, mutex *sync.Mutex) error {
	hub.Mu.Lock()
	userClient, exists := hub.Users[userConn]
	if !exists {
		hub.Mu.Unlock()
		logger.Error(referenceId, "ERROR - SubscribeUserToChannel - User not found, cannot subscribe")
		return fmt.Errorf("user not found")
	}
	hub.Mu.Unlock()

	// Check if device connection exists
	_, exists = hub.DeviceConn[deviceID]
	if !exists {
		err := fmt.Errorf("device connection not found for device ID: %d", deviceID)
		logger.Error(referenceId, fmt.Sprintf("ERROR - SubscribeUserToChannel - Device connection not found for device ID: %d", deviceID))

		// Change device status to 0
		if err := ChangeDeviceStatus(referenceId, 0, deviceID, db); err != nil {
			logger.Error(referenceId, "ERROR - SubscribeUserToChannel - Failed to change device status:", err)
		}

		return err
	}

	logger.Debug(referenceId, fmt.Sprintf("DEBUG - SubscribeUserToChannel - Device connection found for device ID: %d", deviceID))

	channelName := fmt.Sprintf("device:%d", deviceID)

	// If already subscribed, unsubscribe first
	if userClient.ChannelsSubscribed[channelName] {
		logger.Info(referenceId, fmt.Sprintf("User %s already subscribed to channel %s, unsubscribing first...", userClient.Username, channelName))
		err := hub.UnsubscribeUserFromChannel(referenceId, userConn, channelName)
		if err != nil {
			logger.Warning(referenceId, fmt.Sprintf("Unsubscribe failed before resubscribe: %v", err))
		}
	}

	// Continue with subscribing
	ctx, cancel := context.WithCancel(context.Background())
	pubSub := hub.redis.Subscribe(ctx, channelName)
	if pubSub == nil {
		cancel()
		logger.Error(referenceId, "Failed to subscribe to Redis channel")
		return fmt.Errorf("failed to subscribe to Redis channel")
	}

	hub.Mu.Lock()
	userClient.ChannelsSubscribed[channelName] = true
	userClient.PubSubs[channelName] = pubSub
	userClient.PubSubCancels[channelName] = cancel
	if _, ok := hub.ChannelUsers[channelName]; !ok {
		hub.ChannelUsers[channelName] = make(map[*websocket.Conn]bool)
	}
	hub.ChannelUsers[channelName][userConn] = true
	hub.Mu.Unlock()

	go func() {
		defer hub.UnsubscribeUserFromChannel(referenceId, userConn, channelName)
		ch := pubSub.Channel()
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					logger.Warning(referenceId, "Redis channel closed")
					return
				}
				if err := userClient.SafeWriteJSON(map[string]any{
					"message": msg.Payload,
				}); err != nil {

					logger.Error(referenceId, fmt.Sprintf("Failed to send message to user %s: %v", userClient.Username, err))
					hub.RemoveUserFromWebSocket(referenceId, userConn, userId)
					return
				}
			case <-ctx.Done():
				logger.Info(referenceId, fmt.Sprintf("User %s unsubscribed from Redis", userClient.Username))

				// Send unsubscribe notification to client
				unsubMsg := map[string]any{
					"type":      "unsubscribe",
					"status":    "success",
					"message":   "unsubscribed by server",
					"device_id": deviceID,
					"actor":     "server",
				}

				// Safely write JSON data to the connection
				_ = userClient.SafeWriteJSON(unsubMsg)

				return
			}
		}
	}()

	logger.Info(referenceId, fmt.Sprintf("User %s subscribed to channel: %s", userClient.Username, channelName))
	return nil
}

func (userClient *UserClient) SafeWriteJSON(data any) error {
	userClient.WriteMutex.Lock()
	defer userClient.WriteMutex.Unlock()
	return userClient.Conn.WriteJSON(data)
}

/* func (hub *WebSocketHub) SubscribeUserToChannel(referenceId string, userConn *websocket.Conn, userId int64, deviceID int64, db *sqlx.DB) error {
	hub.Mu.Lock()
	userClient, exists := hub.Users[userConn]
	if !exists {
		hub.Mu.Unlock()
		logger.Error(referenceId, "ERROR - SubscribeUserToChannel - User not found, cannot subscribe")
		return fmt.Errorf("user not found")
	}
	hub.Mu.Unlock()

	//check apakah koneksi device yang dituju exist
	_, exists = hub.DeviceConn[deviceID]
	if !exists {
		err := fmt.Errorf("device connection not found for device ID: %d", deviceID)
		logger.Error(referenceId, fmt.Sprintf("ERROR - SubscribeUserToChannel - Device connection not found for device ID: %d", deviceID))

		// Ubah status device menjadi 0
		if err := ChangeDeviceStatus(referenceId, 0, deviceID, db); err != nil {
			logger.Error(referenceId, "ERROR - SubscribeUserToChannel - Failed to change device status:", err)
		}

		return err
	}

	logger.Error(referenceId, fmt.Sprintf("ERROR - SubscribeUserToChannel - Device connection found for device ID: %d", deviceID))

	channelName := fmt.Sprintf("device:%d", deviceID)

	// Jika sudah subscribe, maka unsubscribe dulu
	if userClient.ChannelsSubscribed[channelName] {
		logger.Info(referenceId, fmt.Sprintf("User %s already subscribed to channel %s, unsubscribing first...", userClient.Username, channelName))
		err := hub.UnsubscribeUserFromChannel(referenceId, userConn, channelName)
		if err != nil {
			logger.Warning(referenceId, fmt.Sprintf("Unsubscribe failed before resubscribe: %v", err))
		}
	}

	// Lanjutkan subscribe
	ctx, cancel := context.WithCancel(context.Background())
	pubSub := hub.redis.Subscribe(ctx, channelName)
	if pubSub == nil {
		cancel()
		logger.Error(referenceId, "Failed to subscribe to Redis channel")
		return fmt.Errorf("failed to subscribe to Redis channel")
	}

	hub.Mu.Lock()
	userClient.ChannelsSubscribed[channelName] = true
	userClient.PubSubs[channelName] = pubSub
	userClient.PubSubCancels[channelName] = cancel
	if _, ok := hub.ChannelUsers[channelName]; !ok {
		hub.ChannelUsers[channelName] = make(map[*websocket.Conn]bool)
	}
	hub.ChannelUsers[channelName][userConn] = true
	hub.Mu.Unlock()

	go func() {
		defer hub.UnsubscribeUserFromChannel(referenceId, userConn, channelName)
		ch := pubSub.Channel()
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					logger.Warning(referenceId, "Redis channel closed")
					return
				}
				if err := userConn.WriteMessage(websocket.TextMessage, []byte(msg.Payload)); err != nil {
					logger.Error(referenceId, fmt.Sprintf("Failed to send message to user %s: %v", userClient.Username, err))
					hub.RemoveUserFromWebSocket(referenceId, userConn, userId)
					return
				}
			case <-ctx.Done():
				logger.Info(referenceId, fmt.Sprintf("User %s unsubscribed from Redis", userClient.Username))

				// Kirim notif ke client kalo udah ke-unsubscribe
				unsubMsg := map[string]any{
					"type":      "unsubscribe",
					"status":    "success",
					"message":   "unsubscribed by server",
					"device_id": deviceID,
					"actor":     "server",
				}

				_ = userConn.WriteJSON(unsubMsg)

				return

			}
		}
	}()

	logger.Info(referenceId, fmt.Sprintf("User %s subscribed to channel: %s", userClient.Username, channelName))
	return nil
} */

func ChangeDeviceStatus(referenceId string, newStatus int8, deviceID int64, db *sqlx.DB) error {

	logger.Debug(referenceId, fmt.Sprintf("DEBUG - ChangeDeviceStatus  - Changing device status to %d for device ID: %d", newStatus, deviceID))

	queryToChangeDeviceStatus := `UPDATE device.unit SET st = $1 WHERE id = $2`

	_, err := db.Exec(queryToChangeDeviceStatus, newStatus, deviceID)
	if err != nil {
		logger.Error(referenceId, fmt.Sprintf("ERROR - ChangeDeviceStatus - Failed to change device status: %v", err))
		return fmt.Errorf("failed to change device status: %v", err)
	}
	logger.Info(referenceId, fmt.Sprintf("INFO - ChangeDeviceStatus - Device status changed to %d for device ID: %d", newStatus, deviceID))

	return nil

}

// Unsubscribe user dari channel Redis dan hapus langganan di sistem
func (hub *WebSocketHub) UnsubscribeUserFromChannel(referenceId string, userConn *websocket.Conn, channelName string) error {
	hub.Mu.Lock()
	user, exists := hub.Users[userConn]
	if !exists {
		hub.Mu.Unlock()
		return fmt.Errorf("user not found")
	}

	if !user.ChannelsSubscribed[channelName] {
		hub.Mu.Unlock()
		return fmt.Errorf("user is not subscribed to channel %s", channelName)
	}

	if pubsub, ok := user.PubSubs[channelName]; ok {
		_ = pubsub.Close()
		delete(user.PubSubs, channelName)
	}
	if cancel, ok := user.PubSubCancels[channelName]; ok {
		cancel()
		delete(user.PubSubCancels, channelName)
	}

	delete(user.ChannelsSubscribed, channelName)
	delete(hub.ChannelUsers[channelName], userConn)
	hub.Mu.Unlock()

	logger.Info(referenceId, fmt.Sprintf("User %s unsubscribed from channel: %s", user.Username, channelName))
	return nil
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
