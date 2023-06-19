package helper

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"runtime"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func GetEnv(name, fallback string) string {
	if val, exists := os.LookupEnv(name); exists {
		return val
	}

	return fallback
}
func StringArrayContains(arr []string, element string) bool {
	for _, v := range arr {
		if v == element {
			return true
		}
	}
	return false
}
func ObjectIArrayContains(arr []primitive.ObjectID, element primitive.ObjectID) bool {
	for _, v := range arr {
		if v == element {
			return true
		}
	}
	return false
}

func TimeTrack(start time.Time) {
	elapsed := time.Since(start)

	// Skip this function, and fetch the PC and file for its parent.
	pc, _, _, _ := runtime.Caller(1)

	// Retrieve a function object this functions parent.
	funcObj := runtime.FuncForPC(pc)

	// Regex to extract just the function name (and not the module path).
	runtimeFunc := regexp.MustCompile(`^.*\.(.*)$`)
	name := runtimeFunc.ReplaceAllString(funcObj.Name(), "$1")

	log.Println(fmt.Sprintf("%s took %s", name, elapsed))
}
