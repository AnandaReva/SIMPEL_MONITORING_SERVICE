package ws

import (
	"fmt"
	"monitoring_service/logger"
	"sync"

	"github.com/gorilla/websocket"
)

/*
	!!!NOTE : Terdapat 2 entiti client yang terhubung ke websocket
	1. Device IoT -> mengirim data message sensor ke websocket
	2. User -> Menerima data message dari Device IoT dari websocket
*/

// Struktur client untuk menyimpan client device
type DeviceClient struct {
	DeviceID   int64
	DeviceName string
	Conn       *websocket.Conn
}

// Struktur client untuk menyimpan client user
type UserClient struct {
	UserID   int64
	Username string
	Role     string
	Conn     *websocket.Conn
}

/* // Konfigurasi upgrader WebSocket
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}
*/
// Struktur untuk menyimpan koneksi WebSocket
type WebSocketHub struct {
	mu        sync.Mutex
	devices   map[*websocket.Conn]*DeviceClient
	users     map[*websocket.Conn]*UserClient
	broadcast chan []byte
}

func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		devices:   make(map[*websocket.Conn]*DeviceClient),
		users:     make(map[*websocket.Conn]*UserClient),
		broadcast: make(chan []byte),
	}
}

// Menambahkan device baru
func (hub *WebSocketHub) AddDevice(referenceID string, conn *websocket.Conn, deviceID int64, deviceName string) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	hub.devices[conn] = &DeviceClient{
		DeviceID:   deviceID,
		DeviceName: deviceName,
		Conn:       conn,
	}

	logStr := fmt.Sprintf("NEW DEVICE - ID: %d, Name: %s", deviceID, deviceName)
	logger.Info(referenceID, "INFO -", logStr)
	logger.Info(referenceID, "INFO - TOTAL CONNECTED DEVICES:", len(hub.devices))
}

// Menambahkan user baru
func (hub *WebSocketHub) AddUser(referenceID string, conn *websocket.Conn, userID int64, username, role string) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	hub.users[conn] = &UserClient{
		UserID:   userID,
		Username: username,
		Role:     role,
		Conn:     conn,
	}

	logStr := fmt.Sprintf("NEW USER - ID: %d, Username: %s, Role: %s", userID, username, role)
	logger.Info(referenceID, "INFO -", logStr)
	logger.Info(referenceID, "INFO - TOTAL CONNECTED USERS:", len(hub.users))
}

// Menghapus device
func (hub *WebSocketHub) RemoveDevice(referenceID string, conn *websocket.Conn) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	if device, exists := hub.devices[conn]; exists {
		logStr := fmt.Sprintf("REMOVE DEVICE - ID: %d, Name: %s", device.DeviceID, device.DeviceName)
		logger.Info(referenceID, "INFO -", logStr)
		delete(hub.devices, conn)
	}

	logger.Info(referenceID, "INFO - TOTAL CONNECTED DEVICES:", len(hub.devices))
	conn.Close()
}

// Menghapus user
func (hub *WebSocketHub) RemoveUser(referenceID string, conn *websocket.Conn) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	if user, exists := hub.users[conn]; exists {
		logStr := fmt.Sprintf("REMOVE USER - ID: %d, Username: %s", user.UserID, user.Username)
		logger.Info(referenceID, "INFO -", logStr)
		delete(hub.users, conn)
	}

	logger.Info(referenceID, "INFO - TOTAL CONNECTED USERS:", len(hub.users))
	conn.Close()
}

// Mengirim data ke semua user yang terhubung
func (hub *WebSocketHub) BroadcastData(data []byte) error {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	var broadcastErr error
	for _, user := range hub.users {
		err := user.Conn.WriteMessage(websocket.TextMessage, data)
		if err != nil {
			user.Conn.Close()
			delete(hub.users, user.Conn)
			broadcastErr = err
		}
	}
	return broadcastErr
}

/*
// Handler untuk menerima data dari IoT
func (hub *WebSocketHub) IoTHandler( w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error( "Error upgrading IoT device connection: ", err)
		return
	}
	defer conn.Close()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			logger.Error( "Error reading from IoT device: ", err)
			break
		}
		fmt.Println("Received from IoT:", string(message))

		// Broadcast data ke semua client FE yang terhubung
		hub.BroadcastData(message)
	}
}

// Handler untuk client frontend
func (hub *WebSocketHub) ClientHandler(reference_id string, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(reference_id, "Error upgrading client connection: ", err)
		return
	}
	hub.AddClient(conn)

	defer hub.RemoveClient(conn)

	// Loop untuk menjaga koneksi tetap hidup
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			logger.Error(reference_id, "Client disconnected: ", err)
			break
		}
	}
}
*/
