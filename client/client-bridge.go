// client/client-bridge.go
//
// WebRTC client‐bridge with verbose debug logs:
//  - Signaling connects, ICE candidates send/receive
//  - DataChannel opens and logs each capture/read/send
//  - Helps trace exactly why frames might not flow

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
	log.Printf("SIGNAL → %T %+v", v, v)
	return ws.WriteJSON(v)
}

func main() {
	flag.Parse()

	// 1) Connect to signaling server
	log.Printf("🔌 Dialing signaling server at %s …", *signalURL)
	ws, _, err := websocket.DefaultDialer.Dial(*signalURL, nil)
	if err != nil {
		log.Fatalf("Could not connect to signaling server: %v", err)
	}
	defer ws.Close()
	log.Printf("Connected to signaling server at %s", *signalURL)

	// 2) Build WebRTC config
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}},
	}
	if *turnServerURL != "" {
		config.ICEServers = append(config.ICEServers, webrtc.ICEServer{
			URLs:       []string{*turnServerURL},
			Username:   *turnUser,
			Credential: *turnPass,
		})
		log.Printf("Added TURN: %s (user=%s)", *turnServerURL, *turnUser)
	}

	// 3) Create PeerConnection
	peerConn, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Fatalf("Error creating PeerConnection: %v", err)
	}
	log.Println("WebRTC PeerConnection created")

	// 4) Outbound ICE candidates
	peerConn.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		log.Println("ICE candidate gathered")
		safeWriteJSON(ws, map[string]interface{}{
			"type":      "ice-candidate",
			"candidate": c.ToJSON(),
		})
	})

	// 5) Handle incoming DataChannel (viewer side)
	peerConn.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Printf("DataChannel '%s' created by remote", dc.Label())

		dc.OnOpen(func() {
			log.Println("DataChannel open – starting frame loop")
		})
		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			// The bridge is sending, so ignore incoming here.
			log.Printf("Unexpected DataChannel message (len=%d)", len(msg.Data))
		})

		// Start sending screenshots
		go func() {
			for {
				log.Println(" Capturing frame")
				cmd := exec.Command("fbgrab", "/tmp/frame.png")
				if err := cmd.Run(); err != nil {
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
				log.Printf("Frame read: %d bytes", len(data))

				if err := dc.Send(data); err != nil {
					log.Printf("Send error: %v", err)
					return
				}
				log.Printf("Frame sent: %d bytes", len(data))

				time.Sleep(time.Second) // ~1 FPS
			}
		}()
	})

	// 6) Signaling loop (receive Offer, send Answer, handle ICE)
	for {
		var msg map[string]interface{}
		if err := ws.ReadJSON(&msg); err != nil {
			log.Printf("Signaling read error: %v", err)
			return
		}
		log.Printf("🔔 SIGNAL ← %v", msg["type"])

		switch msg["type"] {
		case "offer":
			// Apply remote SDP offer
			offer := webrtc.SessionDescription{
				Type: webrtc.SDPTypeOffer,
				SDP:  msg["sdp"].(string),
			}
			log.Println("Applying remote SDP offer")
			if err := peerConn.SetRemoteDescription(offer); err != nil {
				log.Fatalf("SetRemoteDescription error: %v", err)
			}

			// Flush buffered ICE
			for _, c := range candidateBuffer {
				if err := peerConn.AddICECandidate(c); err != nil {
					log.Printf("Buffer flush error: %v", err)
				} else {
					log.Println("Buffered ICE flushed")
				}
			}
			candidateBuffer = nil

			// Create and send answer
			log.Println("Creating SDP answer")
			answer, err := peerConn.CreateAnswer(nil)
			if err != nil {
				log.Fatalf("CreateAnswer error: %v", err)
			}
			if err := peerConn.SetLocalDescription(answer); err != nil {
				log.Fatalf("SetLocalDescription error: %v", err)
			}
			go func() {
				<-webrtc.GatheringCompletePromise(peerConn)
				log.Println("Sending SDP answer")
				safeWriteJSON(ws, map[string]interface{}{
					"type": "answer",
					"sdp":  peerConn.LocalDescription().SDP,
				})
			}()

		case "ice-candidate":
			// Incoming ICE
			cand := msg["candidate"].(map[string]interface{})
			sdpMid := cand["sdpMid"].(string)
			sdpMLineIndex := uint16(cand["sdpMLineIndex"].(float64))
			ci := webrtc.ICECandidateInit{
				Candidate:     cand["candidate"].(string),
				SDPMid:        &sdpMid,
				SDPMLineIndex: &sdpMLineIndex,
			}
			if peerConn.RemoteDescription() == nil {
				log.Println("🗄️  Buffering ICE candidate")
				candidateBuffer = append(candidateBuffer, ci)
			} else {
				log.Println("Adding ICE candidate")
				if err := peerConn.AddICECandidate(ci); err != nil {
					log.Printf("AddICECandidate error: %v", err)
				}
			}

		case "answer":
			log.Println("Unexpected 'answer' from viewer")

		default:
			log.Printf("Unknown signal type: %v", msg["type"])
		}
	}
}
