package main

import (
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/websocket"
)

type appConfig struct {
	targetAddr string
	runOnce    bool
	webServer  bool
}

var (
	fileHandler   http.Handler
	shouldExit    bool
	config        appConfig
	logger        *log.Logger
	verboseLogger *log.Logger
)

func ws(w http.ResponseWriter, r *http.Request) {
	if shouldExit {
		return
	}

	// Serve static files if webServer is enabled and no WebSocket upgrade
	if config.webServer {
		if header := r.Header.Get("Connection"); header == "" || !strings.Contains(strings.ToLower(header), "upgrade") {
			verboseLogger.Println("Serving file", r.URL)
			fileHandler.ServeHTTP(w, r)
			return
		}
	}

	// Upgrade to WebSocket
	if config.runOnce {
		shouldExit = true
		defer func() {
			logger.Println("Run once! Exiting...")
			os.Exit(0)
		}()
	}

	upgrader := websocket.Upgrader{
		Subprotocols: []string{"binary"}, // Support binary data like websockify
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for testing; replace with specific origins in production
			// Example: return r.Header.Get("Origin") == "http://localhost:8080"
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Printf("Error upgrading to WebSocket: %v", err)
		return
	}
	verboseLogger.Printf("Received connection from %s", conn.RemoteAddr())
	defer conn.Close()

	// Dial target TCP
	tcpConn, err := net.Dial("tcp", config.targetAddr)
	if err != nil {
		logger.Printf("Error connecting to target %s: %v", config.targetAddr, err)
		return
	}
	defer tcpConn.Close()

	// TCP to WebSocket
	go func() {
		defer verboseLogger.Printf("Closed TCP to WS connection from %s", conn.RemoteAddr())
		defer conn.Close()
		defer tcpConn.Close()
		buf := make([]byte, 1024)
		for {
			n, err := tcpConn.Read(buf)
			if err != nil {
				if err != io.EOF {
					logger.Printf("TCP read error: %v", err)
				}
				return
			}
			if n == 0 {
				continue
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				logger.Printf("WebSocket write error: %v", err)
				return
			}
		}
	}()

	// WebSocket to TCP
	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			if err != websocket.ErrCloseSent {
				logger.Printf("WebSocket read error: %v", err)
			}
			return
		}
		if msgType != websocket.BinaryMessage {
			logger.Println("Non-binary message received")
			continue
		}
		if _, err := tcpConn.Write(msg); err != nil {
			logger.Printf("TCP write error: %v", err)
			return
		}
	}
}

func main() {
	helpFlag := flag.Bool("h", false, "Print help")
	verboseFlag := flag.Bool("v", false, "Enable verbose logging")
	cert := flag.String("cert", "", "SSL certificate file")
	key := flag.String("key", "", "SSL private key file")
	webDir := flag.String("web", "", "Serve files from DIR")
	runOnceFlag := flag.Bool("run-once", false, "Handle a single WebSocket connection and exit")
	flag.Parse()

	if *helpFlag {
		flag.PrintDefaults()
		return
	}

	// Initialize loggers
	logger = log.New(os.Stdout, "", log.Ldate|log.Ltime)
	if *verboseFlag {
		verboseLogger = log.New(os.Stdout, "", log.Ldate|log.Ltime|log.Lshortfile)
	} else {
		verboseLogger = log.New(io.Discard, "", 0)
	}

	// Set config
	config.runOnce = *runOnceFlag
	listenAddr := flag.Arg(0)
	config.targetAddr = flag.Arg(1)

	// Validate arguments
	if listenAddr == "" || config.targetAddr == "" {
		logger.Fatal("Usage: websockify-go <listen_addr> <target_addr> [options]")
	}

	// Web server setup
	if *webDir != "" {
		config.webServer = true
		fileHandler = http.FileServer(http.Dir(*webDir))
	}

	// Log server settings
	sslLog := " - No SSL/TLS support (no cert file)"
	if *cert != "" && *key != "" {
		sslLog = " - SSL/TLS support"
	}
	logger.Printf("WebSocket server settings:\n"+
		" - Listen on %s\n"+
		sslLog+
		" - Proxying to %s\n", listenAddr, config.targetAddr)

	// Register handler
	http.HandleFunc("/", ws)

	// Start server
	if *cert != "" && *key != "" {
		logger.Printf("Starting secure WebSocket server (wss://) on %s", listenAddr)
		if err := http.ListenAndServeTLS(listenAddr, *cert, *key, nil); err != nil {
			logger.Fatal(err)
		}
	} else {
		logger.Printf("Starting WebSocket server (ws://) on %s", listenAddr)
		if err := http.ListenAndServe(listenAddr, nil); err != nil {
			logger.Fatal(err)
		}
	}
}
