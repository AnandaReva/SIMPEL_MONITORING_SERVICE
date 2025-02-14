package db

import (
	"errors"
	"fmt"
	"monitoring_service/logger"
	"sync"
	"time"
	_ "github.com/go-sql-driver/mysql" // MySQL driver (imported only for its side-effect)
	"github.com/jmoiron/sqlx"          // SQLx for database handling
	_ "github.com/lib/pq"              // PostgreSQL driver (imported only for its side-effect)
)

// DBPool struct represents the connection pool for database connections.
type DBPool struct {
	db       *sqlx.DB  // SQLx database instance for connections
	count    int       // Active connection count in the pool
	mutex    sync.Mutex // Mutex to ensure safe concurrent access to the pool
	poolSize int       // Maximum number of connections allowed in the pool
}

// Global variable for the database pool
var dbpool DBPool

// GetConnection retrieves an available database connection from the pool.
// If no connection is available, it returns an error.
func GetConnection() (*sqlx.DB, error) {
	// Lock the pool to ensure no other goroutine can access it concurrently
	dbpool.mutex.Lock()
	defer dbpool.mutex.Unlock()

	// Check if the pool has reached its maximum size
	if dbpool.count >= dbpool.poolSize {
		// If no connection is available, return an error
		dbpool.count = dbpool.poolSize
		return nil, errors.New("no connection available in pool")
	} else {
		// Increment active connection count and return the available connection
		dbpool.count++
		return dbpool.db, nil
	}
}

// ReleaseConnection releases a database connection back to the pool.
// It decreases the active connection count.
func ReleaseConnection() {
	// Lock the pool to ensure no other goroutine can access it concurrently
	dbpool.mutex.Lock()
	defer dbpool.mutex.Unlock()

	// Decrease the connection count if there are any active connections
	if dbpool.count > 0 {
		dbpool.count--
	} else {
		// Ensure the connection count does not go below zero
		dbpool.count = 0
	}
}


func InitDB(driver string, host string, port int, user string, password string, dbname string, poolSize int) error {
	var err error

	//	connStr := fmt.Sprintf("%s://%s:%s@%s:%d/%s", driver, user, password, host, port, dbname)
	connStr := fmt.Sprintf("%s://%s:%s@%s:%d/%s?sslmode=disable", driver, user, password, host, port, dbname)
	// Log the connection string for debugging (ensure this does not log sensitive info in production)
	logger.Debug("DB", "CONNSTR : ", connStr)
	dbpool.db, err = sqlx.Connect(driver, connStr)
	if err != nil {
		// If connection fails, return the error
		return err
	}


	dbpool.db.SetMaxOpenConns(poolSize)
	dbpool.db.SetMaxIdleConns(poolSize)

	// Set the maximum lifetime for connections (connections will be closed after this duration)
	dbpool.db.SetConnMaxLifetime(5 * time.Minute)

	// Set the pool size and reset the active connection count to 0
	dbpool.poolSize = poolSize
	dbpool.count = 0

	// Return nil indicating that the database connection pool has been successfully initialized
	return nil
}
