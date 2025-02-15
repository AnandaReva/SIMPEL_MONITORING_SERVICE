package configs

var workerInterval int16 = 8
var limit int16 = 10

// exp fetch config : configs.GetWorkerInterval()

func GetWorkerInterval() int16 {
	return workerInterval
}

func GetWorkerLimit() int16 {
	return limit
}
