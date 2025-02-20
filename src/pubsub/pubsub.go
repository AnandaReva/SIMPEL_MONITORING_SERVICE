package pubsub

import (
	"context"
	"fmt"
	"monitoring_service/logger"
	"sort"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

/*
	!!!NOTE : Terdapat 2 entiti client yang terhubung ke websocket
	1. Device IoT -> mengirim data message sensor ke Redis
	2. User -> Menerima data message dari Redis melalui WebSocket



	each device can only publish to one channel at a time
	each user can only subscribe to one channel at a time

	Server Tidak Perlu Looping ke Subscribers – Redis menangani distribusi pesan.
*/

// WebSocketHub menyimpan koneksi WebSocket dan integrasi Redis
type WebSocketHub struct {
	mu         sync.Mutex
	Devices    map[*websocket.Conn]*DeviceClient
	Users      map[*websocket.Conn]*UserClient
	UserConn   map[int64]*websocket.Conn
	DeviceConn map[int64]*websocket.Conn // Mapping langsung deviceID ke WebSocket connection
	redis      *redis.Client
}

type DeviceClient struct {
	DeviceID         int64
	DeviceName       string
	Conn             *websocket.Conn
	ChannelToPublish string
	PubSub           *redis.PubSub // Menyimpan referensi PubSub untuk menghindari kebocoran
}

type UserClient struct {
	UserID            int64
	Username          string
	Role              string
	Conn              *websocket.Conn
	ChannelSubscribed string
	PubSub            *redis.PubSub // Menyimpan referensi PubSub untuk menghindari kebocoran
}

var (
	RedisClient *redis.Client
	redisMu     sync.Mutex
	wsHub       *WebSocketHub
	wsHubOnce   sync.Once
)

// InitRedisConn menginisialisasi Redis client
func InitRedisConn(host, pass string, db int) error {
	redisMu.Lock()
	defer redisMu.Unlock()

	if RedisClient != nil {
		return nil
	}

	client := redis.NewClient(&redis.Options{
		Addr:     host,
		Password: pass,
		DB:       db,
	})

	if _, err := client.Ping(context.Background()).Result(); err != nil {
		logger.Error("REDIS", fmt.Sprintf("ERROR - Redis connection failed: %v", err))
		client.Close()
		return err
	}

	RedisClient = client
	logger.Info("REDIS", "INFO - Successfully connected to Redis")
	return nil
}

// GetRedisClient memastikan Redis client aktif
func GetRedisClient() *redis.Client {
	redisMu.Lock()
	defer redisMu.Unlock()

	if RedisClient == nil {
		logger.Error("REDIS", "ERROR - Redis client is not initialized")
		return nil
	}

	if _, err := RedisClient.Ping(context.Background()).Result(); err != nil {
		logger.Error("REDIS", "ERROR - Redis connection lost. Reconnecting...")
		RedisClient.Close()
		RedisClient = nil
	}

	return RedisClient
}

// GetWebSocketHub memastikan hanya ada satu instance WebSocketHub
func GetWebSocketHub(reference_id string) (*WebSocketHub, error) {
	var err error
	wsHubOnce.Do(func() {
		wsHub, err = NewWebSocketHub(reference_id)
		if err != nil {
			wsHub = nil
			logger.Error(reference_id, fmt.Sprintf("ERROR - Failed to initialize WebSocketHub: %v", err))
		}
	})
	if err != nil {
		logger.Error(reference_id, "ERROR - WebSocketHub instance is nil after initialization")
	}
	return wsHub, err
}

// Inisialisasi WebSocketHub dengan Redis
func NewWebSocketHub(reference_id string) (*WebSocketHub, error) {
	redisClient := GetRedisClient()
	if redisClient == nil {
		return nil, fmt.Errorf("failed to initialize WebSocketHub: redis client is nil")
	}

	hub := &WebSocketHub{
		Devices: make(map[*websocket.Conn]*DeviceClient),
		Users:   make(map[*websocket.Conn]*UserClient),
		redis:   redisClient,
	}

	logger.Info(reference_id, "INFO - New WebSocketHub initialized with Redis")
	return hub, nil
}

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

// Publish data dari device ke Redis
func (hub *WebSocketHub) DevicePublishToChannel(reference_id string, deviceID int64, data string) error {
	redisClient := GetRedisClient()
	if redisClient == nil {
		return fmt.Errorf("redis client is not initialized or failed to reconnect")
	}

	// Cek apakah device masih terhubung sebelum publish
	hub.mu.Lock()
	conn, exists := hub.DeviceConn[deviceID]
	hub.mu.Unlock()

	if !exists {
		logger.Warning(reference_id, fmt.Sprintf("WARNING - Attempting to publish data from disconnected device ID: %d", deviceID))
		return fmt.Errorf("device ID %d is not connected", deviceID)
	}

	ctx := context.Background()
	channelName := fmt.Sprintf("device:%d", deviceID)
	logger.Info(reference_id, fmt.Sprintf("INFO - Publishing to channel: %s", channelName))

	if err := redisClient.Publish(ctx, channelName, data).Err(); err != nil {
		logger.Error(reference_id, fmt.Sprintf("ERROR - Failed to publish to Redis: %v", err))
		return err
	}

	// Kirim langsung ke WebSocket client jika ada
	if err := conn.WriteMessage(websocket.TextMessage, []byte(data)); err != nil {
		logger.Error(reference_id, fmt.Sprintf("ERROR - Failed to send message to device ID %d: %v", deviceID, err))
		hub.RemoveDeviceFromWebSocket(reference_id, conn)
		return err
	}

	return nil
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

// PushDataToBuffer menyimpan data ke buffer di Redis
func PushDataToBuffer(ctx context.Context, data string, reference_id string) error {
	redisClient := GetRedisClient()
	if redisClient == nil {
		logger.Error(reference_id, "ERROR - Redis client is nil, cannot push data to buffer")
		return fmt.Errorf("redis client is not initialized or failed to reconnect")
	}

	logger.Info(reference_id, fmt.Sprintf("INFO - Pushing data to buffer: %s", data))

	err := redisClient.RPush(ctx, "buffer:device_data", data).Err()
	if err != nil {
		logger.Error(reference_id, fmt.Sprintf("ERROR - Failed to push data to buffer: %v", err))
		return err
	}

	logger.Info(reference_id, "INFO - Data successfully pushed to buffer")
	return nil
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
