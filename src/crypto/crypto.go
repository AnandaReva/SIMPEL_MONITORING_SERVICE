package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"monitoring_service/logger"

	"golang.org/x/crypto/pbkdf2"
)

func GenerateHMAC(text string, key string) (string, string) {
	if text == "" || key == "" {
		return "", "Missing Text or Key"
	}

	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil)), ""
}

// GeneratePBKDF2 password hashing
func GeneratePBKDF2(text string, salt string, length int, iterations int) (string, string) {
	if text == "" || salt == "" {
		return "", "Missing text or salt"
	}

	// Menggunakan SHA256 sebagai hash function untuk PBKDF2
	hash := pbkdf2.Key([]byte(text), []byte(salt), iterations, length, sha256.New)

	// Debug: Cetak hasil dalam bentuk hex untuk memastikan sesuai dengan frontend
	hashHex := hex.EncodeToString(hash)

	logger.Debug("GeneratePBKDF2", "DEBUG - GeneratePBKDF2 - Text(message):", text)
	logger.Debug("GeneratePBKDF2", "DEBUG - GeneratePBKDF2 - Salt :", salt)
	logger.Debug("GeneratePBKDF2", "DEBUG - GeneratePBKDF2 - Iterations:", iterations)
	logger.Debug("GeneratePBKDF2", "DEBUG - GeneratePBKDF2 - Derived Key (Hex):", hashHex)

	return hashHex, ""
}
