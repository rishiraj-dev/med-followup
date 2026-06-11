package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/emiago/sipgo"

	"hosp-welfare/internal/config"
	"hosp-welfare/internal/logger"
	"hosp-welfare/internal/models"
	"hosp-welfare/internal/sipdialer"
)

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func promptForPatient(reader *bufio.Reader) models.PatientData {
	var patientData models.PatientData
	var err error

	for {
		clearScreen()

		var patients []models.PatientData
		if file, readErr := os.ReadFile("patients.json"); readErr == nil {
			json.Unmarshal(file, &patients)
		}

		var right []string
		right = append(right, "\033[1;36mOUTBOUND SIP DIALER\033[0m")
		right = append(right, "-------------------")
		right = append(right, "\033[1;33mVersion:\033[0m 1.0.0")
		right = append(right, "\033[1;33mProtocol:\033[0m SIP/2.0")
		right = append(right, fmt.Sprintf("\033[1;33mRTP Port:\033[0m %d", config.LocalRTPPort))
		right = append(right, "")
		right = append(right, "\033[1;32mAVAILABLE PATIENTS\033[0m")
		right = append(right, "------------------")
		for _, p := range patients {
			right = append(right, fmt.Sprintf(" \033[1;37m%s\033[0m (%s) - %s", p.PatientID, p.Name, p.Procedure))
		}

		logo := []string{
			"\033[38;5;135m⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢀⣀⡀⠀⣀⣀⠀⢀⣀⡀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀\033[0m",
			"\033[38;5;141m⠀⠀⠀⠀⠀⠀⢀⣠⣴⣾⣿⣿⣇⠸⣿⣿⠇⣸⣿⣿⣷⣦⣄⡀⠀⠀⠀⠀⠀⠀\033[0m",
			"\033[38;5;147m⠀⢀⣠⣴⣶⠿⠋⣩⡿⣿⡿⠻⣿⡇⢠⡄⢸⣿⠟⢿⣿⢿⣍⠙⠿⣶⣦⣄⡀⠀\033[0m",
			"\033[38;5;153m⠀⠀⠉⠉⠁⠶⠟⠋⠀⠉⠀⢀⣈⣁⡈⢁⣈⣁⡀⠀⠉⠀⠙⠻⠶⠈⠉⠉⠀⠀\033[0m",
			"\033[38;5;159m⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣴⣿⡿⠛⢁⡈⠛⢿⣿⣦⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀\033[0m",
			"\033[38;5;123m⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠿⣿⣦⣤⣈⠁⢠⣴⣿⠿⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀\033[0m",
			"\033[38;5;117m⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠈⠉⠻⢿⣿⣦⡉⠁⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀\033[0m",
			"\033[38;5;111m⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠘⢷⣦⣈⠛⠃⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀\033[0m",
			"\033[38;5;105m⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢠⣴⠦⠈⠙⠿⣦⡄⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀\033[0m",
			"\033[38;5;99m⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠸⣿⣤⡈⠁⢤⣿⠇⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀\033[0m",
			"\033[38;5;93m⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠉⠛⠷⠄⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀\033[0m",
			"\033[38;5;105m⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⢀⣀⠑⢶⣄⡀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀\033[0m",
			"\033[38;5;111m⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⣿⠁⢰⡆⠈⡿⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀\033[0m",
			"\033[38;5;117m⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠈⠳⠈⣡⠞⠁⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀\033[0m",
			"\033[38;5;123m⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠈⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀\033[0m",
		}

		maxLines := len(logo)
		if len(right) > maxLines {
			maxLines = len(right)
		}

		fmt.Println()
		for i := 0; i < maxLines; i++ {
			leftStr := ""
			if i < len(logo) {
				leftStr = logo[i]
			} else {
				leftStr = strings.Repeat(" ", 40)
			}

			rightStr := ""
			if i < len(right) {
				rightStr = right[i]
			}

			fmt.Printf("   %-50s %s\n", leftStr, rightStr)
		}

		fmt.Print("\n\033[1;32m>\033[0m Enter Patient ID (or 'exit' to quit) [\033[1;37mPT-1001\033[0m]: ")
		patientID, _ := reader.ReadString('\n')
		patientID = strings.TrimSpace(patientID)

		if patientID == "exit" || patientID == "quit" {
			os.Exit(0)
		}

		if patientID == "" {
			patientID = "PT-1001"
		}

		logger.System("Fetching data for Patient ID: %s", patientID)

		patientData, err = models.FetchPatientFromServiceNow(patientID)
		if err != nil {
			logger.Error("Failed to retrieve patient data: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		return patientData
	}
}

func main() {
	config.LoadEnv()

	os.MkdirAll("outbound-data/recordings", 0755)
	os.MkdirAll("outbound-data/transcripts", 0755)

	ua, err := sipgo.NewUA()
	if err != nil {
		logger.Error("Failed to create SIP User Agent: %v", err)
		os.Exit(1)
	}

	srv, err := sipgo.NewServer(ua)
	if err != nil {
		logger.Error("Failed to create SIP Server: %v", err)
		os.Exit(1)
	}

	client, err := sipgo.NewClient(ua)
	if err != nil {
		logger.Error("Failed to create SIP Client: %v", err)
		os.Exit(1)
	}

	localAddrs, err := net.InterfaceAddrs()
	if err != nil {
		logger.Error("Failed to get local addresses: %v", err)
		os.Exit(1)
	}

	var localIP string
	for _, addr := range localAddrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				localIP = ipNet.IP.String()
				break
			}
		}
	}

	if localIP == "" {
		logger.Error("Could not determine local IP address")
		os.Exit(1)
	}

	go func() {
		if err := srv.ListenAndServe(context.Background(), "udp", fmt.Sprintf("%s:%d", localIP, config.LocalSIPPort)); err != nil {
			logger.Error("SIP Server listen error: %v", err)
		}
	}()

	reader := bufio.NewReader(os.Stdin)

	for {
		patientData := promptForPatient(reader)
		sipdialer.HandleCall(patientData, client, srv, localIP, reader)

		logger.System("Call flow finished. Restarting application loop...")
		time.Sleep(3 * time.Second)
	}
}
