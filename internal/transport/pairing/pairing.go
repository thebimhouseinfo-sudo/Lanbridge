package pairing

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/thebimhouseinfo-sudo/testbridge/internal/third_party/cpace"
)

const (
	CodeDuration   = 2 * time.Minute
	MaxFailedTries = 3
)

// Session manages the active 6-digit code on the Windows host
type HostPairingManager struct {
	mu           sync.Mutex
	activeCode   string
	expiresAt    time.Time
	failedTries  int
	hostCertFP   string
	clientCertFP string
	cpaceState   *cpace.State
	clientMsg    []byte
}

func NewHostPairingManager(hostCertFP string) *HostPairingManager {
	return &HostPairingManager{
		hostCertFP: hostCertFP,
	}
}

// GenerateNewCode generates a fresh 6-digit code valid for 2 minutes
func (m *HostPairingManager) GenerateNewCode() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", fmt.Errorf("failed to generate random pairing code: %w", err)
	}

	m.activeCode = fmt.Sprintf("%06d", n.Int64())
	m.expiresAt = time.Now().Add(CodeDuration)
	m.failedTries = 0
	m.clearExchange()

	return m.activeCode, nil
}

// StartResponder starts CPace as Responder on Host with ChannelID = host_fp || client_fp.
func (m *HostPairingManager) StartResponder(clientCertFP string, clientMsg []byte) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeCode == "" || !time.Now().Before(m.expiresAt) {
		m.activeCode = ""
		m.clearExchange()
		return nil, errors.New("pairing code has expired or was not generated")
	}
	if m.failedTries >= MaxFailedTries {
		return nil, errors.New("pairing code invalidated due to 3 failed attempts")
	}
	if m.cpaceState != nil {
		return nil, errors.New("a pairing exchange is already in progress")
	}
	if clientCertFP == "" || len(clientMsg) == 0 {
		return nil, errors.New("client certificate fingerprint and CPace message are required")
	}

	channelID := []byte(m.hostCertFP + clientCertFP)
	state, msg, err := cpace.Start(&cpace.Config{
		Role:      cpace.Responder,
		Password:  []byte(m.activeCode),
		ChannelID: channelID,
	})
	if err != nil {
		return nil, fmt.Errorf("cpace responder start failed: %w", err)
	}

	m.cpaceState = state
	m.clientMsg = append([]byte(nil), clientMsg...)
	m.clientCertFP = clientCertFP
	return msg, nil
}

// FinishResponder completes the PAKE and verifies the initiator's confirmation tag.
func (m *HostPairingManager) FinishResponder(clientTag []byte, peerCertFP string) (myTag []byte, clientCertFP string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cpaceState == nil {
		return nil, "", errors.New("responder not started")
	}
	if m.activeCode == "" || !time.Now().Before(m.expiresAt) {
		m.activeCode = ""
		m.clearExchange()
		return nil, "", errors.New("pairing code has expired")
	}
	if peerCertFP == "" || peerCertFP != m.clientCertFP {
		return nil, "", m.recordFailure(errors.New("pairing peer certificate changed during the exchange"))
	}

	_, err = m.cpaceState.Finish(m.clientMsg, nil)
	if err != nil {
		return nil, "", m.recordFailure(fmt.Errorf("cpace finish failed: %w", err))
	}

	if err := m.cpaceState.Verify(clientTag); err != nil {
		return nil, "", m.recordFailure(errors.New("invalid client confirmation tag (wrong code)"))
	}

	myTag = m.cpaceState.Tag()
	clientCertFP = m.clientCertFP
	m.activeCode = ""
	m.clearExchange()
	return myTag, clientCertFP, nil
}

func (m *HostPairingManager) recordFailure(err error) error {
	m.failedTries++
	m.clearExchange()
	if m.failedTries >= MaxFailedTries {
		m.activeCode = ""
	}
	return err
}

func (m *HostPairingManager) clearExchange() {
	m.cpaceState = nil
	m.clientCertFP = ""
	m.clientMsg = nil
}

func (m *HostPairingManager) HasActiveCode() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeCode != "" && !time.Now().Before(m.expiresAt) {
		m.activeCode = ""
		m.clearExchange()
	}
	return m.activeCode != ""
}

func (m *HostPairingManager) Invalidate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeCode = ""
	m.clearExchange()
}

func (m *HostPairingManager) ResetExchange() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearExchange()
}

// ClientPairingSession handles CPace Initiator on macOS client
type ClientPairingSession struct {
	code         string
	hostCertFP   string
	clientCertFP string
	cpaceState   *cpace.State
	finished     bool
}

func NewClientPairingSession(code, hostCertFP, clientCertFP string) *ClientPairingSession {
	return &ClientPairingSession{
		code:         code,
		hostCertFP:   hostCertFP,
		clientCertFP: clientCertFP,
	}
}

// StartInitiator starts CPace on Client with ChannelID = host_fp || client_fp
func (s *ClientPairingSession) StartInitiator() ([]byte, error) {
	if len(s.code) != 6 {
		return nil, errors.New("pairing code must contain six digits")
	}
	for _, digit := range s.code {
		if digit < '0' || digit > '9' {
			return nil, errors.New("pairing code must contain six digits")
		}
	}
	if s.hostCertFP == "" || s.clientCertFP == "" {
		return nil, errors.New("host and client certificate fingerprints are required")
	}
	if s.cpaceState != nil {
		return nil, errors.New("initiator has already started")
	}

	channelID := []byte(s.hostCertFP + s.clientCertFP)
	state, msg, err := cpace.Start(&cpace.Config{
		Role:      cpace.Initiator,
		Password:  []byte(s.code),
		ChannelID: channelID,
	})
	if err != nil {
		return nil, fmt.Errorf("cpace initiator start failed: %w", err)
	}

	s.cpaceState = state
	return msg, nil
}

// FinishInitiator finishes CPace and returns the initiator's confirmation tag.
func (s *ClientPairingSession) FinishInitiator(hostMsg []byte) (myTag []byte, err error) {
	if s.cpaceState == nil {
		return nil, errors.New("initiator not started")
	}
	if s.finished {
		return nil, errors.New("initiator has already finished")
	}

	_, err = s.cpaceState.Finish(hostMsg, nil)
	if err != nil {
		return nil, fmt.Errorf("client cpace finish failed: %w", err)
	}
	s.finished = true
	return s.cpaceState.Tag(), nil
}

func (s *ClientPairingSession) VerifyHostTag(hostTag []byte) error {
	if s.cpaceState == nil || !s.finished {
		return errors.New("initiator has not finished")
	}
	if err := s.cpaceState.Verify(hostTag); err != nil {
		return errors.New("invalid host confirmation tag (wrong code or untrusted host)")
	}
	return nil
}
