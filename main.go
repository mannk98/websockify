package main

import (
	"flag"
	"io"
	"log"
	"net"
	"net/http"

	"golang.org/x/net/websocket"
)

var (
	listenAddr = flag.String("listen", ":6080", "address to listen on")
	targetAddr = flag.String("target", "localhost:5900", "address to proxy to")
	webDir     = flag.String("web", "", "directory to serve static files from")
)

func main() {
	flag.Parse()

	if *webDir != "" {
		http.Handle("/", http.FileServer(http.Dir(*webDir)))
	}

	http.Handle("/websockify", websocket.Handler(handleWss))

	log.Printf("Listening on %s, proxying to %s", *listenAddr, *targetAddr)
	if *webDir != "" {
		log.Printf("Serving static files from %s", *webDir)
	}

	err := http.ListenAndServe(*listenAddr, nil)
	if err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}

func handleWss(wsconn *websocket.Conn) {
	log.Println("New connection from", wsconn.Request().RemoteAddr)

	conn, err := net.Dial("tcp", *targetAddr)
	if err != nil {
		log.Println("Error connecting to target:", err)
		wsconn.Close()
		return
	}
	defer conn.Close()
	defer wsconn.Close()

	wsconn.PayloadType = websocket.BinaryFrame

	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(conn, wsconn)
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(wsconn, conn)
		errCh <- err
	}()

	<-errCh
	log.Println("Connection closed")
}
