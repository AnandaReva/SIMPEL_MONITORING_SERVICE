package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"monitoring_service/configs"
	"monitoring_service/db"
	"monitoring_service/handlers"
	"monitoring_service/logger"
	"monitoring_service/pubsub"
	"monitoring_service/utils"
	"monitoring_service/worker"
)

func generateReferenceID(timer int64) string {

	timeBase36 := strconv.FormatUint(uint64(timer), 36)
	randString, err := utils.RandomStringGenerator(8)
	if err != "" {
		randString = "12345678"
	}

	reference_id := timeBase36 + "." + randString // concate

	return reference_id

}
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow all origins
		w.Header().Set("Access-Control-Allow-Origin", "*")
		// Allow only GET and POST methods
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		// Allow only JSON content
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			// If preflight request, return 204 No Content
			w.WriteHeader(http.StatusNoContent)
			return
		}

		start := time.Now()
		requestID := generateReferenceID(start.UnixNano())

		// Log request details
		logger.Info(requestID, "Handle Request Started: ", r.Method, " ", r.URL.Path)
		logger.Info(requestID, "Query String: ", r.URL.RawQuery)
		logger.Info(requestID, "Headers:")
		for name, values := range r.Header {
			for _, value := range values {
				logger.Debug(requestID, name, ": ", value)
			}
		}

		// Add request ID to context ,
		// !!! note : context key is like mini state-management
		ctx := context.WithValue(r.Context(), handlers.HTTPContextKey("requestID"), requestID)
		next.ServeHTTP(w, r.WithContext(ctx))

		// Log completion and duration
		duration := time.Since(start)
		logger.Info(requestID, " Handle Request Completed in: ", duration)
		logger.Info(requestID, " ----------------------------------------------")

	})
}

// runWorker menjalankan worker dengan interval tertentu dan menangani graceful shutdown
func runWorker(interval time.Duration, redisMemoryLimit int64) chan struct{} {
	logger.Info("MAIN", "RUNNING WORKER")
	stopChan := make(chan struct{})
	var wg sync.WaitGroup // WaitGroup untuk menunggu worker selesai

	wg.Add(1) // Tambah counter WaitGroup
	go func() {
		defer wg.Done() // Pastikan WaitGroup berkurang saat worker selesai
		worker.StartRedisToDBWorker(interval, redisMemoryLimit, stopChan)
	}()

	// Menangani sinyal sistem untuk shutdown yang aman
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("MAIN", "Shutting down worker...")

		close(stopChan) // Menghentikan worker saat aplikasi ditutup

		// Tunggu hingga worker benar-benar selesai sebelum keluar
		wg.Wait()

		logger.Info("MAIN", "Worker stopped, exiting program.")
		os.Exit(0) // Keluar dengan status sukses
	}()

	return stopChan
}

