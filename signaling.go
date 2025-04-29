package main

import (
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var (
	upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	clients  = make(map[*websocket.Conn]bool)
	mu       sync.Mutex
)

func handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer ws.Close()

	mu.Lock()
	clients[ws] = true
	mu.Unlock()
	log.Printf("Client connected: %s", ws.RemoteAddr())

	for {
		mt, msg, err := ws.ReadMessage()
		if err != nil {
			break
		}
		mu.Lock()
		for c := range clients {
			if c != ws {
				c.WriteMessage(mt, msg)
			}
		}
		mu.Unlock()
	}

	mu.Lock()
	delete(clients, ws)
	mu.Unlock()
	log.Printf("Client disconnected: %s", ws.RemoteAddr())
}

func main() {
	http.HandleFunc("/ws", handleWS)
	log.Println("Signaling server listening on :8000/ws")
	log.Fatal(http.ListenAndServe(":8000", nil))
}
