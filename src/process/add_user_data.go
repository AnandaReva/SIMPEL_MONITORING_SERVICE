package process

import (
	"monitoring_service/configs"
	"monitoring_service/crypto"
	"monitoring_service/logger"
	"monitoring_service/utils"
	"time"

	"github.com/jmoiron/sqlx"
)

/*



	exp :

	{
		"username" :  "",
		"full_name" : "",
		"email" : "",
		"password" : "",
		"data" : {
			"" : ""

		} // optional

	}
*/

func Add_User_Data(referenceId string, conn *sqlx.DB, editorUserId int64, editorRole string, param map[string]any) utils.ResultFormat {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		logger.Debug(referenceId, "DEBUG - Add_User_Data - Execution completed in", duration)
	}()

	result := utils.ResultFormat{
		ErrorCode:    "000000",
		ErrorMessage: "",
		Payload:      make(map[string]any),
	}

	logger.Info(referenceId, "INFO - Add_User_Data - params:", param)

	username, ok := param["username"].(string)
	if !ok || username == "" || len(username) < 3 {
		logger.Error(referenceId, "ERROR - Add_User_Data - Invalid username")
		result.ErrorCode = "400001"
		result.ErrorMessage = "Invalid request"
		return result
	}

	fullName, ok := param["full_name"].(string)
	if !ok || fullName == "" {
		logger.Error(referenceId, "ERROR - Add_User_Data - Invalid full_name")
		result.ErrorCode = "400002"
		result.ErrorMessage = "Invalid request"
		return result
	}

	email, ok := param["email"].(string) // ubah dari int64 ke string agar sesuai format umum email
	if !ok || email == "" {
		logger.Error(referenceId, "ERROR - Add_User_Data - Invalid email")
		result.ErrorCode = "400003"
		result.ErrorMessage = "Invalid request"
		return result
	}

	newUserRole, ok := param["role"].(string)
	if !ok || newUserRole == "" {
		logger.Error(referenceId, "ERROR - Add_User_Data - Invalid newUserRole")
		result.ErrorCode = "400005"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Cek newUserRole tidak boleh membuat user dengan newUserRole sama atau lebih tinggi
	if !canEditUser(editorRole, newUserRole) {
		logger.Error(referenceId, "ERROR - Add_User_Data - Editor role: ", editorRole, " not allowed to assign new user role: ", newUserRole)
		result.ErrorCode = "403001"
		result.ErrorMessage = "Forbidden"
		return result
	}

	password, ok := param["password"].(string)
	if !ok || password == "" || len(password) < 8 {
		logger.Error(referenceId, "ERROR - Add_User_Data - Invalid password")
		result.ErrorCode = "400004"
		result.ErrorMessage = "Invalid request"
		return result
	}

	// Cek apakah username atau email sudah ada
	var existingField *string
	queryCheck := `
		SELECT CASE 
			WHEN EXISTS (SELECT 1 FROM sysuser."user" WHERE username = $1) THEN 'username' 
			WHEN EXISTS (SELECT 1 FROM sysuser."user" WHERE email = $2) THEN 'email' 
			ELSE NULL 
		END AS existing_field;
	`
	errCheck := conn.Get(&existingField, queryCheck, username, email)
	if errCheck != nil {
		logger.Error(referenceId, "ERROR - Add_User_Data - Error checking existing fields:", errCheck)
		result.ErrorCode = "500001"
		result.ErrorMessage = "Internal Server Error"
		return result
	}
	if existingField != nil && *existingField != "" {
		logger.Error(referenceId, "ERROR - Add_User_Data - Field already exists:", *existingField)
		result.ErrorCode = "409001"
		result.ErrorMessage = "Conflict"
		result.Payload = map[string]any{"field": *existingField}
		return result
	}

	// Ambil data tambahan jika ada, jika tidak masukkan objek kosong
	jsonData := "{}"
	if deviceData, hasData := param["data"].(map[string]any); hasData {
		if jsonDataBytes, err := utils.MapToJSON(deviceData); err == nil {
			jsonData = string(jsonDataBytes)
		} else {
			logger.Error(referenceId, "ERROR - Add_User_Data - Failed to convert map to JSON: ", err)
			return utils.ResultFormat{ErrorCode: "500008", ErrorMessage: "Internal Server Error"}
		}
	}
	// Enkripsi password
	salt, _ := utils.RandomStringGenerator(16)
	saltedPassword, _ := crypto.GeneratePBKDF2(password, salt, 32, configs.GetPBKDF2Iterations())

	// Eksekusi query untuk menyimpan user baru
	// Eksekusi query untuk menyimpan user baru
	queryToRegister := `
	INSERT INTO sysuser."user" 
		(username, full_name, email, st, salt, saltedpassword, data, role)
	VALUES 
		($1, $2, $3, $4, $5, $6, $7, $8)
	RETURNING id;
`
	var newUserId int
	err := conn.Get(&newUserId, queryToRegister, username, fullName, email, 1, salt, saltedPassword, jsonData, newUserRole)

	if err != nil {
		logger.Error(referenceId, "ERROR - Add_User_Data - Failed to insert new user:", err)
		result.ErrorCode = "500002"
		result.ErrorMessage = "Internal Server Error"
		return result
	}

	logger.Info(referenceId, "INFO - Add_User_Data - New user ID:", newUserId)

	result.Payload["status"] = "success"
	result.Payload["new_user_id"] = newUserId
	return result
}
