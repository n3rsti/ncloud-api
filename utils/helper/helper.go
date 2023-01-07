package helper

import "os"

func GetEnv(name, fallback string) string {
	if val, exists := os.LookupEnv(name); exists {
		return val
	}

	return fallback
}
