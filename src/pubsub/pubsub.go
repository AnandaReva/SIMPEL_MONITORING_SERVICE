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

	Server Tidak Perlu Looping ke Subscribers – Redis menangani distribusi pesan.
*/

// WebSocketHub menyimpan koneksi WebSocket dan integrasi Redis
type WebSocketHub struct {
	mu      sync.Mutex
	Devices map[*websocket.Conn]*DeviceClient
	Users   map[*websocket.Conn]*UserClient
	redis   *redis.Client
}

type DeviceClient struct {
	DeviceID   int64
	DeviceName string
	Conn       *websocket.Conn
}

type UserClient struct {
	UserID   int64
	Username string
	Role     string
	Conn     *websocket.Conn
}

var (
	RedisClient *redis.Client
	redisMu     sync.Mutex
)

var wsHub *WebSocketHub
var wsHubOnce sync.Once

// InitRedisConn menginisialisasi Redis client
func InitRedisConn(host string, pass string, db int) error {
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

	_, err := client.Ping(context.Background()).Result()
	if err != nil {
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

	_, err := RedisClient.Ping(context.Background()).Result()
	if err != nil {
		logger.Error("REDIS", "ERROR - Redis connection lost. Closing client...")
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
		}
	})
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

/* func (hub *WebSocketHub) GetActiveDevices(reference_id string, pageNumber int64, pageSize int64) []*DeviceClient {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	logStr := fmt.Sprintf("GetActiveDevices ,  page_number: %d, page_size: %d ", pageNumber, pageSize)

	logger.Info(reference_id, "INFO - ", logStr)

	devices := make([]*DeviceClient, 0, len(hub.Devices))
	for _, device := range hub.Devices {
		devices = append(devices, device)
	}

	// Sorting berdasarkan DeviceID untuk memastikan urutan
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].DeviceID < devices[j].DeviceID
	})

	// Implementasi pagination
	startIndex := -1
	for i, device := range devices {
		if device.DeviceID >= pageNumber {
			startIndex = i
			break
		}
	}

	if startIndex == -1 {
		return []*DeviceClient{} // Jika tidak ada device dengan DeviceID >= pageNumber
	}

	endIndex := startIndex + int(pageSize)
	if endIndex > len(devices) {
		endIndex = len(devices)
	}

	return devices[startIndex:endIndex]
}
 */
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

// Menambahkan Device Baru
func (hub *WebSocketHub) AddDevice(reference_id string, conn *websocket.Conn, deviceID int64, deviceName string) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	hub.Devices[conn] = &DeviceClient{
		DeviceID:   deviceID,
		DeviceName: deviceName,
		Conn:       conn,
	}

	logger.Info(reference_id, fmt.Sprintf("INFO - NEW DEVICE Connected - DeviceID: %d, DeviceName: %s", deviceID, deviceName))
	logger.Info(reference_id, fmt.Sprintf("INFO - Total devices connected: %d", len(hub.Devices)))
}

// Menghapus Device
func (hub *WebSocketHub) RemoveDevice(reference_id string, conn *websocket.Conn) {
	hub.mu.Lock()
	device, exists := hub.Devices[conn]
	if exists {
		delete(hub.Devices, conn)
	}
	hub.mu.Unlock()

	if exists {
		logger.Info(reference_id, fmt.Sprintf("INFO - REMOVING DEVICE - DeviceID: %d, DeviceName: %s", device.DeviceID, device.DeviceName))
		conn.Close()
		logger.Info(reference_id, "INFO - REMOVING DEVICE SUCCESSFUL")
	}

	logger.Info(reference_id, fmt.Sprintf("INFO - Total devices connected: %d", len(hub.Devices)))
}

func (hub *WebSocketHub) AddUser(reference_id string, conn *websocket.Conn, userId int64, username string, role string) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	hub.Users[conn] = &UserClient{
		UserID:   userId,
		Username: username,
		Role:     role,
		Conn:     conn,
	}

	logger.Info(reference_id, fmt.Sprintf("INFO - NEW USER Connected - userId: %d, username: %s, role: %s", userId, username, role))
	logger.Info(reference_id, fmt.Sprintf("INFO - Total devices connected: %d", len(hub.Devices)))
}

// Menghapus User
func (hub *WebSocketHub) RemoveUser(reference_id string, conn *websocket.Conn) {
	hub.mu.Lock()
	user, exists := hub.Users[conn]
	if exists {
		delete(hub.Users, conn)
	}
	hub.mu.Unlock()

	if exists {
		logger.Info(reference_id, fmt.Sprintf("INFO - REMOVE USER - ID: %d, Username: %s", user.UserID, user.Username))
		conn.Close()
	}
}

// User subscribe ke channel Redis

func (hub *WebSocketHub) SubscribeUserToChannel(reference_id string, userConn *websocket.Conn, deviceID string) {
	ctx := context.Background()
	channelName := fmt.Sprintf("device:%s", deviceID)
	sub := hub.redis.Subscribe(ctx, channelName)

	logger.Info(reference_id, fmt.Sprintf("INFO - User subscribed to channel: %s", channelName))

	go func() {
		defer sub.Close()
		ch := sub.Channel()

		for msg := range ch {
			err := userConn.WriteMessage(websocket.TextMessage, []byte(msg.Payload))
			if err != nil {
				logger.Error(reference_id, fmt.Sprintf("ERROR - Failed to send message to user: %v", err))
				hub.RemoveUser(reference_id, userConn)
				return
			}
		}
	}()
}

// DevicePublishToChannel mengirimkan data ke channel Redis
func (hub *WebSocketHub) DevicePublishToChannel(reference_id string, deviceID int64, data string) error {
	redisClient := GetRedisClient()
	if redisClient == nil {
		return fmt.Errorf("redis client is not initialized or failed to reconnect")
	}

	ctx := context.Background()
	channelName := fmt.Sprintf("device:%d", deviceID)
	logger.Info(reference_id, fmt.Sprintf("INFO - Publishing to channel: %s", channelName))

	// Publikasikan data ke channel Redis
	err := redisClient.Publish(ctx, channelName, data).Err()
	if err != nil {
		logger.Error(reference_id, fmt.Sprintf("ERROR - Failed to publish to Redis: %v", err))
		return err
	}

	return nil
}

// PushDataToBuffer menyimpan data ke buffer di Redis
func PushDataToBuffer(ctx context.Context, data string, reference_id string) error {
	redisClient := GetRedisClient()
	if redisClient == nil {
		return fmt.Errorf("redis client is not initialized or failed to reconnect")
	}

	logger.Info(reference_id, "INFO - Pushing data to buffer")

	// Simpan data ke buffer dengan RPUSH (FIFO)
	err := redisClient.RPush(ctx, "buffer:device_data", data).Err()
	if err != nil {
		logger.Error(reference_id, fmt.Sprintf("ERROR - Failed to push data to buffer: %v", err))
		return err
	}

	logger.Info(reference_id, "INFO - Data successfully pushed to buffer")
	return nil
}
