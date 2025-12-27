package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ErrUnsupportedPlatform is returned when browser opening is not supported.
var ErrUnsupportedPlatform = errors.New("unsupported platform")

//go:embed static/*
var staticFiles embed.FS

// ReverbController defines the interface for controlling the reverb.
type ReverbController interface {
	GetWetLevel() float64
	GetDryLevel() float64
	SetWetLevel(level float64)
	SetDryLevel(level float64)
	SwitchIR(data []byte, irIndex int) (string, error)
	GetMetrics(channel int) (inputLevel, outputLevel, reverbLevel float32)
}

// IREntry represents an impulse response entry for JSON serialization.
type IREntry struct {
	Index      int     `json:"index"`
	Name       string  `json:"name"`
	Category   string  `json:"category"`
	SampleRate float64 `json:"sampleRate"`
	Channels   int     `json:"channels"`
	Samples    int     `json:"samples"`
	Duration   float64 `json:"duration"`
}

// Message represents a WebSocket message.
type Message struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

// StatePayload represents the current state.
type StatePayload struct {
	Wet     float64 `json:"wet"`
	Dry     float64 `json:"dry"`
	IRIndex int     `json:"irIndex"`
	IRName  string  `json:"irName"`
}

// MetersPayload represents meter values in dB.
type MetersPayload struct {
	InL  float64 `json:"inL"`
	InR  float64 `json:"inR"`
	RevL float64 `json:"revL"`
	RevR float64 `json:"revR"`
	OutL float64 `json:"outL"`
	OutR float64 `json:"outR"`
}

// Server is the web server for the convolution reverb UI.
type Server struct {
	reverb        ReverbController
	irLibraryData []byte
	irList        []IREntry
	port          int
	hub           *Hub
	httpServer    *http.Server

	mu            sync.RWMutex
	currentIRIdx  int
	currentIRName string
}

// IRIndexEntryAdapter is used to convert from dsp.IRIndexEntry.
type IRIndexEntryAdapter interface {
	GetName() string
	GetCategory() string
	GetSampleRate() float64
	GetChannels() int
	GetSamples() int
	Duration() float64
}

// NewServer creates a new web server.
func NewServer(
	reverb ReverbController, irLibraryData []byte, irEntries interface{},
	port int, initialIRIdx int, initialIRName string,
) *Server {
	// Convert IR entries to our format
	var irList []IREntry

	// Use reflection-free approach with type switch
	switch entries := irEntries.(type) {
	case []IREntry:
		irList = entries
	default:
		// Handle slice of structs with required fields
		// This will be populated by the caller converting their type
	}

	return &Server{
		reverb:        reverb,
		irLibraryData: irLibraryData,
		irList:        irList,
		port:          port,
		hub:           NewHub(),
		currentIRIdx:  initialIRIdx,
		currentIRName: initialIRName,
	}
}

// SetIRList sets the IR list (used when the caller needs to convert types).
func (s *Server) SetIRList(entries []IREntry) {
	s.irList = entries
}

// Start starts the web server.
func (s *Server) Start() error {
	go s.hub.Run()
	go s.meterBroadcastLoop()

	// Create file system for static files
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("failed to create static file system: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/api/state", s.handleAPIState)
	mux.HandleFunc("/api/ir-list", s.handleAPIIRList)

	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", s.port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	slog.Info("Web server starting", "port", s.port, "url", fmt.Sprintf("http://localhost:%d", s.port))
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// handleIndex serves the main HTML page.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

//nolint:gochecknoglobals // WebSocket upgrader configuration
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(_ *http.Request) bool {
		return true // Allow all origins for local development
	},
}

// handleWebSocket handles WebSocket connections.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("WebSocket upgrade failed", "error", err)
		return
	}

	client := &Client{
		hub:  s.hub,
		conn: conn,
		send: make(chan []byte, 256),
	}

	s.hub.register <- client

	// Send initial state
	s.sendState(client)
	s.sendIRList(client)

	// Start client pumps
	go client.writePump()
	client.readPump(func(msg []byte) {
		s.handleClientMessage(msg)
	})
}

// sendState sends the current state to a client.
func (s *Server) sendState(client *Client) {
	s.mu.RLock()
	state := StatePayload{
		Wet:     s.reverb.GetWetLevel(),
		Dry:     s.reverb.GetDryLevel(),
		IRIndex: s.currentIRIdx,
		IRName:  s.currentIRName,
	}
	s.mu.RUnlock()

	msg := Message{Type: "state", Payload: state}
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("Failed to marshal state", "error", err)
		return
	}
	client.send <- data
}

// sendIRList sends the IR list to a client.
func (s *Server) sendIRList(client *Client) {
	msg := Message{Type: "ir_list", Payload: s.irList}
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("Failed to marshal IR list", "error", err)
		return
	}
	client.send <- data
}

