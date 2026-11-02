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

// Client implements the macOS Client TLS transport
type Client struct {
	targetAddr       string
	tlsConfig        *tls.Config
	stream           *protocol.TransportStream
	rawConn          net.Conn
	connMu           sync.Mutex
	pendingMu        sync.Mutex
	pendingRequests  map[string]chan *protocol.Message
	onDisconnect     func()
	onConnect        func()
	onNotification   func(msg *protocol.Message)
	closing          atomic.Bool
	stopChan         chan struct{}
	lastReceivedTime atomic.Int64
}

// NewClient initializes a macOS client instance
func NewClient(targetAddr string, tlsConfig *tls.Config) *Client {
	return &Client{
		targetAddr:      targetAddr,
		tlsConfig:       tlsConfig,
		pendingRequests: make(map[string]chan *protocol.Message),
		stopChan:        make(chan struct{}),
	}
}

// SetCallbacks registers lifecycle and notification handlers
func (c *Client) SetCallbacks(onConnect func(), onDisconnect func(), onNotification func(*protocol.Message)) {
	c.onConnect = onConnect
	c.onDisconnect = onDisconnect
	c.onNotification = onNotification
}

// Connect dials the host address with TLS
func (c *Client) Connect() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	conn, err := tls.Dial("tcp", c.targetAddr, c.tlsConfig)
	if err != nil {
		return fmt.Errorf("failed to dial TLS host %s: %w", c.targetAddr, err)
	}

	c.rawConn = conn
	c.stream = protocol.NewTransportStream(conn)
	c.lastReceivedTime.Store(time.Now().UnixNano())

	if c.onConnect != nil {
		c.onConnect()
	}

	go c.readLoop()
	go c.heartbeatLoop()

	return nil
}

// StartAutoReconnect runs a background loop to reconnect when disconnected
func (c *Client) StartAutoReconnect() {
	go func() {
		backoff := 3 * time.Second
		for {
			select {
			case <-c.stopChan:
				return
			default:
				if !c.IsConnected() && !c.closing.Load() {
					err := c.Connect()
					if err == nil {
						backoff = 3 * time.Second
					} else {
						time.Sleep(backoff)
						backoff *= 2
						if backoff > 30*time.Second {
							backoff = 30 * time.Second
						}
					}
				} else {
					time.Sleep(2 * time.Second)
				}
			}
		}
	}()
}

func (c *Client) readLoop() {
	defer func() {
		c.connMu.Lock()
		if c.rawConn != nil {
			c.rawConn.Close()
			c.rawConn = nil
			c.stream = nil
		}
		c.connMu.Unlock()

		if c.onDisconnect != nil && !c.closing.Load() {
			c.onDisconnect()
		}
	}()

	for {
		c.connMu.Lock()
		stream := c.stream
		c.connMu.Unlock()

		if stream == nil {
			return
		}

		msg, err := stream.ReadMessage()
		if err != nil {
			if errors.Is(err, io.EOF) || c.closing.Load() {
				return
			}
			return
		}

		c.lastReceivedTime.Store(time.Now().UnixNano())

		// Check if message is a response to a pending request
		c.pendingMu.Lock()
		respChan, exists := c.pendingRequests[msg.ID]
		if exists {
			delete(c.pendingRequests, msg.ID)
		}
		c.pendingMu.Unlock()

		if exists && respChan != nil {
			respChan <- msg
			continue
		}

		// Asynchronous notification from host
		if c.onNotification != nil {
			c.onNotification(msg)
		}
	}
}

func (c *Client) heartbeatLoop() {
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			if !c.IsConnected() {
				return
			}
			msg := &protocol.Message{
				ID:   fmt.Sprintf("hb-%d", time.Now().UnixNano()),
				Type: protocol.TypeHeartbeat,
			}
			_ = c.Send(msg)
		}
	}
}

// RoundTrip sends a request and waits for its matching response
func (c *Client) RoundTrip(msg *protocol.Message, timeout time.Duration) (*protocol.Message, error) {
	if !c.IsConnected() {
		return nil, errors.New("client not connected")
	}

	respChan := make(chan *protocol.Message, 1)

	c.pendingMu.Lock()
	c.pendingRequests[msg.ID] = respChan
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pendingRequests, msg.ID)
		c.pendingMu.Unlock()
	}()

	c.connMu.Lock()
	stream := c.stream
	c.connMu.Unlock()

	if stream == nil {
		return nil, errors.New("connection closed before write")
	}

	if err := stream.WriteMessage(msg); err != nil {
		return nil, fmt.Errorf("failed to write message: %w", err)
	}

	select {
	case resp := <-respChan:
		return resp, nil
	case <-time.After(timeout):
		return nil, errors.New("request timed out waiting for response")
	case <-c.stopChan:
		return nil, errors.New("client closed")
	}
}

// Send sends an asynchronous message
func (c *Client) Send(msg *protocol.Message) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.stream == nil {
		return errors.New("client not connected")
	}
	return c.stream.WriteMessage(msg)
}

// IsConnected returns whether the client has an active TLS connection
func (c *Client) IsConnected() bool {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	return c.rawConn != nil && c.stream != nil
}

// PeerFingerprint returns the remote certificate fingerprint from the live TLS session.
func (c *Client) PeerFingerprint() (string, error) {
	c.connMu.Lock()
	conn := c.rawConn
	c.connMu.Unlock()
	tlsConn, ok := conn.(*tls.Conn)
	if !ok || tlsConn == nil {
		return "", errors.New("client has no active TLS connection")
	}
	return ExtractPeerFingerprint(tlsConn.ConnectionState())
}

func (c *Client) SetTLSConfig(config *tls.Config) error {
	if config == nil {
		return errors.New("TLS config cannot be nil")
	}
	c.connMu.Lock()
	c.tlsConfig = config.Clone()
	c.connMu.Unlock()
	return nil
}

// Close gracefully closes the client connection sending a bye frame first
func (c *Client) Close() error {
	if !c.closing.CompareAndSwap(false, true) {
		return nil
	}
	close(c.stopChan)

	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.stream != nil {
		byeMsg := &protocol.Message{Type: protocol.TypeBye}
		_ = c.stream.WriteMessage(byeMsg)
		if c.rawConn != nil {
			c.rawConn.Close()
		}
		c.stream = nil
		c.rawConn = nil
	}

	return nil
}
