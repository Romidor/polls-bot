package main

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Token    string
	GroupID  int
	AdminIDs map[int]bool
	Timezone *time.Location
	DataDir  string
}

func LoadConfig() *Config {
	token := os.Getenv("VK_TOKEN")
	if token == "" {
		log.Fatal("VK_TOKEN is required")
	}

	groupID, err := strconv.Atoi(os.Getenv("VK_GROUP_ID"))
	if err != nil {
		log.Fatal("VK_GROUP_ID must be an integer")
	}

	adminRaw := os.Getenv("VK_ADMIN_IDS")
	adminIDs := make(map[int]bool)
	for _, s := range strings.Split(adminRaw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := strconv.Atoi(s)
		if err != nil {
			log.Fatalf("invalid admin id: %s", s)
		}
		adminIDs[id] = true
	}

	tzName := os.Getenv("TZ")
	if tzName == "" {
		tzName = "Europe/Moscow"
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		log.Fatalf("invalid timezone %s: %v", tzName, err)
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}

	return &Config{
		Token:    token,
		GroupID:  groupID,
		AdminIDs: adminIDs,
		Timezone: loc,
		DataDir:  dataDir,
	}
}