// handleClientMessage handles incoming WebSocket messages.
func (s *Server) handleClientMessage(data []byte) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		slog.Error("Failed to parse WebSocket message", "error", err)
		return
	}

	switch msg.Type {
	case "set_wet":
		if payload, ok := msg.Payload.(map[string]interface{}); ok {
			if value, ok := payload["value"].(float64); ok {
				s.reverb.SetWetLevel(value)
				s.broadcastParamChange("wet", value)
			}
		}

	case "set_dry":
		if payload, ok := msg.Payload.(map[string]interface{}); ok {
			if value, ok := payload["value"].(float64); ok {
				s.reverb.SetDryLevel(value)
				s.broadcastParamChange("dry", value)
			}
		}

	case "set_ir":
		if payload, ok := msg.Payload.(map[string]interface{}); ok {
			if index, ok := payload["index"].(float64); ok {
				idx := int(index)
				if len(s.irLibraryData) > 0 {
					name, err := s.reverb.SwitchIR(s.irLibraryData, idx)
					if err == nil {
						s.mu.Lock()
						s.currentIRIdx = idx
						s.currentIRName = name
						s.mu.Unlock()
						s.broadcastIRChange(idx, name)
					} else {
						slog.Error("Failed to switch IR", "index", idx, "error", err)
					}
				}
			}
		}
	}
}

// broadcastParamChange broadcasts a parameter change to all clients.
func (s *Server) broadcastParamChange(param string, value float64) {
	msg := Message{
		Type: "param_changed",
		Payload: map[string]interface{}{
			"param": param,
			"value": value,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("Failed to marshal param change", "error", err)
		return
	}
	s.hub.Broadcast(data)
}

// broadcastIRChange broadcasts an IR change to all clients.
func (s *Server) broadcastIRChange(index int, name string) {
	msg := Message{
		Type: "ir_changed",
		Payload: map[string]interface{}{
			"index": index,
			"name":  name,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("Failed to marshal IR change", "error", err)
		return
	}
	s.hub.Broadcast(data)
}

// meterBroadcastLoop broadcasts meter values at 50ms intervals.
func (s *Server) meterBroadcastLoop() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		if s.hub.ClientCount() == 0 {
			continue // No clients, skip
		}

		inL, outL, revL := s.reverb.GetMetrics(0)
		inR, outR, revR := s.reverb.GetMetrics(1)

		meters := MetersPayload{
			InL:  linToDB(inL),
			InR:  linToDB(inR),
			RevL: linToDB(revL),
			RevR: linToDB(revR),
			OutL: linToDB(outL),
			OutR: linToDB(outR),
		}

		msg := Message{Type: "meters", Payload: meters}
		data, err := json.Marshal(msg)
		if err != nil {
			continue // Skip this tick on marshal error
		}
		s.hub.Broadcast(data)
	}
}

// linToDB converts linear amplitude to dB.
func linToDB(l float32) float64 {
	if l <= 1e-9 {
		return -96.0
	}
	db := 20 * math.Log10(float64(l))
	if db < -96.0 {
		return -96.0
	}
	if db > 6.0 {
		return 6.0
	}
	return db
}

// handleAPIState handles the REST API state endpoint.
func (s *Server) handleAPIState(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	state := StatePayload{
		Wet:     s.reverb.GetWetLevel(),
		Dry:     s.reverb.GetDryLevel(),
		IRIndex: s.currentIRIdx,
		IRName:  s.currentIRName,
	}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	//nolint:errchkjson // StatePayload is a well-defined struct
	_ = json.NewEncoder(w).Encode(state)
}

// handleAPIIRList handles the REST API IR list endpoint.
func (s *Server) handleAPIIRList(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	//nolint:errchkjson // IREntry slice is well-defined
	_ = json.NewEncoder(w).Encode(s.irList)
}

// OnWetLevelChange is called when the wet level changes (StateListener).
func (s *Server) OnWetLevelChange(level float64) {
	s.broadcastParamChange("wet", level)
}

// OnDryLevelChange is called when the dry level changes (StateListener).
func (s *Server) OnDryLevelChange(level float64) {
	s.broadcastParamChange("dry", level)
}

// OnIRChange is called when the IR changes (StateListener).
func (s *Server) OnIRChange(index int, name string) {
	s.mu.Lock()
	s.currentIRIdx = index
	s.currentIRName = name
	s.mu.Unlock()
	s.broadcastIRChange(index, name)
}

// OpenBrowser opens the default browser to the specified URL.
func OpenBrowser(url string) error {
	ctx := context.Background()
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "linux":
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	case "windows":
		cmd = exec.CommandContext(ctx, "cmd", "/c", "start", url)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedPlatform, runtime.GOOS)
	}

	return cmd.Start()
}
