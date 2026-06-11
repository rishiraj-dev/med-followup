package models

import (
	"encoding/json"
	"fmt"
	"os"
)

type PatientData struct {
	PatientID      string `json:"patient_id"`
	Name           string `json:"name"`
	Procedure      string `json:"procedure"`
	AssignedDoctor string `json:"assigned_doctor"`
	SIPAddress     string `json:"sip_address"`
	DaysPostOp     int    `json:"days_post_op"`
}

type CallLogPayload struct {
	PatientID       string `json:"patient_id"`
	PainLevel       int    `json:"pain_level"`
	CallSummary     string `json:"call_summary"`
	NeedsEscalation bool   `json:"needs_escalation"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type TranscriptEntry struct {
	Timestamp string `json:"timestamp"`
	Speaker   string `json:"speaker"`
	Text      string `json:"text"`
}

type CallTranscript struct {
	PatientID   string            `json:"patient_id"`
	PatientName string            `json:"patient_name"`
	CallStart   string            `json:"call_start"`
	CallEnd     string            `json:"call_end"`
	Recording   string            `json:"recording_file"`
	Entries     []TranscriptEntry `json:"entries"`
}

func FetchPatientFromServiceNow(patientID string) (PatientData, error) {
	file, err := os.ReadFile("patients.json")
	if err != nil {
		return PatientData{}, fmt.Errorf("failed to read patients.json: %v", err)
	}

	var patients []PatientData
	if err := json.Unmarshal(file, &patients); err != nil {
		return PatientData{}, err
	}

	for _, p := range patients {
		if p.PatientID == patientID {
			return p, nil
		}
	}
	return PatientData{}, fmt.Errorf("patient not found")
}

func LogCallToServiceNow(payload CallLogPayload) error {
	filename := "outbound-data/call_logs.json"

	var logs []CallLogPayload

	if file, err := os.ReadFile(filename); err == nil {
		json.Unmarshal(file, &logs)
	}

	logs = append(logs, payload)

	jsonData, err := json.MarshalIndent(logs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, jsonData, 0644)
}
