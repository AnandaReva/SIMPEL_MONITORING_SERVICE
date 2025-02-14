package pubsub

import (
	"context"
	"fmt"
	"monitoring_service/logger"
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
	devices map[*websocket.Conn]*DeviceClient
	users   map[*websocket.Conn]*UserClient
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
func GetWebSocketHub(referenceID string) (*WebSocketHub, error) {
	var err error
	wsHubOnce.Do(func() {
		wsHub, err = NewWebSocketHub(referenceID)
		if err != nil {
			wsHub = nil
		}
	})
	return wsHub, err
}

// Inisialisasi WebSocketHub dengan Redis
func NewWebSocketHub(referenceID string) (*WebSocketHub, error) {
	redisClient := GetRedisClient()
	if redisClient == nil {
		return nil, fmt.Errorf("failed to initialize WebSocketHub: redis client is nil")
	}

	hub := &WebSocketHub{
		devices: make(map[*websocket.Conn]*DeviceClient),
		users:   make(map[*websocket.Conn]*UserClient),
		redis:   redisClient,
	}

	logger.Info(referenceID, "INFO - New WebSocketHub initialized with Redis")
	return hub, nil
}

// Menambahkan Device Baru
func (hub *WebSocketHub) AddDevice(referenceID string, conn *websocket.Conn, deviceID int64, deviceName string) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	hub.devices[conn] = &DeviceClient{
		DeviceID:   deviceID,
		DeviceName: deviceName,
		Conn:       conn,
	}

	logger.Info(referenceID, fmt.Sprintf("INFO - NEW DEVICE Connected - DeviceID: %d, DeviceName: %s", deviceID, deviceName))
	logger.Info(referenceID, fmt.Sprintf("INFO - Total devices connected: %d", len(hub.devices)))
}

// Menghapus Device
func (hub *WebSocketHub) RemoveDevice(referenceID string, conn *websocket.Conn) {
	hub.mu.Lock()
	device, exists := hub.devices[conn]
	if exists {
		delete(hub.devices, conn)
	}
	hub.mu.Unlock()

	if exists {
		logger.Info(referenceID, fmt.Sprintf("INFO - REMOVING DEVICE - DeviceID: %d, DeviceName: %s", device.DeviceID, device.DeviceName))
		conn.Close()
		logger.Info(referenceID, "INFO - REMOVING DEVICE SUCCESSFUL")
	}

	logger.Info(referenceID, fmt.Sprintf("INFO - Total devices connected: %d", len(hub.devices)))
}

// Menghapus User
func (hub *WebSocketHub) RemoveUser(referenceID string, conn *websocket.Conn) {
	hub.mu.Lock()
	user, exists := hub.users[conn]
	if exists {
		delete(hub.users, conn)
	}
	hub.mu.Unlock()

	if exists {
		logger.Info(referenceID, fmt.Sprintf("INFO - REMOVE USER - ID: %d, Username: %s", user.UserID, user.Username))
		conn.Close()
	}
}

// User subscribe ke channel Redis
func (hub *WebSocketHub) SubscribeUserToDevice(userConn *websocket.Conn, deviceID int64) {
	ctx := context.Background()
	channelName := fmt.Sprintf("device:%d", deviceID)
	sub := hub.redis.Subscribe(ctx, channelName)

	logger.Info("", fmt.Sprintf("INFO - User subscribed to channel: %s", channelName))

	go func() {
		defer sub.Close()
		ch := sub.Channel()
		for msg := range ch {
			err := userConn.WriteMessage(websocket.TextMessage, []byte(msg.Payload))
			if err != nil {
				logger.Error("", fmt.Sprintf("ERROR - Failed to send message to user: %v", err))
				userConn.Close()
				return
			}
		}
	}()
}

// Device mengirimkan data ke channel Redis
func (hub *WebSocketHub) DevicePublishToChannel(referenceID string, deviceID int64, data string) error {
	redisClient := GetRedisClient()
	if redisClient == nil {
		return fmt.Errorf("redis client is not initialized or failed to reconnect")
	}

	channelName := fmt.Sprintf("device:%d", deviceID)
	logger.Info(referenceID, fmt.Sprintf("INFO - Publishing to channel: %s", channelName))

	err := redisClient.Publish(context.Background(), channelName, data).Err()
	if err != nil {
		logger.Error(referenceID, fmt.Sprintf("ERROR - Failed to publish to Redis: %v", err))
		return err
	}
	return nil
}
