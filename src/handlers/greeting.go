package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"monitoring_service/logger"
	"monitoring_service/utils"
	"net/http"
)

type HTTPContextKey string

// RequestBody adalah struktur untuk membaca JSON dari body request.
type RequestBody struct {
	Name string `json:"name"`
}

// Greeting handles requests to the root endpoint ("/").
func Greeting(w http.ResponseWriter, r *http.Request) {
	var res string

	// Context key untuk request ID
	var ctxKey HTTPContextKey = "requestID"
	referenceId, ok := r.Context().Value(ctxKey).(string)
	if !ok {
		referenceId = "unknown"
	}

	// Set header untuk response JSON
	w.Header().Set("Content-Type", "application/json")

	// Default response
	response := map[string]any{
		"error_code":    "000000000",
		"error_message": "",
	}

	// Validasi metode HTTP
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		logger.Error(referenceId, "Invalid method: ", r.Method)
		response["error_code"] = "405000001"
		response["error_message"] = "Method Not Allowed"
		res, _ = utils.JSONencode(response)
		http.Error(w, res, http.StatusMethodNotAllowed)
		return
	}

	// Penanganan untuk metode GET
	if r.Method == http.MethodGet {
		res := "Hello!"
		fmt.Fprint(w, res)
		return
	}

	if r.URL.Path != "/" {
		res := "Not Found"
		fmt.Fprint(w, res)
		return
	}

	// Penanganan untuk metode POST
	logger.Info(referenceId, "Request Method: POST")

	// Periksa apakah Content-Type adalah application/json
	if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
		logger.Error(referenceId, "Invalid content-type: ", contentType)
		response["error_code"] = "400000001"
		response["error_message"] = "Bad Request. Not JSON"
		res, _ = utils.JSONencode(response)
		http.Error(w, res, http.StatusBadRequest)
		return
	}

	// Baca body POST
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error(referenceId, "Failed to read body: ", err)
		response["error_code"] = "400000002"
		response["error_message"] = "Bad Request. Can't read POST body"
		res, _ = utils.JSONencode(response)
		http.Error(w, res, http.StatusBadRequest)
		return
	}
	logger.Info(referenceId, "POST Body: ", string(body))

	// Decode JSON body ke struct RequestBody
	var req RequestBody
	err = json.Unmarshal(body, &req)
	if err != nil {
		logger.Error(referenceId, "Failed to decode JSON: ", err)
		response["error_code"] = "400000003"
		response["error_message"] = "Bad Request. Invalid JSON"
		res, _ = utils.JSONencode(response)
		http.Error(w, res, http.StatusBadRequest)
		return
	}

	// Buat message berdasarkan input
	if req.Name == "" {
		response["message"] = "Hello!"
	} else {
		response["message"] = "Hello, " + req.Name + "!"
	}
	response["referenceId"] = referenceId

	// Encode response ke JSON dan kirimkan
	res, _ = utils.JSONencode(response)
	logger.Info(referenceId, "Response body: ", res)
	fmt.Fprint(w, res)

}
