package configs

var workerInterval int16 = 60    // second
var memoryLimit int64 = 52428800 // 50 mb
var PBKDF2Iterations int = 15000
var clientURL string = "http://localhost:3000"

// exp fetch config : configs.GetWorkerInterval()

func GetWorkerInterval() int16 {
	return workerInterval
}

func GetRedisMemoryLimit() int64 {
	return memoryLimit
}

func GetPBKDF2Iterations() int {
	return PBKDF2Iterations
}

func GetClientURL() string {
	return clientURL
}
