package config

import (
	"bufio"
	"os"
	"strings"
)

const (
	LocalSIPPort = 5060
	LocalRTPPort = 5004

	AudioCodec      = "PCMU/8000"
	RTPPayloadType  = 0
	RTPHeaderLength = 12
)

func LoadEnv() {
	file, err := os.Open(".env")
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			os.Setenv(parts[0], parts[1])
		}
	}
}
