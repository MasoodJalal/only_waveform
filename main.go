package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// WAVHeader represents the header of a WAV file
type WAVHeader struct {
	ChunkID       [4]byte
	ChunkSize     uint32
	Format        [4]byte
	SubChunk1ID   [4]byte
	SubChunk1Size uint32
	AudioFormat   uint16
	NumChannels   uint16
	SampleRate    uint32
	ByteRate      uint32
	BlockAlign    uint16
	BitsPerSample uint16
	SubChunk2ID   [4]byte
	SubChunk2Size uint32
}

// AudioData holds separated channel data
type AudioData struct {
	LeftChannel  []float64
	RightChannel []float64
	SampleRate   uint32
}

func main() {

	// inputFile := "input.wav" // Change this to your WAV file path
	inputPath := "./audios"
	outputDir := "./waveforms"
	width := 1920
	height := 640

	// Read directory contents
	files, err := os.ReadDir(inputPath)
	if err != nil {
		fmt.Printf("Error reading directory: %v\n", err)
		return
	}

	var wg sync.WaitGroup

	startTime := time.Now()

	for _, file := range files {

		fileName := file.Name()

		if !strings.HasSuffix(strings.ToLower(fileName), ".wav") {
			continue // Skip non-WAV files
		}

		inputFile := filepath.Join(inputPath, fileName)

		wg.Add(1)
		go GenerateStereoWaveforms(inputFile, outputDir, fileName, width, height, &wg)
	}

	wg.Wait()

	endTime := time.Now()
	totalTime := time.Since(startTime)

	fmt.Printf("\nTime Start: %v\n", startTime)
	fmt.Printf("\nTime End: %v\n", endTime)

	fmt.Printf("\nTime Taken: %v \n", totalTime)
}

// GenerateStereoWaveforms creates separate waveform images for left and right channels
func GenerateStereoWaveforms(inputFile, outputDir, fileName string, width, height int, wg *sync.WaitGroup) {

	defer wg.Done()

	// Parse WAV file
	audioData, err := parseWAVFile(inputFile)
	if err != nil {
		// return fmt.Errorf("failed to parse WAV file: %w", err)
		fmt.Printf("failed to parse WAV file: %v  %v", inputFile, err)
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		// return fmt.Errorf("failed to create output directory: %w", err)
		fmt.Printf("failed to create output directory: %v  %v", inputFile, err)
	}

	// Generate left channel waveform
	leftFile := fmt.Sprintf("%s/%s.png", outputDir, strings.Split(fileName, ".")[0])
	if err := generateWaveformImage(audioData.LeftChannel, width, height, leftFile); err != nil {
		// return fmt.Errorf("failed to generate left channel waveform: %w", err)
		fmt.Printf("failed to generate left channel waveform: %v  %v", inputFile, err)
	}

	fmt.Printf("Successfully generated waveforms:\n")
	fmt.Printf("  Left channel: %s\n", leftFile)
	fmt.Printf("  Sample rate: %d Hz\n", audioData.SampleRate)
	fmt.Printf("  Duration: %.2f seconds\n", float64(len(audioData.LeftChannel))/float64(audioData.SampleRate))
	fmt.Printf("  Samples: %d\n", len(audioData.LeftChannel))

}

