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

// Inisialisasi WebSocketHub dengan Redis
func NewWebSocketHub(reference_id string) (*WebSocketHub, error) {
	redisClient := GetRedisClient()
	if redisClient == nil {
		return nil, fmt.Errorf("failed to initialize WebSocketHub: redis client is nil")
	}

	hub := &WebSocketHub{
		Devices:    make(map[*websocket.Conn]*DeviceClient),
		Users:      make(map[*websocket.Conn]*UserClient),
		UserConn:   make(map[int64]*websocket.Conn), // Inisialisasi map
		DeviceConn: make(map[int64]*websocket.Conn), // Inisialisasi map
		redis:      redisClient,
	}

	logger.Info(reference_id, "INFO - New WebSocketHub initialized with Redis")
	return hub, nil
}

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


