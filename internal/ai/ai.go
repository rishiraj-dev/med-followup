package ai

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hosp-welfare/internal/logger"
	"hosp-welfare/internal/models"
)

func CallWhisper(wavFilename string) string {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		logger.Error("OPENROUTER_API_KEY is not set")
		return ""
	}

	audioData, err := os.ReadFile(wavFilename)
	if err != nil {
		logger.Error("Failed to read wav file for Whisper: %v", err)
		return ""
	}

	base64Audio := base64.StdEncoding.EncodeToString(audioData)

	payload := map[string]interface{}{
		"model": "openai/whisper-large-v3-turbo",
		"input_audio": map[string]string{
			"data":   base64Audio,
			"format": "wav",
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		logger.Error("Failed to marshal Whisper payload: %v", err)
		return ""
	}

	req, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/audio/transcriptions", bytes.NewBuffer(jsonData))
	if err != nil {
		logger.Error("Failed to create Whisper request: %v", err)
		return ""
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("Whisper request failed: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Error("Whisper returned status: %d", resp.StatusCode)
		bodyBytes, _ := io.ReadAll(resp.Body)
		logger.Error("Whisper error body: %s", string(bodyBytes))
		return ""
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logger.Error("Failed to decode Whisper response: %v", err)
		return ""
	}

	return strings.TrimSpace(result.Text)
}

func CallOpenRouter(history []models.Message, sentenceChan chan<- string) string {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		logger.Error("OPENROUTER_API_KEY is not set")
		return ""
	}

	url := "https://openrouter.ai/api/v1/chat/completions"

	payload := map[string]interface{}{
		"model":    "mistralai/mistral-small-24b-instruct-2501", // "qwen/qwen3-30b-a3b-instruct-2507"
		"messages": history,
		"stream":   true,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		logger.Error("Failed to marshal OpenRouter payload: %v", err)
		return ""
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		logger.Error("Failed to create OpenRouter request: %v", err)
		return ""
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("OpenRouter request failed: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		logger.Error("OpenRouter API returned status: %d body: %s", resp.StatusCode, string(bodyBytes))
		return ""
	}

	reader := bufio.NewReader(resp.Body)
	var fullResponse strings.Builder
	var currentSentence strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			logger.Error("Error reading stream: %v", err)
			break
		}

		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err == nil {
			if len(chunk.Choices) > 0 {
				content := chunk.Choices[0].Delta.Content
				fullResponse.WriteString(content)

				for _, r := range content {
					currentSentence.WriteRune(r)
					if r == '.' || r == '?' || r == '!' || r == '\n' || r == ']' {
						trimmed := strings.TrimSpace(currentSentence.String())
						if len(trimmed) > 0 {
							if sentenceChan != nil {
								re := regexp.MustCompile(`\[PAIN:\s*\d+\]`)
								cleanSentence := re.ReplaceAllString(trimmed, "")
								cleanSentence = strings.ReplaceAll(cleanSentence, "[CALL_ENDED]", "")
								cleanSentence = strings.TrimSpace(cleanSentence)
								if len(cleanSentence) > 0 {
									sentenceChan <- cleanSentence
								}
							}
						}
						currentSentence.Reset()
					}
				}
			}
		}
	}

	if currentSentence.Len() > 0 {
		trimmed := strings.TrimSpace(currentSentence.String())
		if len(trimmed) > 0 {
			if sentenceChan != nil {
				re := regexp.MustCompile(`\[PAIN:\s*\d+\]`)
				cleanSentence := re.ReplaceAllString(trimmed, "")
				cleanSentence = strings.ReplaceAll(cleanSentence, "[CALL_ENDED]", "")
				cleanSentence = strings.TrimSpace(cleanSentence)
				if len(cleanSentence) > 0 {
					sentenceChan <- cleanSentence
				}
			}
		}
	}

	return strings.TrimSpace(fullResponse.String())
}

func BuildSystemPrompt(patient models.PatientData) string {
	prompt := fmt.Sprintf(`You are Jasmine, an AI assistant tasked with conducting a post-operative triage call.
Patient Name: %s
Procedure: %s
Assigned Doctor: %s

Keep responses under 2 sentences. Extract pain levels 1 to 10. Append [CALL_ENDED] to your sentence if triage is complete or if pain is 8 or above. When you successfully determine their pain level, append [PAIN: X] to your response where X is the integer from 0 to 10 (e.g. [PAIN: 9]). CRITICAL: For spoken text, expand all abbreviations into full words (convert Dr. to doctor). Do not use numerical digits in spoken sentences, ONLY inside the [PAIN: X] tag. Never ask the patient to hold or stay on the line.

Never ask the patient to contact the doctor themselves. Be kind and empathetic. Depending on their pain level, respond exactly as follows before ending the call:
- High pain (8-10): Tell them the matter has been escalated, the doctor has been notified, and someone will contact them soon.
- Mid pain (4-7): Tell them we will get back to them with an appropriate resolution.
- Low/bearable pain (0-3): Wish them a speedy recovery, say get well soon, and move on.`, patient.Name, patient.Procedure, patient.AssignedDoctor)

	return prompt
}

func ExtractPainScore(text string) int {
	re := regexp.MustCompile(`\[PAIN:\s*(\d+)\]`)
	matches := re.FindStringSubmatch(text)
	if len(matches) > 1 {
		score, err := strconv.Atoi(matches[1])
		if err == nil {
			return score
		}
	}
	return -1
}
