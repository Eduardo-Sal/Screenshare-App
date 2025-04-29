package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

// Flags
var (
	piAddr        = flag.String("addr", "", "Raspberry Pi streamer address (ip:port)") // not used anymore
	signalURL     = flag.String("signal", "ws://localhost:8000/ws", "WebSocket signaling server URL")
	turnServerURL = flag.String("turn", "", "TURN server URL (e.g., turn:host:3478)")
	turnUser      = flag.String("turn-user", "", "TURN username")
	turnPass      = flag.String("turn-pass", "", "TURN password")
)

// Mutex for WebSocket writes
var wsMu sync.Mutex

func safeWriteJSON(ws *websocket.Conn, v interface{}) error {
	wsMu.Lock()
	defer wsMu.Unlock()
	return ws.WriteJSON(v)
}

func main() {
	flag.Parse()

	// Connect to signaling server
	ws, _, err := websocket.DefaultDialer.Dial(*signalURL, nil)
	if err != nil {
		log.Fatalf("Could not connect to signaling server: %v", err)
	}
	defer ws.Close()
	log.Printf("Connected to signaling server at %s", *signalURL)

	// WebRTC config
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}
	if *turnServerURL != "" {
		config.ICEServers = append(config.ICEServers, webrtc.ICEServer{
			URLs:       []string{*turnServerURL},
			Username:   *turnUser,
			Credential: *turnPass,
		})
	}

	peerConn, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Fatalf("Error creating PeerConnection: %v", err)
	}

	// Create DataChannel for JPEG
	dc, err := peerConn.CreateDataChannel("media", nil)
	if err != nil {
		log.Fatalf("Error creating DataChannel: %v", err)
	}

	// ICE candidates
	peerConn.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		safeWriteJSON(ws, map[string]interface{}{
			"type":      "ice-candidate",
			"candidate": c.ToJSON(),
		})
	})

	// WebSocket signaling handler
	go func() {
		for {
			var msg map[string]interface{}
			if err := ws.ReadJSON(&msg); err != nil {
				log.Printf("Signaling read error: %v", err)
				return
			}
			switch msg["type"] {
			case "offer":
				offer := webrtc.SessionDescription{
					Type: webrtc.SDPTypeOffer,
					SDP:  msg["sdp"].(string),
				}
				if err := peerConn.SetRemoteDescription(offer); err != nil {
					log.Fatalf("SetRemoteDescription error: %v", err)
				}
				answer, err := peerConn.CreateAnswer(nil)
				if err != nil {
					log.Fatalf("CreateAnswer error: %v", err)
				}
				if err := peerConn.SetLocalDescription(answer); err != nil {
					log.Fatalf("SetLocalDescription error: %v", err)
				}
				safeWriteJSON(ws, map[string]interface{}{
					"type": "answer",
					"sdp":  answer.SDP,
				})

			case "answer":
				log.Println("Received answer from peer")
				ans := webrtc.SessionDescription{
					Type: webrtc.SDPTypeAnswer,
					SDP:  msg["sdp"].(string),
				}
				if err := peerConn.SetRemoteDescription(ans); err != nil {
					log.Fatalf("SetRemoteDescription(answer) error: %v", err)
				}

			case "ice-candidate":
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

	// Once DataChannel is open, start sending screenshots
	dc.OnOpen(func() {
		log.Println("🔗 DataChannel 'media' open - streaming frames...")

		for {
			// Capture a screenshot
			cmd := exec.Command("fbgrab", "/tmp/frame.png")
			if err := cmd.Run(); err != nil {
				log.Printf("Failed to capture screenshot: %v", err)
				time.Sleep(time.Second)
				continue
			}

			// Open the screenshot
			data, err := os.ReadFile("/tmp/frame.png")
			if err != nil {
				log.Printf("Failed to read screenshot: %v", err)
				time.Sleep(time.Second)
				continue
			}

			// Send screenshot over DataChannel
			if err := dc.Send(data); err != nil {
				log.Printf("Error sending frame: %v", err)
				return
			}

			time.Sleep(1 * time.Second) // adjust fps here (currently 1fps)
		}
	})

	select {}
}
