package configs

var workerInterval int16 = 60    // second
var memoryLimit int64 = 52428800 // 50 mb

// exp fetch config : configs.GetWorkerInterval()

func GetWorkerInterval() int16 {
	return workerInterval
}

func GetRedisMemoryLimit() int64 {
	return memoryLimit
}
