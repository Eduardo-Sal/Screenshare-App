package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

const (
	port    = "8080" // TCP port to listen on
	fps     = 1      // Frames per second
	imgW    = 640    // Width of dummy image
	imgH    = 480    // Height of dummy image
	quality = 80     // JPEG quality (0–100)
)

// getPublicIP queries a public service to discover the Pi's external IP.
func getPublicIP() (string, error) {
	resp, err := http.Get("https://api.ipify.org")
	if err != nil {
		return "", fmt.Errorf("could not fetch public IP: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("could not read IP response: %w", err)
	}
	return string(data), nil
}

// generateDummyFrame creates a JPEG-encoded solid-color image.
// The color cycles based on the current second for visual feedback.
func generateDummyFrame() ([]byte, error) {
	// Make a new RGBA image
	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))

	// Pick a color that changes every second
	sec := time.Now().Second()
	col := color.RGBA{uint8(sec * 4), uint8(255 - sec*4), 128, 255}

	// Fill the entire image with that color
	for y := 0; y < imgH; y++ {
		for x := 0; x < imgW; x++ {
			img.Set(x, y, col)
		}
	}

	// Encode to JPEG
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	if err != nil {
		return nil, fmt.Errorf("jpeg encode error: %w", err)
	}
	return buf.Bytes(), nil
}

// handleConnection streams dummy frames to a connected client.
// Each frame is prefixed with a 4-byte big-endian length header.
func handleConnection(conn net.Conn) {
	defer conn.Close()
	clientAddr := conn.RemoteAddr().String()
	log.Printf("👥 Client connected: %s\n", clientAddr)

	ticker := time.NewTicker(time.Second / time.Duration(fps))
	defer ticker.Stop()

	for range ticker.C {
		frame, err := generateDummyFrame()
		if err != nil {
			log.Printf("Frame generation error: %v\n", err)
			return
		}

		// Write length prefix
		if err := binary.Write(conn, binary.BigEndian, uint32(len(frame))); err != nil {
			log.Printf("Error writing length to %s: %v\n", clientAddr, err)
			return
		}

		// Write JPEG data
		if _, err := conn.Write(frame); err != nil {
			log.Printf("Error sending frame to %s: %v\n", clientAddr, err)
			return
		}
	}

	log.Printf("Client disconnected: %s\n", clientAddr)
}

func main() {
	// 1) Print public IP so user knows what to connect to
	ip, err := getPublicIP()
	if err != nil {
		log.Fatalf("%v\n", err)
	}
	fmt.Printf("Raspberry Pi Streamer\n Public IP: %s:%s\n", ip, port)

	// 2) Start TCP listener
	ln, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Could not start TCP listener: %v\n", err)
	}
	defer ln.Close()
	log.Printf("Listening on port %s (WAN)\n", port)

	// 3) Accept and handle clients
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Accept error: %v\n", err)
			continue
		}
		go handleConnection(conn)
	}
}
