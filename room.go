package main

import (
	"log"
	"net/http"

	"github.com/sgravitz/chat/trace"
	"github.com/stretchr/objx"

	"github.com/gorilla/websocket"
)

type room struct {

	// forward is a channel that holds incoming messages
	// that should be forwarded to other clients
	forward chan *message
	// join is a channel for on-boarding new clients
	join chan *client
	// leave is a channel for leaving a room
	leave chan *client
	// clients holds all current clients
	clients map[*client]bool
	// tracer will receive trace information of activity in the room.
	tracer trace.Tracer
}

func newRoom() *room {
	return &room{
		forward: make(chan *message),
		join:    make(chan *client),
		leave:   make(chan *client),
		clients: make(map[*client]bool),
		tracer:  trace.Off(),
	}
}

func (r *room) run() {
	for {
		select {
		case client := <-r.join:
			// joining
			r.clients[client] = true
			r.tracer.Trace("New client joined")
		case client := <-r.leave:
			// leaving
			delete(r.clients, client)
			close(client.send)
			r.tracer.Trace("client left")
		case msg := <-r.forward:
			// forward msg to clients
			for client := range r.clients {
				select {
				case client.send <- msg:
					// send the messages
					r.tracer.Trace("-- send to clients --> ", string(msg.Message))
				default:
					// failed to send
					delete(r.clients, client)
					close(client.send)
					r.tracer.Trace("-- failed to send, cleaned up client")
				}
			}
		}
	}
}

const (
	socketBufferSize  = 1024
	messageBufferSize = 256
)

var upgrader = &websocket.Upgrader{ReadBufferSize: socketBufferSize, WriteBufferSize: socketBufferSize}

func (r *room) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	socket, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		log.Fatal("ServerHTTP:", err)
		return
	}
	authCookie, err := req.Cookie("auth")
	if err != nil {
		log.Fatal("Failed to get auth Cookie:", err)
		return
	}

	client := &client{
		socket:   socket,
		send:     make(chan *message, messageBufferSize),
		room:     r,
		userData: objx.MustFromBase64(authCookie.Value),
	}
	r.join <- client
	defer func() { r.leave <- client }()
	go client.write()
	client.read()
}
