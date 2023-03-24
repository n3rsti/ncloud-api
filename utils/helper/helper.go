package helper

import "os"

func GetEnv(name, fallback string) string {
	if val, exists := os.LookupEnv(name); exists {
		return val
	}

	return fallback
}
func StringArrayContains(arr []string, element string) bool{
	for _, v := range arr {
		if v == element {
			return true
		}
	}
	return false
}