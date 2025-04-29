// client-bridge.go
// Raspberry Pi WebRTC client using pion/webrtc and WebSocket signaling.

package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"io"
	"log"
	"net"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

// Flags
var (
	piAddr        = flag.String("addr", "", "Raspberry Pi streamer address (ip:port)")
	signalURL     = flag.String("signal", "ws://localhost:8000/ws", "WebSocket signaling server URL")
	turnServerURL = flag.String("turn", "", "TURN server URL (e.g., turn:host:3478)")
	turnUser      = flag.String("turn-user", "", "TURN username")
	turnPass      = flag.String("turn-pass", "", "TURN password")
)

func main() {
	flag.Parse()
	if *piAddr == "" {
		log.Fatal("Missing -addr flag, e.g. -addr=192.168.1.10:8080")
	}

	// 1) Connect to Pi TCP streamer
	piConn, err := net.Dial("tcp", *piAddr)
	if err != nil {
		log.Fatalf("Could not connect to Pi streamer: %v", err)
	}
	log.Printf("Connected to Pi streamer at %s", *piAddr)

	// 2) Connect to signaling server
	ws, _, err := websocket.DefaultDialer.Dial(*signalURL, nil)
	if err != nil {
		log.Fatalf("Could not connect to signaling server: %v", err)
	}
	defer ws.Close()
	log.Printf("Connected to signaling server at %s", *signalURL)

	// 3) Prepare WebRTC configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}}, // public STUN
		},
	}
	if *turnServerURL != "" {
		config.ICEServers = append(config.ICEServers, webrtc.ICEServer{
			URLs:       []string{*turnServerURL},
			Username:   *turnUser,
			Credential: *turnPass,
		})
	}

	// 4) Create PeerConnection
	peerConn, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Fatalf("Error creating PeerConnection: %v", err)
	}

	// 5) Create a reliable DataChannel for JPEG frames
	dc, err := peerConn.CreateDataChannel("media", nil)
	if err != nil {
		log.Fatalf("Error creating DataChannel: %v", err)
	}

	// When new ICE candidate is found → send to signaling
	peerConn.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		ws.WriteJSON(map[string]interface{}{
			"type":      "ice-candidate",
			"candidate": c.ToJSON(),
		})
	})

	// 6) Handle incoming signaling messages
	go func() {
		for {
			var msg map[string]interface{}
			if err := ws.ReadJSON(&msg); err != nil {
				log.Printf("Signaling read error: %v", err)
				return
			}
			switch msg["type"] {
			case "offer":
				// Set remote description
				offer := webrtc.SessionDescription{
					Type: webrtc.SDPTypeOffer,
					SDP:  msg["sdp"].(string),
				}
				if err := peerConn.SetRemoteDescription(offer); err != nil {
					log.Fatalf("SetRemoteDescription error: %v", err)
				}

				// Create and send answer
				answer, err := peerConn.CreateAnswer(nil)
				if err != nil {
					log.Fatalf("CreateAnswer error: %v", err)
				}
				if err := peerConn.SetLocalDescription(answer); err != nil {
					log.Fatalf("SetLocalDescription error: %v", err)
				}
				ws.WriteJSON(map[string]interface{}{
					"type": "answer",
					"sdp":  answer.SDP,
				})

			case "answer":
				// If, for some reason, Pi ever sends us an answer:
				log.Println("Received answer from peer")
				ans := webrtc.SessionDescription{
					Type: webrtc.SDPTypeAnswer,
					SDP:  msg["sdp"].(string),
				}
				if err := peerConn.SetRemoteDescription(ans); err != nil {
					log.Fatalf("SetRemoteDescription(answer) error: %v", err)
				}

			case "ice-candidate":
				// Parse and add ICE candidate
				cand := msg["candidate"].(map[string]interface{})
				sdpMid := cand["sdpMid"].(string)
				sdpMLine := uint16(cand["sdpMLineIndex"].(float64))

				ci := webrtc.ICECandidateInit{
					Candidate:     cand["candidate"].(string),
					SDPMid:        &sdpMid,
					SDPMLineIndex: &sdpMLine,
				}
				if err := peerConn.AddICECandidate(ci); err != nil {
					log.Printf("AddICECandidate error: %v", err)
				}

			default:
				log.Printf("Unknown signal type: %v", msg["type"])
			}
		}
	}()

	// 7) Once DataChannel is open, start bridging Pi frames
	dc.OnOpen(func() {
		log.Println("🔗 DataChannel 'media' open - streaming frames...")
		reader := bufio.NewReader(piConn)
		for {
			var length uint32
			if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
				if err == io.EOF {
					log.Println("Pi streamer closed connection")
				}
				return
			}
			buf := make([]byte, length)
			if _, err := io.ReadFull(reader, buf); err != nil {
				log.Fatalf("Error reading frame data: %v", err)
			}
			dc.Send(buf)
			time.Sleep(time.Second) // match ~1 fps
		}
	})

	// Block forever
	select {}
}