func main() {

	paths := make(map[string]func(http.ResponseWriter, *http.Request))

	//initialize database connection

	DBDRIVER := os.Getenv("DBDRIVER")
	DBNAME := os.Getenv("DBNAME")
	DBHOST := os.Getenv("DBHOST")
	DBUSER := os.Getenv("DBUSER")
	DBPASS := os.Getenv("DBPASS")
	DBPORT, err := strconv.Atoi(os.Getenv("DBPORT"))
	if err != nil {
		logger.Error("MAIN", "Failed to parse DBPORT, using default (5432), reason: ", err)
		DBPORT = 5432 // Default to 5432 if parsing fails
	}

	DBPOOLSIZE, err := strconv.Atoi(os.Getenv("DBPOOLSIZE"))
	if err != nil {
		logger.Warning("MAIN", "Failed to parse DBPOOLSIZE, using default (20), reason: ", err)
		DBPOOLSIZE = 20 // Default to 20 if parsing fails
	}

	if len(DBDRIVER) == 0 {
		logger.Error("MAIN", "DBDRIVER environment variable is required")
	}

	if len(DBNAME) == 0 {
		logger.Error("DBNAME environment variable is required")
	}

	if len(DBHOST) == 0 {
		logger.Error("DBHOST environment variable is required")
	}

	if len(DBUSER) == 0 {
		logger.Error("DBUSER environment variable is required")
	}

	if len(DBPASS) == 0 {
		logger.Error("DBPASS environment variable is required")
	}

	logger.Info("MAIN", "-----------POSTGRESQL CONF : ")
	logger.Info("MAIN", "DBDRIVER : ", DBDRIVER)
	logger.Info("MAIN", "DBHOST : ", DBHOST)
	logger.Info("MAIN", "DBPORT : ", DBPORT)
	logger.Debug("MAIN", "DBUSER : ", DBUSER)
	logger.Debug("MAIN", "DBPASS : ", DBPASS)
	logger.Info("MAIN", "DBNAME : ", DBNAME)
	logger.Info("MAIN", "DBPOOLSIZE : ", DBPOOLSIZE)

	logger.Info("MAIN", "-----------REDIS CONF : ")
	// log redis conf
	RDHOST := os.Getenv("RDHOST")
	RDPASS := os.Getenv("RDPASS")
	RDDB, errConv := strconv.Atoi(os.Getenv("RDDB"))

	if len(RDHOST) == 0 {
		logger.Error("RDHOST environment variable is required")
	}

	if len(RDPASS) == 0 {
		logger.Warning("RDPASS environment variable is required")
	}

	if errConv != nil {
		logger.Warning("MAIN", "Failed to parse RDDB, using default (0), reason: ", errConv)
		RDDB = 0 // Default to 0 if parsing fails
	}

	logger.Info("MAIN", "RDHOST : ", RDHOST)
	logger.Info("MAIN", "RDPASS : ", RDPASS)
	logger.Info("MAIN", "RDDB : ", RDDB)

	///////////////////////////////// POSTGRESQL ///////////////////////////////
	err = db.InitDB(DBDRIVER, DBHOST, DBPORT, DBUSER, DBPASS, DBNAME, DBPOOLSIZE)
	if err != nil {
		logger.Error("MAIN", "ERROR !!! FAILED TO INITIATE DB POOL..", err)
		os.Exit(1)
	} else {
		logger.Info("MAIN", "Database Connection Pool Initated.")
	}

	///////////////////////////////// REDIS ///////////////////////////////
	// Inisialisasi Redis hanya di main

	if err := pubsub.InitRedisConn(RDHOST, RDPASS, RDDB); err != nil {
		logger.Error("MAIN", "ERROR - Redis connection failed:", err)
		os.Exit(1)
	}

	///////////////////////////////// WORKER ///////////////////////////////

	logger.Info("MAIN", "-----------WORKER CONF : ")
	workerInterval := configs.GetWorkerInterval()
	redisMemoryLimit := configs.GetRedisMemoryLimit()

	// Validasi workerInterval harus bertipe int16
	if workerInterval < 1 {
		logger.Warning("MAIN", "Invalid worker interval, using default: 30 seconds")
		workerInterval = 30
	}

	// Validasi redisMemoryLimit harus bertipe int64
	if redisMemoryLimit < 1 {
		logger.Warning("MAIN", "Invalid Redis memory limit, using default: 50MB")
	}

	redisMemoryMB := redisMemoryLimit / (1024 * 1024)
	logger.Info("MAIN", "WORKER INTERVAL (s):  : ", workerInterval)
	logger.Info("MAIN", "REDIS MEMORY LIMIT (MB):  : ", redisMemoryMB)

	runWorker(time.Duration(workerInterval)*time.Second, redisMemoryLimit)

	//////////////////////////////// ////////////////////////////////

	paths["/"] = handlers.Greeting
	// send requestID and db conn as parameter
	paths["/process"] = handlers.Process

	paths["/device-connect"] = handlers.Device_Create_Conn
	//paths["/device-disconnect"] = handlers.Device_Create_Conn

	// Register endpoints with a multiplexer
	mux := http.NewServeMux()
	for path, handler := range paths {
		mux.HandleFunc(path, handler)
	}

	// Start server
	port := ":5001"
	logger.Info("INFO", "Starting server on http://localhost", port)
	if err := http.ListenAndServe(port, corsMiddleware(mux)); err != nil {
		logger.Error("Server failed: ", err)
	}

}
