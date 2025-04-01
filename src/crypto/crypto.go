package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"monitoring_service/logger"

	"golang.org/x/crypto/pbkdf2"
)

func GenerateHMAC(text string, key string) (string, error) {
	if text == "" || key == "" {
		return "", errors.New("missing Text or Key")
	}

	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// GeneratePBKDF2 password hashing
func GeneratePBKDF2(text string, salt string, length int, iterations int) (string, error) {
	if text == "" || salt == "" {
		return "", errors.New("missing text or salt")
	}

	// Menggunakan SHA256 sebagai hash function untuk PBKDF2
	hash := pbkdf2.Key([]byte(text), []byte(salt), iterations, length, sha256.New)

	// Debug: Cetak hasil dalam bentuk hex untuk memastikan sesuai dengan frontend
	hashHex := hex.EncodeToString(hash)

	logger.Debug("GeneratePBKDF2", "DEBUG - GeneratePBKDF2 - Text(message):", text)
	logger.Debug("GeneratePBKDF2", "DEBUG - GeneratePBKDF2 - Salt :", salt)
	logger.Debug("GeneratePBKDF2", "DEBUG - GeneratePBKDF2 - Iterations:", iterations)
	logger.Debug("GeneratePBKDF2", "DEBUG - GeneratePBKDF2 - Derived Key (Hex):", hashHex)

	return hashHex, nil
}

// Fungsi untuk mengenkripsi data menggunakan AES dengan mode CBC
// Fungsi untuk mengenkripsi data menggunakan AES-256 CBC dengan output dalam hex
func EncryptAES256(plainText string, keyHex string) (string, string, error) {
	// Decode key dari hex ke byte slice
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", "", errors.New("invalid hex key")
	}

	// Pastikan panjang key adalah 32 byte (AES-256)
	if len(keyBytes) != 32 {
		return "", "", errors.New("invalid key size, must be 32 bytes for AES-256")
	}

	// Membuat cipher block menggunakan key AES
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", "", err
	}

	// Menambahkan padding agar panjang plaintext adalah kelipatan dari block size
	plainTextBytes := []byte(plainText)
	blockSize := block.BlockSize()
	padding := blockSize - len(plainTextBytes)%blockSize
	paddingText := make([]byte, padding)
	for i := range paddingText {
		paddingText[i] = byte(padding)
	}
	plainTextBytes = append(plainTextBytes, paddingText...)

	// Membuat IV acak
	iv := make([]byte, blockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", "", err
	}

	// Menggunakan cipher block mode CBC untuk mengenkripsi data
	cipherText := make([]byte, len(plainTextBytes))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(cipherText, plainTextBytes)

	// Mengonversi cipherText dan iv ke hex
	cipherTextHex := hex.EncodeToString(cipherText)
	ivHex := hex.EncodeToString(iv)

	return cipherTextHex, ivHex, nil
}

// Fungsi untuk mendekripsi data menggunakan AES-256 CBC dengan input dalam hex
func DecryptAES256(cipherTextHex string, ivHex string, keyHex string) (string, error) {
	// Decode key dari hex ke byte slice
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", errors.New("invalid hex key")
	}

	// Pastikan panjang key adalah 32 byte (AES-256)
	if len(keyBytes) != 32 {
		return "", errors.New("invalid key size, must be 32 bytes for AES-256")
	}

	// Decode ciphertext dan IV dari hex
	cipherText, err := hex.DecodeString(cipherTextHex)
	if err != nil {
		return "", errors.New("invalid hex cipherText")
	}

	iv, err := hex.DecodeString(ivHex)
	if err != nil {
		return "", errors.New("invalid hex IV")
	}

	// Membuat cipher block menggunakan key AES
	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", err
	}

	// Membuat mode CBC untuk dekripsi
	mode := cipher.NewCBCDecrypter(block, iv)
	plainText := make([]byte, len(cipherText))
	mode.CryptBlocks(plainText, cipherText)

	// Menghapus padding PKCS7
	padding := int(plainText[len(plainText)-1])
	if padding > len(plainText) || padding > block.BlockSize() {
		return "", errors.New("invalid padding")
	}
	plainText = plainText[:len(plainText)-padding]

	return string(plainText), nil
}
