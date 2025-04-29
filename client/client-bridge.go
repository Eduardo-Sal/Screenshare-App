// client-bridge.go
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
	signalURL     = flag.String("signal", "ws://localhost:8000/ws", "WebSocket signaling server URL")
	turnServerURL = flag.String("turn", "", "TURN server URL (e.g., turn:host:3478)")
	turnUser      = flag.String("turn-user", "", "TURN username")
	turnPass      = flag.String("turn-pass", "", "TURN password")
)

var (
	wsMu              sync.Mutex
	pendingCandidates []webrtc.ICECandidateInit
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
	log.Printf("Connected to signaling server at %s", *signalURL)

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

	peerConn, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Fatalf("Error creating PeerConnection: %v", err)
	}

	// 3) Send ICE candidates to browser
	peerConn.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		safeWriteJSON(ws, map[string]interface{}{"type": "ice-candidate", "candidate": c.ToJSON()})
	})

	// 4) Handle incoming DataChannel from browser
	peerConn.OnDataChannel(func(d *webrtc.DataChannel) {
		log.Println("DataChannel created by browser:", d.Label())
		d.OnOpen(func() {
			log.Println("🔗 DataChannel open - streaming frames...")
			for {
				// Capture framebuffer screenshot
				if err := exec.Command("fbgrab", "/tmp/frame.png").Run(); err != nil {
					log.Printf("Screenshot error: %v", err)
					time.Sleep(time.Second)
					continue
				}

				// Read image file
				data, err := os.ReadFile("/tmp/frame.png")
				if err != nil {
					log.Printf("Read file error: %v", err)
					time.Sleep(time.Second)
					continue
				}

				// Send over WebRTC
				if err := d.Send(data); err != nil {
					log.Printf("Send frame error: %v", err)
					return
				}

				time.Sleep(1 * time.Second)
			}
		})
	})

	// 5) Handle signaling messages
	go func() {
		for {
			var msg map[string]interface{}
			if err := ws.ReadJSON(&msg); err != nil {
				log.Printf("Signaling error: %v", err)
				return
			}

			switch msg["type"] {
			case "offer":
				offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: msg["sdp"].(string)}
				if err := peerConn.SetRemoteDescription(offer); err != nil {
					log.Fatalf("SetRemoteDescription error: %v", err)
				}
				// flush buffered ICE candidates
				for _, c := range pendingCandidates {
					if err := peerConn.AddICECandidate(c); err != nil {
						log.Printf("Flush ICE error: %v", err)
					}
				}
				pendingCandidates = nil

				// create answer
				answer, err := peerConn.CreateAnswer(nil)
				if err != nil {
					log.Fatalf("CreateAnswer error: %v", err)
				}
				if err := peerConn.SetLocalDescription(answer); err != nil {
					log.Fatalf("SetLocalDescription error: %v", err)
				}

				// wait for ICE gathering to finish
				<-webrtc.GatheringCompletePromise(peerConn)
				safeWriteJSON(ws, map[string]interface{}{"type": "answer", "sdp": peerConn.LocalDescription().SDP})

			case "ice-candidate":
				cand := msg["candidate"].(map[string]interface{})
				sdpMid := cand["sdpMid"].(string)
				sdpMLine := uint16(cand["sdpMLineIndex"].(float64))
				ci := webrtc.ICECandidateInit{Candidate: cand["candidate"].(string), SDPMid: &sdpMid, SDPMLineIndex: &sdpMLine}
				// buffer or add remotely
				if peerConn.RemoteDescription() == nil {
					pendingCandidates = append(pendingCandidates, ci)
				} else {
					if err := peerConn.AddICECandidate(ci); err != nil {
						log.Printf("AddICECandidate error: %v", err)
					}
				}

			default:
				log.Printf("Unknown type: %v", msg["type"])
			}
		}
	}()

	select {}
}
