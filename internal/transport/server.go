package transport

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/protocol"
)

const (
	HeartbeatInterval = 10 * time.Second
	HeartbeatTimeout  = 30 * time.Second
)

// HandlerFunc handles an incoming request and optionally returns a response
type HandlerFunc func(msg *protocol.Message) (*protocol.Message, error)

// Server implements the Windows Host TLS listener
type Server struct {
	port             int
	tlsConfig        *tls.Config
	listener         net.Listener
	handlers         map[string]HandlerFunc
	handlersMu       sync.RWMutex
	requestGuard     func(*protocol.Message) *protocol.Message
	requestGuardMu   sync.RWMutex
	activeConn       *protocol.TransportStream
	activeConnMu     sync.Mutex
	lastReceivedTime atomic.Int64
	onClientConnect  func(peerFingerprint string)
	onClientBye      func()
	onClientDrop     func()
	closing          atomic.Bool
	stopChan         chan struct{}
}

// NewServer initializes a new host TLS server
func NewServer(port int, tlsConfig *tls.Config) *Server {
	return &Server{
		port:      port,
		tlsConfig: tlsConfig,
		handlers:  make(map[string]HandlerFunc),
		stopChan:  make(chan struct{}),
	}
}

// RegisterHandler registers a request handler for a message type
func (s *Server) RegisterHandler(msgType string, h HandlerFunc) {
	s.handlersMu.Lock()
	defer s.handlersMu.Unlock()
	s.handlers[msgType] = h
}

// SetRequestGuard installs a guard that can reject requests before handler dispatch.
func (s *Server) SetRequestGuard(guard func(*protocol.Message) *protocol.Message) {
	s.requestGuardMu.Lock()
	defer s.requestGuardMu.Unlock()
	s.requestGuard = guard
}

// SetCallbacks sets connection lifecycle hooks
func (s *Server) SetCallbacks(onConnect func(string), onBye func(), onDrop func()) {
	s.onClientConnect = onConnect
	s.onClientBye = onBye
	s.onClientDrop = onDrop
}

// Start begins listening and serving connections
func (s *Server) Start() error {
	addr := fmt.Sprintf("0.0.0.0:%d", s.port)
	ln, err := tls.Listen("tcp", addr, s.tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = ln

	go s.acceptLoop()
	return nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.closing.Load() {
				return
			}
			continue
		}

		tlsConn, ok := conn.(*tls.Conn)
		if !ok {
			conn.Close()
			continue
		}

		// Perform handshake to extract peer identity
		if err := tlsConn.Handshake(); err != nil {
			conn.Close()
			continue
		}

		s.handleNewConnection(tlsConn)
	}
}

func (s *Server) handleNewConnection(tlsConn *tls.Conn) {
	s.activeConnMu.Lock()
	if s.activeConn != nil {
		// Only one active client allowed at a time (CRITERIA §11.3)
		s.activeConnMu.Unlock()
		tlsConn.Close()
		return
	}

	stream := protocol.NewTransportStream(tlsConn)
	s.activeConn = stream
	s.activeConnMu.Unlock()

	s.lastReceivedTime.Store(time.Now().UnixNano())

	peerFP, _ := ExtractPeerFingerprint(tlsConn.ConnectionState())
	if s.onClientConnect != nil {
		s.onClientConnect(peerFP)
	}

	go s.connectionLoop(stream, tlsConn, peerFP)
	go s.heartbeatMonitor(tlsConn)
}

func (s *Server) connectionLoop(stream *protocol.TransportStream, rawConn net.Conn, peerFingerprint string) {
	gracefulBye := false
	defer func() {
		rawConn.Close()
		s.activeConnMu.Lock()
		if s.activeConn == stream {
			s.activeConn = nil
		}
		s.activeConnMu.Unlock()
		if !gracefulBye && !s.closing.Load() && s.onClientDrop != nil {
			s.onClientDrop()
		}
	}()

	for {
		msg, err := stream.ReadMessage()
		if err != nil {
			if errors.Is(err, io.EOF) || s.closing.Load() {
				break
			}
			break
		}
		msg.PeerFingerprint = peerFingerprint

		s.lastReceivedTime.Store(time.Now().UnixNano())

		if msg.Type == protocol.TypeBye {
			// Graceful exit from Mac client: cancel active tasks
			gracefulBye = true
			if s.onClientBye != nil {
				s.onClientBye()
			}
			break
		}

		if msg.Type == protocol.TypeHeartbeat {
			// Echo heartbeat back
			resp, _ := protocol.NewResponse(msg.ID, protocol.TypeHeartbeat, nil)
			_ = stream.WriteMessage(resp)
			continue
		}

		s.requestGuardMu.RLock()
		guard := s.requestGuard
		s.requestGuardMu.RUnlock()
		if guard != nil {
			if response := guard(msg); response != nil {
				if response.ID == "" {
					response.ID = msg.ID
				}
				_ = stream.WriteMessage(response)
				continue
			}
		}

		// Route message to handler
		s.handlersMu.RLock()
		handler, exists := s.handlers[msg.Type]
		s.handlersMu.RUnlock()

		if !exists {
			errResp := protocol.NewErrorResponse(msg.ID, "ERR_UNKNOWN_TYPE", fmt.Sprintf("no handler for message type: %s", msg.Type))
			_ = stream.WriteMessage(errResp)
			continue
		}

		go func(msg *protocol.Message, handler HandlerFunc) {
			resp, err := handler(msg)
			if err != nil {
				resp = protocol.NewErrorResponse(msg.ID, "ERR_HANDLER", err.Error())
			}
			if resp != nil {
				_ = stream.WriteMessage(resp)
			}
		}(msg, handler)
	}
}

func (s *Server) heartbeatMonitor(conn net.Conn) {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.activeConnMu.Lock()
			active := s.activeConn != nil
			s.activeConnMu.Unlock()

			if !active {
				return
			}

			lastTime := time.Unix(0, s.lastReceivedTime.Load())
			if time.Since(lastTime) > HeartbeatTimeout {
				// The connection loop handles lifecycle notification after this close.
				conn.Close()
				return
			}
		}
	}
}

// SendMessage sends a message to the currently connected client
func (s *Server) SendMessage(msg *protocol.Message) error {
	s.activeConnMu.Lock()
	defer s.activeConnMu.Unlock()
	if s.activeConn == nil {
		return errors.New("no active client connection")
	}
	return s.activeConn.WriteMessage(msg)
}

// Close gracefully stops the server listener
func (s *Server) Close() error {
	s.closing.Store(true)
	close(s.stopChan)
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}
