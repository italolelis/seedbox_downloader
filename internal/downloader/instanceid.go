package downloader

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strconv"
)

// GenerateInstanceID returns a unique string for this process (hostname+pid+random)
func GenerateInstanceID() string {
	host, _ := os.Hostname()
	pid := os.Getpid()
	rnd := make([]byte, 4)
	_, _ = rand.Read(rnd)
	return host + "-" + strconv.Itoa(pid) + "-" + hex.EncodeToString(rnd)
}
