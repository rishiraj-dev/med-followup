package sipdialer

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"hosp-welfare/internal/ai"
	"hosp-welfare/internal/audio"
	"hosp-welfare/internal/config"
	"hosp-welfare/internal/logger"
	"hosp-welfare/internal/models"
)

var currentState = "INIT"

func setState(state string) {
	logger.Debug("State Machine Transition: %s -> %s", currentState, state)
	currentState = state
}

func headersToString(msg *sip.Response) string {
	var sb strings.Builder
	for _, h := range msg.Headers() {
		sb.WriteString(h.String() + "\n")
	}
	return sb.String()
}

func parseSDP(body []byte) (string, int) {
	lines := strings.Split(string(body), "\n")
	var ip string
	var port int
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "c=IN IP4 ") {
			ip = strings.TrimPrefix(line, "c=IN IP4 ")
		} else if strings.HasPrefix(line, "m=audio ") {
			parts := strings.Split(line, " ")
			if len(parts) > 1 {
				port, _ = strconv.Atoi(parts[1])
			}
		}
	}
	return ip, port
}

func HandleCall(patientData models.PatientData, client *sipgo.Client, srv *sipgo.Server, localIP string, reader *bufio.Reader) {
	logger.Info("Patient Profile Loaded:")
	logger.Info("  ID: %s", patientData.PatientID)
	logger.Info("  Name: %s", patientData.Name)
	logger.Info("  Procedure: %s", patientData.Procedure)
	logger.Info("  Assigned Doctor: %s", patientData.AssignedDoctor)
	logger.Info("  Days Post-Op: %d", patientData.DaysPostOp)
	logger.Info("  SIP Address: %s", patientData.SIPAddress)

	callPayload := models.CallLogPayload{
		PatientID:       patientData.PatientID,
		PainLevel:       0,
		CallSummary:     "Outbound call initiated from CLI",
		NeedsEscalation: false,
	}
	_ = models.LogCallToServiceNow(callPayload)

	targetURIStr := patientData.SIPAddress

	logger.System("Pre-generating initial AI greeting for zero-latency start...")
	history := []models.Message{
		{Role: "system", Content: ai.BuildSystemPrompt(patientData)},
		{Role: "user", Content: "Hello, I just picked up the phone. Please greet me and start the triage."},
	}

	initialGreetingChan := make(chan string, 50)
	initialGreeting := ai.CallOpenRouter(history, initialGreetingChan)
	close(initialGreetingChan)

	if initialGreeting != "" {
		history = append(history, models.Message{Role: "assistant", Content: initialGreeting})
	}

	fmt.Print("\n\033[1;32m>\033[0m Press ENTER to initiate dialing to " + targetURIStr + "... ")
	_, _ = reader.ReadString('\n')

	logger.System("Starting application initialization...")

	udpAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", config.LocalRTPPort))
	if err != nil {
		logger.Error("Failed to resolve UDP address: %v", err)
		return
	}

	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		logger.Error("Failed to start UDP listener: %v", err)
		return
	}
	defer udpConn.Close()

	callEndedByRemote := false
	srv.OnBye(func(req *sip.Request, tx sip.ServerTransaction) {
		logger.Info("Received BYE from remote")
		res := sip.NewResponseFromRequest(req, 200, "OK", nil)
		tx.Respond(res)
		setState("DISCONNECTED_BY_REMOTE")
		callEndedByRemote = true
		if udpConn != nil {
			udpConn.Close()
		}
	})

	inviteCtx, inviteCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer inviteCancel()

	var targetURI sip.Uri
	err = sip.ParseUri(targetURIStr, &targetURI)
	if err != nil {
		logger.Error("Failed to parse Target URI: %v", err)
		return
	}

	req := sip.NewRequest(sip.INVITE, targetURI)

	req.RemoveHeader("From")
	fromStr := fmt.Sprintf("\"sipgo\" <sip:sipgo@%s>;tag=%x", localIP, rand.Int31())
	req.AppendHeader(sip.NewHeader("From", fromStr))

	contactHeader := sip.ContactHeader{
		Address: sip.Uri{
			User: "sipgo",
			Host: localIP,
			Port: config.LocalSIPPort,
		},
	}
	req.AppendHeader(&contactHeader)

	sdpBody := fmt.Sprintf(
		"v=0\r\n"+
			"o=- 0 0 IN IP4 %s\r\n"+
			"s=session\r\n"+
			"c=IN IP4 %s\r\n"+
			"t=0 0\r\n"+
			"m=audio %d RTP/AVP %d\r\n"+
			"a=rtpmap:%d %s\r\n"+
			"a=sendrecv\r\n",
		localIP, localIP, config.LocalRTPPort, config.RTPPayloadType, config.RTPPayloadType, config.AudioCodec)

	req.SetBody([]byte(sdpBody))
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))

	tx, err := client.TransactionRequest(inviteCtx, req)
	if err != nil {
		logger.Error("Failed to send INVITE transaction: %v", err)
		return
	}

	logger.Info("Outbound INVITE sent to %s", targetURIStr)
	logger.Debug("INVITE Request Payload:\n%s", req.String())
	setState("INVITE_SENT")

	var dialogResponse *sip.Response