// parseWAVFile reads a WAV file and extracts stereo audio data
func parseWAVFile(filename string) (*AudioData, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Get file size for validation
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}
	fileSize := fileInfo.Size()

	// Read WAV header
	var header WAVHeader
	if err := binary.Read(file, binary.LittleEndian, &header); err != nil {
		return nil, fmt.Errorf("failed to read WAV header: %w", err)
	}

	// Validate WAV format
	if string(header.ChunkID[:]) != "RIFF" || string(header.Format[:]) != "WAVE" {
		return nil, fmt.Errorf("not a valid WAV file")
	}

	if header.NumChannels != 2 {
		return nil, fmt.Errorf("only stereo files are supported (found %d channels)", header.NumChannels)
	}

	if header.BitsPerSample != 16 {
		return nil, fmt.Errorf("only 16-bit samples are supported (found %d bits)", header.BitsPerSample)
	}

	fmt.Printf("File: %s\n", filename)
	fmt.Printf("SampleRate: %d\n", header.SampleRate)
	fmt.Printf("NumChannels: %d\n", header.NumChannels)
	fmt.Printf("BitsPerSample: %d\n", header.BitsPerSample)
	fmt.Printf("SubChunk2Size (header): %d bytes\n", header.SubChunk2Size)
	fmt.Printf("BlockAlign: %d bytes\n", header.BlockAlign)
	fmt.Printf("File size: %d bytes\n", fileSize)

	// Calculate actual audio data size
	headerSize := int64(44) // Standard WAV header size
	actualAudioDataSize := fileSize - headerSize

	// Use the actual file size if header reports 0 or unrealistic size
	audioDataSize := header.SubChunk2Size
	if audioDataSize == 0 || int64(audioDataSize) > actualAudioDataSize {
		fmt.Printf("Warning: Header reports SubChunk2Size=%d, but calculated actual size=%d. Using actual size.\n",
			header.SubChunk2Size, actualAudioDataSize)
		audioDataSize = uint32(actualAudioDataSize)
	}

	// Calculate number of samples
	bytesPerSample := header.NumChannels * (header.BitsPerSample / 8)
	numSamples := int(audioDataSize) / int(bytesPerSample)

	fmt.Printf("Calculated audio data size: %d bytes\n", audioDataSize)
	fmt.Printf("Bytes per sample: %d\n", bytesPerSample)
	fmt.Printf("Number of samples: %d\n", numSamples)

	// Read audio data
	audioData := &AudioData{
		SampleRate: header.SampleRate,
	}

	// Pre-allocate slices for better performance
	audioData.LeftChannel = make([]float64, 0, numSamples)
	audioData.RightChannel = make([]float64, 0, numSamples)

	samplesRead := 0
	for samplesRead < numSamples {
		if header.NumChannels == 1 {
			// Mono file - read one sample and duplicate it
			var sample int16
			if err := binary.Read(file, binary.LittleEndian, &sample); err != nil {
				if err == io.EOF {
					break
				}
				return nil, fmt.Errorf("failed to read mono sample at position %d: %w", samplesRead, err)
			}

			// Convert to float64 and normalize to [-1, 1]
			normalizedSample := float64(sample) / 32767.0
			audioData.LeftChannel = append(audioData.LeftChannel, normalizedSample)
			audioData.RightChannel = append(audioData.RightChannel, normalizedSample)
		} else {
			// Stereo file - read left and right samples
			var leftSample, rightSample int16

			if err := binary.Read(file, binary.LittleEndian, &leftSample); err != nil {
				if err == io.EOF {
					break
				}
				return nil, fmt.Errorf("failed to read left sample at position %d: %w", samplesRead, err)
			}

			if err := binary.Read(file, binary.LittleEndian, &rightSample); err != nil {
				if err == io.EOF {
					fmt.Printf("Warning: EOF reached while reading right channel at sample %d\n", samplesRead)
					break
				}
				return nil, fmt.Errorf("failed to read right sample at position %d: %w", samplesRead, err)
			}

			// Convert to float64 and normalize to [-1, 1]
			audioData.LeftChannel = append(audioData.LeftChannel, float64(leftSample)/32767.0)
			audioData.RightChannel = append(audioData.RightChannel, float64(rightSample)/32767.0)
		}

		samplesRead++
	}

	actualDuration := float64(len(audioData.LeftChannel)) / float64(header.SampleRate)
	fmt.Printf("Actual samples read: %d\n", len(audioData.LeftChannel))
	fmt.Printf("Actual duration: %.2f seconds\n", actualDuration)

	if len(audioData.LeftChannel) == 0 {
		return nil, fmt.Errorf("no audio data found in file")
	}

	return audioData, nil

	// numSamples = int(header.SubChunk2Size) / int(header.BlockAlign)

	// for i := 0; i < numSamples; i++ {
	// 	var leftSample, rightSample int16

	// 	if err := binary.Read(file, binary.LittleEndian, &leftSample); err != nil {
	// 		if err == io.EOF {
	// 			break
	// 		}
	// 		return nil, fmt.Errorf("failed to read left sample: %w", err)
	// 	}

	// 	if err := binary.Read(file, binary.LittleEndian, &rightSample); err != nil {
	// 		if err == io.EOF {
	// 			break
	// 		}
	// 		return nil, fmt.Errorf("failed to read right sample: %w", err)
	// 	}

	// 	// Convert to float64 and normalize to [-1, 1]
	// 	audioData.LeftChannel = append(audioData.LeftChannel, float64(leftSample)/32767.0)
	// 	audioData.RightChannel = append(audioData.RightChannel, float64(rightSample)/32767.0)
	// }

	// fmt.Printf("SampleRate: %d\n", header.SampleRate)
	// fmt.Printf("NumChannels: %d\n", header.NumChannels)
	// fmt.Printf("BitsPerSample: %d\n", header.BitsPerSample)
	// fmt.Printf("Subchunk2Size: %d bytes\n", header.SubChunk2Size)
	// fmt.Printf("BlockAlign: %d bytes\n", header.BlockAlign)

	// duration := float64(header.SubChunk2Size) / float64(header.SampleRate*uint32(header.BlockAlign))
	// fmt.Printf("Calculated Duration: %.2f seconds\n", duration)

	// return audioData, nil
}

// generateWaveformImage creates a waveform image from audio samples
func generateWaveformImage(samples []float64, width, height int, filename string) error {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill background with white
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{255, 255, 255, 255})
		}
	}

	if len(samples) == 0 {
		return fmt.Errorf("no audio samples to process")
	}

	// Calculate samples per pixel
	samplesPerPixel := len(samples) / width
	if samplesPerPixel == 0 {
		samplesPerPixel = 1
	}

	centerY := height / 2
	maxAmplitude := float64(height) / 2.0

	// Draw waveform
	for x := 0; x < width; x++ {
		startSample := x * samplesPerPixel
		endSample := startSample + samplesPerPixel
		if endSample > len(samples) {
			endSample = len(samples)
		}

		// Find min and max amplitude in this pixel range
		var minAmp, maxAmp float64
		for i := startSample; i < endSample; i++ {
			amp := samples[i]
			if i == startSample || amp < minAmp {
				minAmp = amp
			}
			if i == startSample || amp > maxAmp {
				maxAmp = amp
			}
		}

		// Convert amplitude to pixel coordinates
		minY := centerY - int(minAmp*maxAmplitude)
		maxY := centerY - int(maxAmp*maxAmplitude)

		// Clamp values
		if minY < 0 {
			minY = 0
		}
		if minY >= height {
			minY = height - 1
		}
		if maxY < 0 {
			maxY = 0
		}
		if maxY >= height {
			maxY = height - 1
		}

		// Ensure maxY >= minY
		if maxY < minY {
			minY, maxY = maxY, minY
		}

		// Draw vertical line from minY to maxY
		for y := minY; y <= maxY; y++ {
			img.Set(x, y, color.RGBA{0, 0, 0, 255}) // Black waveform
		}
	}

	// Save image
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create image file: %w", err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		return fmt.Errorf("failed to encode PNG: %w", err)
	}

	return nil
}
