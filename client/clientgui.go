package main

import (
	"bytes"
	"fmt"
	"image"
	"log"
	"net/url"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

func main() {
	piIP := "4.227.177.31" // Replace with your signaling server IP

	a := app.New()
	w := a.NewWindow("Screenshare Viewer")
	status := widget.NewLabel("Connecting…")
	img := canvas.NewImageFromResource(nil)
	img.FillMode = canvas.ImageFillContain

	w.SetContent(container.NewVBox(img, status))
	w.Resize(fyne.NewSize(640, 480))
	w.Show()

	go func() {
		err := connectAndStream(piIP, func(frame []byte) {
			log.Printf("🖼️ Received frame: %d bytes", len(frame))

			i, _, err := image.Decode(bytes.NewReader(frame))
			if err != nil {
				log.Println("Image decode error:", err)
				return
			}
			img.Image = i
			img.Refresh()
		}, func(s string) {
			log.Println("📺 Status:", s)
			status.SetText(s)
		})
		if err != nil {
			log.Println(" Fatal error:", err)
			status.SetText("Error: " + err.Error())
			os.Exit(1)
		}
	}()

	a.Run()
}

func connectAndStream(ip string, onFrame func([]byte), onStatus func(string)) error {
	u := url.URL{Scheme: "ws", Host: ip + ":8000", Path: "/ws"}
	log.Println("🔌 Connecting to WebSocket at", u.String())

	ws, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("WebSocket error: %w", err)
	}
	defer ws.Close()
	onStatus("WebSocket connected")

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
			{URLs: []string{fmt.Sprintf("turn:%s:3478", ip)}, Username: "user", Credential: "pass"},
		},
	})
	if err != nil {
		return fmt.Errorf("PeerConnection error: %w", err)
	}

	dc, err := pc.CreateDataChannel("media", nil)
	if err != nil {
		return fmt.Errorf("DataChannel error: %w", err)
	}

	dc.OnOpen(func() {
		log.Println("DataChannel opened")
		onStatus("Streaming…")
	})
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		log.Printf("📥 DataChannel received %d bytes", len(msg.Data))
		onFrame(msg.Data)
	})

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			log.Println("Sending ICE candidate")
			_ = ws.WriteJSON(map[string]any{"type": "ice-candidate", "candidate": c.ToJSON()})
		}
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		return fmt.Errorf("Offer error: %w", err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		return fmt.Errorf("SetLocalDescription error: %w", err)
	}

	log.Println("📡 Sending SDP offer")
	<-webrtc.GatheringCompletePromise(pc)
	_ = ws.WriteJSON(map[string]string{
		"type": "offer",
		"sdp":  pc.LocalDescription().SDP,
	})

	for {
		var msg map[string]any
		if err := ws.ReadJSON(&msg); err != nil {
			return err
		}
		log.Printf("🔔 Received message: %v", msg["type"])
		switch msg["type"] {
		case "answer":
			sdp := msg["sdp"].(string)
			if err := pc.SetRemoteDescription(webrtc.SessionDescription{
				Type: webrtc.SDPTypeAnswer,
				SDP:  sdp,
			}); err != nil {
				log.Println("SetRemoteDescription error:", err)
			} else {
				log.Println("Remote description set")
			}
		case "ice-candidate":
			cand := msg["candidate"].(map[string]any)
			candidateStr := cand["candidate"].(string)
			sdpMid := cand["sdpMid"].(string)
			sdpIdx := uint16(cand["sdpMLineIndex"].(float64))
			err := pc.AddICECandidate(webrtc.ICECandidateInit{
				Candidate:     candidateStr,
				SDPMid:        &sdpMid,
				SDPMLineIndex: &sdpIdx,
			})
			if err != nil {
				log.Println("ICE candidate error:", err)
			} else {
				log.Println("ICE candidate added")
			}
		default:
			log.Printf("Unknown signal type: %v", msg["type"])
		}
	}
}
