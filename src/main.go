package main

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"time"

	"monitoring_service/db"
	"monitoring_service/handlers"
	"monitoring_service/logger"
	"monitoring_service/utils"
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
		logger.Error("DBDRIVER environment variable is required")
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

	logger.Info("MAIN", "DBDRIVER : ", DBDRIVER)
	logger.Info("MAIN", "DBHOST : ", DBHOST)
	logger.Info("MAIN", "DBPORT : ", DBPORT)
	logger.Debug("MAIN", "DBUSER : ", DBUSER)
	logger.Debug("MAIN", "DBPASS : ", DBPASS)
	logger.Info("MAIN", "DBNAME : ", DBNAME)
	logger.Info("MAIN", "DBPOOLSIZE : ", DBPOOLSIZE)

	err = db.InitDB(DBDRIVER, DBHOST, DBPORT, DBUSER, DBPASS, DBNAME, DBPOOLSIZE)
	if err != nil {
		logger.Error("MAIN", "ERROR !!! FAILED TO INITIATE DB POOL..", err)
		os.Exit(1)
	} else {
		logger.Info("MAIN", "Database Connection Pool Initated.")
	}

	paths["/"] = handlers.Greeting
	// send requestID and db conn as parameter
	paths["/process"] = handlers.Process

	paths["/device-connect"] = handlers.Device_Conn_WS

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