WaitLoop:
	for {
		select {
		case r := <-tx.Responses():
			if r.StatusCode >= 100 && r.StatusCode < 200 {
				logger.Info("Received provisional response: %d %s", r.StatusCode, r.Reason)
			} else if r.StatusCode >= 200 && r.StatusCode < 300 {
				dialogResponse = r
				logger.Info("Received %d %s", r.StatusCode, r.Reason)
				logger.Debug("200 OK Headers:\n%s", headersToString(r))
				setState("200_OK_RECEIVED")
				break WaitLoop
			} else if r.StatusCode >= 300 {
				logger.Error("Received failure response: %d %s", r.StatusCode, r.Reason)
				return
			}
		case <-inviteCtx.Done():
			logger.Error("Context cancelled before call was answered")
			return
		}
	}

	ackReq := sip.NewRequest(sip.ACK, targetURI)

	if callIDHeader := dialogResponse.CallID(); callIDHeader != nil {
		ackReq.AppendHeader(callIDHeader)
	}
	if fromHeader := dialogResponse.From(); fromHeader != nil {
		ackReq.AppendHeader(fromHeader)
	}
	if toHeader := dialogResponse.To(); toHeader != nil {
		ackReq.AppendHeader(toHeader)
	}
	if cseqHeader := dialogResponse.CSeq(); cseqHeader != nil {
		ackReq.AppendHeader(&sip.CSeqHeader{
			SeqNo:      cseqHeader.SeqNo,
			MethodName: sip.ACK,
		})
	}
	maxFwd := sip.MaxForwardsHeader(70)
	ackReq.AppendHeader(&maxFwd)

	err = client.WriteRequest(ackReq)
	if err != nil {
		logger.Error("Failed to send ACK: %v", err)
	} else {
		logger.Info("ACK sent successfully, connection fully established")
		setState("CONNECTED")
	}

	var remoteRTPAddr *net.UDPAddr
	if dialogResponse != nil {
		ip, port := parseSDP(dialogResponse.Body())
		if ip != "" && port != 0 {
			addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", ip, port))
			if err == nil {
				remoteRTPAddr = addr
				logger.Info("Parsed remote RTP address from SDP: %s", remoteRTPAddr.String())
			}
		}
	}

	buffer := make([]byte, 2048)

	if remoteRTPAddr == nil {
		logger.System("Waiting for initial RTP packet from remote to establish return path...")

		udpConn.SetReadDeadline(time.Now().Add(10 * time.Second))
		for {
			if callEndedByRemote {
				break
			}
			_, addr, err := udpConn.ReadFromUDP(buffer)
			if err == nil {
				remoteRTPAddr = addr
				logger.Info("Captured remote RTP stream address: %s", addr.String())
				break
			} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				logger.Warn("Timeout waiting for remote RTP stream.")
				break
			} else if strings.Contains(err.Error(), "use of closed network connection") {
				break
			}
		}
	}

	if !callEndedByRemote && remoteRTPAddr != nil {
		callStartTime := time.Now()
		logger.System("Initializing AI Conversation...")

		var transcript []models.TranscriptEntry
		var fullRecording []byte
		var isProcessing atomic.Bool
		var isAgentSpeaking atomic.Bool
		var callEndedByAgent atomic.Bool
		var recordMutex sync.Mutex

		if initialGreeting != "" {
			logger.Info("Assistant greeting: %s", initialGreeting)
			transcript = append(transcript, models.TranscriptEntry{
				Timestamp: time.Now().Format(time.RFC3339),
				Speaker:   "agent",
				Text:      initialGreeting,
			})
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				time.Sleep(500 * time.Millisecond) // Wait 0.5 seconds before speaking
				audio.StreamAudioFromChannel(initialGreetingChan, udpConn, remoteRTPAddr, &fullRecording, &recordMutex, &isAgentSpeaking)
			}()
			wg.Wait()
		}

		logger.System("Starting conversational loop with silence detection...")
		var audioBuffer []byte
		var isSpeaking bool
		var lastSpokeTime time.Time

		processTurn := func(audioData []byte) {
			defer isProcessing.Store(false)

			if len(audioData) < 4000 {
				logger.System("Discarding user turn: audio too short (%d bytes)", len(audioData))
				return
			}

			logger.System("Silence detected. Processing user turn (%d bytes PCMU)...", len(audioData))

			var pcm16 []byte
			for _, b := range audioData {
				sample := audio.MuLawToLinear(b)
				sampleBytes := make([]byte, 2)
				binary.LittleEndian.PutUint16(sampleBytes, uint16(sample))
				pcm16 = append(pcm16, sampleBytes...)
				pcm16 = append(pcm16, sampleBytes...)
			}

			wavName := "user_turn.wav"
			err := audio.WriteWavFile(wavName, pcm16)
			if err != nil {
				logger.Error("Failed to write user turn WAV: %v", err)
			} else {
				transcription := ai.CallWhisper(wavName)
				if transcription != "" {
					lower := strings.ToLower(strings.TrimSpace(transcription))
					if lower == "thank you." || lower == "thank you" || lower == "thanks for watching." || lower == "bye." {
						logger.System("Discarding known Whisper hallucination: '%s'", transcription)
						return
					}

					logger.Info("User said: %s", transcription)
					history = append(history, models.Message{Role: "user", Content: transcription})
					transcript = append(transcript, models.TranscriptEntry{
						Timestamp: time.Now().Format(time.RFC3339),
						Speaker:   "patient",
						Text:      transcription,
					})

					replyChan := make(chan string, 10)
					var wg sync.WaitGroup
					wg.Add(1)
					go func() {
						defer wg.Done()
						audio.StreamAudioFromChannel(replyChan, udpConn, remoteRTPAddr, &fullRecording, &recordMutex, &isAgentSpeaking)
					}()

					fillers := []string{"Hmm.", "Umm.", "Well.", "Let's see.", "Alright.", "Okay."}
					replyChan <- fillers[rand.Intn(len(fillers))]

					reply := ai.CallOpenRouter(history, replyChan)
					close(replyChan)
					wg.Wait()

					if reply != "" {
						callEnded := false
						if strings.Contains(reply, "[CALL_ENDED]") {
							callEnded = true
							reply = strings.ReplaceAll(reply, "[CALL_ENDED]", "")
						}

						reply = strings.TrimSpace(reply)
						history = append(history, models.Message{Role: "assistant", Content: reply})
						logger.Info("Assistant reply: %s", reply)
						transcript = append(transcript, models.TranscriptEntry{
							Timestamp: time.Now().Format(time.RFC3339),
							Speaker:   "agent",
							Text:      reply,
						})

						if callEnded {
							logger.System("Call Ended flag detected in AI response. Initiating teardown...")
							callEndedByAgent.Store(true)
						}
					}
				}
			}
		}

		for {
			if callEndedByRemote || callEndedByAgent.Load() {
				break
			}

			udpConn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
			n, addr, err := udpConn.ReadFromUDP(buffer)

			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					if len(audioBuffer) > 0 && isSpeaking {
						if !isProcessing.Load() {
							isProcessing.Store(true)
							audioToProcess := make([]byte, len(audioBuffer))
							copy(audioToProcess, audioBuffer)
							audioBuffer = nil
							isSpeaking = false
							go processTurn(audioToProcess)
						}
					}
					continue
				}

				if strings.Contains(err.Error(), "use of closed network connection") {
					break
				}
				logger.Warn("Error reading from UDP: %v", err)
				continue
			}

			if remoteRTPAddr == nil || remoteRTPAddr.String() != addr.String() {
				remoteRTPAddr = addr
			}

			if n > config.RTPHeaderLength {
				payload := buffer[config.RTPHeaderLength:n]

				if isAgentSpeaking.Load() {
					isSpeaking = false
					audioBuffer = nil
					continue
				}

				recordMutex.Lock()
				fullRecording = append(fullRecording, payload...)
				recordMutex.Unlock()

				var sum int64
				for _, b := range payload {
					sample := audio.MuLawToLinear(b)
					if sample < 0 {
						sample = -sample
					}
					sum += int64(sample)
				}
				avgEnergy := sum / int64(len(payload))

				if avgEnergy > 2000 {
					if !isSpeaking {
						audioBuffer = nil
					}
					lastSpokeTime = time.Now()
					isSpeaking = true
				}

				if isSpeaking && !isProcessing.Load() {
					audioBuffer = append(audioBuffer, payload...)
				}

				if isSpeaking && time.Since(lastSpokeTime) > 2500*time.Millisecond {
					if !isProcessing.Load() {
						isProcessing.Store(true)
						audioToProcess := make([]byte, len(audioBuffer))
						copy(audioToProcess, audioBuffer)
						audioBuffer = nil
						isSpeaking = false
						go processTurn(audioToProcess)
					}
				}
			}
		}

		setState("TEARDOWN_INITIATED")

		finalScore := 0
		for _, msg := range history {
			if msg.Role == "assistant" {
				score := ai.ExtractPainScore(msg.Content)
				if score >= 0 {
					finalScore = score
				}
			}
		}

		finalPayload := models.CallLogPayload{
			PatientID:       patientData.PatientID,
			PainLevel:       finalScore,
			CallSummary:     "Triage completed successfully. Pain level recorded as " + strconv.Itoa(finalScore),
			NeedsEscalation: finalScore >= 8,
		}

		fmt.Printf("\n--- CALL SUMMARY ---\n")
		fmt.Printf("Patient ID: %s\n", finalPayload.PatientID)
		fmt.Printf("Pain Level: %d\n", finalPayload.PainLevel)
		fmt.Printf("Summary: %s\n", finalPayload.CallSummary)
		fmt.Printf("Escalation Required: %v\n", finalPayload.NeedsEscalation)
		fmt.Printf("--------------------\n\n")

		_ = models.LogCallToServiceNow(finalPayload)

		if len(fullRecording) > 0 {
			var recPCM16 []byte
			for _, b := range fullRecording {
				sample := audio.MuLawToLinear(b)
				sampleBytes := make([]byte, 2)
				binary.LittleEndian.PutUint16(sampleBytes, uint16(sample))
				recPCM16 = append(recPCM16, sampleBytes...)
			}
			recFile := fmt.Sprintf("outbound-data/recordings/%s_%s.wav", patientData.PatientID, callStartTime.Format("2006-01-02_15-04-05"))
			if err := audio.WriteWavFileCustom(recFile, recPCM16, 8000); err != nil {
				logger.Error("Failed to save call recording: %v", err)
			} else {
				logger.Info("Call recording saved: %s", recFile)
			}

			callRecord := models.CallTranscript{
				PatientID:   patientData.PatientID,
				PatientName: patientData.Name,
				CallStart:   callStartTime.Format(time.RFC3339),
				CallEnd:     time.Now().Format(time.RFC3339),
				Recording:   recFile,
				Entries:     transcript,
			}
			transcriptFile := fmt.Sprintf("outbound-data/transcripts/%s_%s.json", patientData.PatientID, callStartTime.Format("2006-01-02_15-04-05"))
			transcriptJSON, _ := json.MarshalIndent(callRecord, "", "  ")
			if err := os.WriteFile(transcriptFile, transcriptJSON, 0644); err != nil {
				logger.Error("Failed to save transcript: %v", err)
			} else {
				logger.Info("Transcript saved: %s", transcriptFile)
			}
		}
	}

	if !callEndedByRemote && dialogResponse != nil {
		time.Sleep(1 * time.Second) // Wait 1 second before ending the call
		byeTargetURI := targetURI
		if contactHeader := dialogResponse.GetHeader("Contact"); contactHeader != nil {
			if contact, ok := contactHeader.(*sip.ContactHeader); ok {
				byeTargetURI = contact.Address
			}
		}

		byeReq := sip.NewRequest(sip.BYE, byeTargetURI)
		if callIDHeader := dialogResponse.CallID(); callIDHeader != nil {
			byeReq.AppendHeader(callIDHeader)
		}
		if fromHeader := dialogResponse.From(); fromHeader != nil {
			byeReq.AppendHeader(fromHeader)
		}
		if toHeader := dialogResponse.To(); toHeader != nil {
			byeReq.AppendHeader(toHeader)
		}
		if cseqHeader := dialogResponse.CSeq(); cseqHeader != nil {
			byeReq.AppendHeader(&sip.CSeqHeader{
				SeqNo:      cseqHeader.SeqNo + 1,
				MethodName: sip.BYE,
			})
		}
		byeMaxFwd := sip.MaxForwardsHeader(70)
		byeReq.AppendHeader(&byeMaxFwd)

		byeTx, err := client.TransactionRequest(context.Background(), byeReq)
		if err != nil {
			logger.Error("Failed to send BYE transaction: %v", err)
		} else {
			logger.Info("Outbound BYE sent to softphone")
			setState("BYE_SENT")

			select {
			case r := <-byeTx.Responses():
				logger.Info("Received response to BYE: %d %s", r.StatusCode, r.Reason)
				setState("DISCONNECTED")
			case <-time.After(2 * time.Second):
				logger.Warn("Timeout waiting for response to BYE")
			}
		}
	}
}
