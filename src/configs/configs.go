package configs

var workerInterval int16 = 30    // second
var memoryLimit int64 = 52428800 // 50 mb
var PBKDF2Iterations int = 15000
var clientURL string = "http://localhost:3000"

// device
var deviceWsPingInterval int16 = 150 //second

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

func GetDeviceWsPingInterval() int16 {
	return deviceWsPingInterval
}
