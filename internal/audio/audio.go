package audio

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"hosp-welfare/internal/logger"
)

func MuLawToLinear(muLaw byte) int16 {
	muLaw = ^muLaw
	sign := (muLaw & 0x80)
	exponent := (muLaw & 0x70) >> 4
	data := (muLaw & 0x0F)
	sample := (int16(data) << 3) + 0x84
	sample <<= exponent
	sample -= 0x84
	if sign != 0 {
		return -sample
	}
	return sample
}

func LinearToMuLaw(sample int16) byte {
	const cBiasa = 0x84
	const cClip = 32635

	sign := (sample >> 8) & 0x80
	if sign != 0 {
		sample = -sample
	}
	if sample > cClip {
		sample = cClip
	}
	sample += cBiasa

	exponent := uint16(7)
	for mask := uint16(0x4000); (uint16(sample)&mask) == 0 && exponent > 0; mask >>= 1 {
		exponent--
	}

	mantissa := (uint16(sample) >> (exponent + 3)) & 0x0F
	ulawByte := ^(uint8(sign) | uint8(exponent<<4) | uint8(mantissa))

	return ulawByte
}

func WriteWavFile(filename string, pcmData []byte) error {
	return WriteWavFileCustom(filename, pcmData, 16000)
}

func WriteWavFileCustom(filename string, pcmData []byte, sampleRate uint32) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	byteRate := sampleRate * 1 * 16 / 8

	file.WriteString("RIFF")
	chunkSize := uint32(36 + len(pcmData))
	binary.Write(file, binary.LittleEndian, chunkSize)
	file.WriteString("WAVE")

	file.WriteString("fmt ")
	binary.Write(file, binary.LittleEndian, uint32(16))
	binary.Write(file, binary.LittleEndian, uint16(1))
	binary.Write(file, binary.LittleEndian, uint16(1))
	binary.Write(file, binary.LittleEndian, sampleRate)
	binary.Write(file, binary.LittleEndian, byteRate)
	binary.Write(file, binary.LittleEndian, uint16(2))
	binary.Write(file, binary.LittleEndian, uint16(16))

	file.WriteString("data")
	binary.Write(file, binary.LittleEndian, uint32(len(pcmData)))

	_, err = file.Write(pcmData)
	return err
}

func StreamAudioFromChannel(sentenceChan <-chan string, conn *net.UDPConn, target *net.UDPAddr, fullRecording *[]byte, recordMutex *sync.Mutex, isAgentSpeaking *atomic.Bool) {
	type AudioChunk struct {
		pcmuData []byte
		err      error
	}

	chunkChan := make(chan AudioChunk, 50)

	go func() {
		for chunk := range sentenceChan {
			outputFile := fmt.Sprintf("reply_%d.wav", rand.Intn(1000000))
			cmd := exec.Command("say", "-v", "Samantha", "-o", outputFile, "--data-format=LEI16@16000", chunk)

			if err := cmd.Run(); err != nil {
				logger.Error("Failed to generate speech chunk: %v", err)
				chunkChan <- AudioChunk{err: err}
				continue
			}

			data, err := os.ReadFile(outputFile)
			os.Remove(outputFile)

			if err != nil || len(data) <= 44 {
				chunkChan <- AudioChunk{err: fmt.Errorf("invalid wav file for chunk")}
				continue
			}

			pcm16 := data[44:]
			var pcmuData []byte
			for j := 0; j+1 < len(pcm16); j += 4 {
				sample := int16(binary.LittleEndian.Uint16(pcm16[j : j+2]))
				pcmuData = append(pcmuData, LinearToMuLaw(sample))
			}

			chunkChan <- AudioChunk{pcmuData: pcmuData}
		}
		close(chunkChan)
	}()

	chunkSize := 160
	sequenceNumber := uint16(rand.Intn(65535))
	timestamp := uint32(rand.Int31())
	ssrc := uint32(rand.Int31())

	isAgentSpeaking.Store(true)
	defer isAgentSpeaking.Store(false)

	for audio := range chunkChan {
		if audio.err != nil {
			continue
		}

		pcmuData := audio.pcmuData
		for i := 0; i < len(pcmuData); i += chunkSize {
			end := i + chunkSize
			if end > len(pcmuData) {
				break
			}

			payload := pcmuData[i:end]

			if fullRecording != nil && recordMutex != nil {
				recordMutex.Lock()
				*fullRecording = append(*fullRecording, payload...)
				recordMutex.Unlock()
			}

			header := make([]byte, 12)
			header[0] = 0x80
			header[1] = 0x00
			binary.BigEndian.PutUint16(header[2:4], sequenceNumber)
			binary.BigEndian.PutUint32(header[4:8], timestamp)
			binary.BigEndian.PutUint32(header[8:12], ssrc)

			packet := append(header, payload...)

			if _, err := conn.WriteToUDP(packet, target); err != nil {
				logger.Error("Failed to send RTP packet: %v", err)
				return
			}

			sequenceNumber++
			timestamp += 160

			time.Sleep(20 * time.Millisecond)
		}
	}
}
