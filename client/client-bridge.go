// client/client-bridge.go
//
// WebRTC client-bridge: connects to a signaling server, negotiates ICE/SDP,
// and once the DataChannel opens, captures and sends screenshots at 1 FPS.
//
// This version buffers any incoming ICE candidates until after the remote
// description (offer) has been set, eliminating "remote description not set" errors.

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

var (
	signalURL     = flag.String("signal", "ws://localhost:8000/ws", "WebSocket signaling server URL")
	turnServerURL = flag.String("turn", "", "TURN server URL (e.g., turn:host:3478)")
	turnUser      = flag.String("turn-user", "", "TURN username")
	turnPass      = flag.String("turn-pass", "", "TURN password")
)

var (
	wsMu            sync.Mutex
	candidateBuffer []webrtc.ICECandidateInit
)

func safeWriteJSON(ws *websocket.Conn, v interface{}) error {
	wsMu.Lock()
	defer wsMu.Unlock()
	return ws.WriteJSON(v)
}

func main() {
	flag.Parse()

	// 1) Connect to signaling server
	ws, _, err := websocket.DefaultDialer.Dial(*signalURL, nil)
	if err != nil {
		log.Fatalf("Could not connect to signaling server: %v", err)
	}
	defer ws.Close()
	log.Printf("🔗 Connected to signaling server at %s", *signalURL)

	// 2) Prepare WebRTC configuration
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	}
	if *turnServerURL != "" {
		config.ICEServers = append(config.ICEServers, webrtc.ICEServer{
			URLs:       []string{*turnServerURL},
			Username:   *turnUser,
			Credential: *turnPass,
		})
	}

	// 3) Create PeerConnection
	peerConn, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Fatalf("Error creating PeerConnection: %v", err)
	}

	// 4) Send our ICE candidates to the remote peer (via signaling)
	peerConn.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		safeWriteJSON(ws, map[string]interface{}{
			"type":      "ice-candidate",
			"candidate": c.ToJSON(),
		})
	})

	// 5) Set up DataChannel handler: when remote opens "media", start sending frames
	peerConn.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Printf("📺 DataChannel '%s' created by remote", dc.Label())

		dc.OnOpen(func() {
			log.Println("▶️ DataChannel open – streaming frames")
			for {
				// Capture screen to a temporary PNG
				if err := exec.Command("fbgrab", "/tmp/frame.png").Run(); err != nil {
					log.Printf("Capture error: %v", err)
					time.Sleep(time.Second)
					continue
				}

				data, err := os.ReadFile("/tmp/frame.png")
				if err != nil {
					log.Printf("Read error: %v", err)
					time.Sleep(time.Second)
					continue
				}

				if err := dc.Send(data); err != nil {
					log.Printf("Send error: %v", err)
					return
				}

				time.Sleep(time.Second) // ~1 FPS
			}
		})
	})

	// 6) Read signaling messages
	for {
		var msg map[string]interface{}
		if err := ws.ReadJSON(&msg); err != nil {
			log.Printf("Signaling read error: %v", err)
			return
		}
		switch msg["type"] {
		case "offer":
			// Decode the remote SDP offer
			offer := webrtc.SessionDescription{
				Type: webrtc.SDPTypeOffer,
				SDP:  msg["sdp"].(string),
			}

			// 6a) Apply remote description
			if err := peerConn.SetRemoteDescription(offer); err != nil {
				log.Fatalf("SetRemoteDescription error: %v", err)
			}

			// 6b) Flush any buffered ICE candidates
			for _, c := range candidateBuffer {
				if err := peerConn.AddICECandidate(c); err != nil {
					log.Printf("Error flushing candidate: %v", err)
				}
			}
			candidateBuffer = nil

			// 6c) Create and send SDP answer
			answer, err := peerConn.CreateAnswer(nil)
			if err != nil {
				log.Fatalf("CreateAnswer error: %v", err)
			}
			if err := peerConn.SetLocalDescription(answer); err != nil {
				log.Fatalf("SetLocalDescription error: %v", err)
			}
			go func() {
				<-webrtc.GatheringCompletePromise(peerConn)
				safeWriteJSON(ws, map[string]interface{}{
					"type": "answer",
					"sdp":  peerConn.LocalDescription().SDP,
				})
			}()

		case "answer":
			log.Println("(Unexpected answer received (server-to-server only)")

		case "ice-candidate":
			// Decode ICE candidate from signaling
			cand := msg["candidate"].(map[string]interface{})
			sdpMid := cand["sdpMid"].(string)
			sdpMLineIndex := uint16(cand["sdpMLineIndex"].(float64))
			ci := webrtc.ICECandidateInit{
				Candidate:     cand["candidate"].(string),
				SDPMid:        &sdpMid,
				SDPMLineIndex: &sdpMLineIndex,
			}
			// Buffer if remote description not yet set
			if peerConn.RemoteDescription() == nil {
				candidateBuffer = append(candidateBuffer, ci)
			} else {
				if err := peerConn.AddICECandidate(ci); err != nil {
					log.Printf("AddICECandidate error: %v", err)
				}
			}

		default:
			log.Printf("Unknown signal type: %v", msg["type"])
		}
	}
}
