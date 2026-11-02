package pairing

import (
	"testing"
	"time"
)

const (
	testHostFP   = "1111222233334444555566667777888899990000aaaabbbbccccddddeeeeffff"
	testClientFP = "aaaabbbbccccddddeeeeffff1111222233334444555566667777888899990000"
)

func TestPairingSuccess(t *testing.T) {
	hostMgr := NewHostPairingManager(testHostFP)
	code, err := hostMgr.GenerateNewCode()
	if err != nil {
		t.Fatal(err)
	}

	clientSession := NewClientPairingSession(code, testHostFP, testClientFP)
	clientMsg, err := clientSession.StartInitiator()
	if err != nil {
		t.Fatalf("client start failed: %v", err)
	}
	hostMsg, err := hostMgr.StartResponder(testClientFP, clientMsg)
	if err != nil {
		t.Fatalf("host start failed: %v", err)
	}
	clientTag, err := clientSession.FinishInitiator(hostMsg)
	if err != nil {
		t.Fatalf("client finish failed: %v", err)
	}
	hostTag, pairedClientFP, err := hostMgr.FinishResponder(clientTag, testClientFP)
	if err != nil {
		t.Fatalf("host verification failed: %v", err)
	}
	if pairedClientFP != testClientFP {
		t.Fatalf("paired client fingerprint = %q, want %q", pairedClientFP, testClientFP)
	}
	if err := clientSession.VerifyHostTag(hostTag); err != nil {
		t.Fatalf("client verification failed: %v", err)
	}
	if hostMgr.HasActiveCode() {
		t.Fatal("successful pairing did not consume the code")
	}
}

func TestPairingWrongCodeCanRetry(t *testing.T) {
	hostMgr := NewHostPairingManager(testHostFP)
	code, err := hostMgr.GenerateNewCode()
	if err != nil {
		t.Fatal(err)
	}
	wrongCode := "000000"
	if code == wrongCode {
		wrongCode = "999999"
	}

	clientSession := NewClientPairingSession(wrongCode, testHostFP, testClientFP)
	clientMsg, err := clientSession.StartInitiator()
	if err != nil {
		t.Fatal(err)
	}
	hostMsg, err := hostMgr.StartResponder(testClientFP, clientMsg)
	if err != nil {
		t.Fatal(err)
	}
	clientTag, err := clientSession.FinishInitiator(hostMsg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := hostMgr.FinishResponder(clientTag, testClientFP); err == nil {
		t.Fatal("expected failure when client uses the wrong pairing code")
	}
	if !hostMgr.HasActiveCode() {
		t.Fatal("one failed attempt should leave the code active")
	}
}

func TestPairingRejectsPeerFingerprintChangeDuringExchange(t *testing.T) {
	hostMgr := NewHostPairingManager(testHostFP)
	code, err := hostMgr.GenerateNewCode()
	if err != nil {
		t.Fatal(err)
	}
	clientSession := NewClientPairingSession(code, testHostFP, testClientFP)
	clientMsg, err := clientSession.StartInitiator()
	if err != nil {
		t.Fatal(err)
	}
	hostMsg, err := hostMgr.StartResponder(testClientFP, clientMsg)
	if err != nil {
		t.Fatal(err)
	}
	clientTag, err := clientSession.FinishInitiator(hostMsg)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := hostMgr.FinishResponder(clientTag, "changed-peer-fingerprint"); err == nil {
		t.Fatal("responder accepted a peer fingerprint that changed during pairing")
	}
	if !hostMgr.HasActiveCode() {
		t.Fatal("peer fingerprint mismatch should count as a failed attempt without consuming the code")
	}
}

func TestPairingCodeExpiresAndLimitsFailedAttempts(t *testing.T) {
	t.Run("expired code", func(t *testing.T) {
		hostMgr := NewHostPairingManager(testHostFP)
		if _, err := hostMgr.GenerateNewCode(); err != nil {
			t.Fatal(err)
		}
		hostMgr.mu.Lock()
		hostMgr.expiresAt = time.Now().Add(-time.Second)
		hostMgr.mu.Unlock()
		if hostMgr.HasActiveCode() {
			t.Fatal("expired code is still active")
		}
		if _, err := hostMgr.StartResponder(testClientFP, []byte("client-message")); err == nil {
			t.Fatal("expired code started a responder")
		}
	})

	t.Run("three failed confirmations", func(t *testing.T) {
		hostMgr := NewHostPairingManager(testHostFP)
		code, err := hostMgr.GenerateNewCode()
		if err != nil {
			t.Fatal(err)
		}
		wrongCode := "000000"
		if code == wrongCode {
			wrongCode = "999999"
		}

		for attempt := 1; attempt <= MaxFailedTries; attempt++ {
			clientSession := NewClientPairingSession(wrongCode, testHostFP, testClientFP)
			clientMsg, err := clientSession.StartInitiator()
			if err != nil {
				t.Fatal(err)
			}
			hostMsg, err := hostMgr.StartResponder(testClientFP, clientMsg)
			if err != nil {
				t.Fatalf("attempt %d could not start: %v", attempt, err)
			}
			clientTag, err := clientSession.FinishInitiator(hostMsg)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := hostMgr.FinishResponder(clientTag, testClientFP); err == nil {
				t.Fatalf("attempt %d unexpectedly succeeded", attempt)
			}
			if got, want := hostMgr.HasActiveCode(), attempt < MaxFailedTries; got != want {
				t.Fatalf("after attempt %d, HasActiveCode() = %v, want %v", attempt, got, want)
			}
		}
	})
}

func TestPairingRequiresSixDigitCodeAndSequentialConfirmation(t *testing.T) {
	if _, err := NewClientPairingSession("12a456", testHostFP, testClientFP).StartInitiator(); err == nil {
		t.Fatal("invalid pairing code was accepted")
	}

	hostMgr := NewHostPairingManager(testHostFP)
	code, err := hostMgr.GenerateNewCode()
	if err != nil {
		t.Fatal(err)
	}
	clientSession := NewClientPairingSession(code, testHostFP, testClientFP)
	clientMsg, err := clientSession.StartInitiator()
	if err != nil {
		t.Fatal(err)
	}
	if err := clientSession.VerifyHostTag([]byte("unexpected")); err == nil {
		t.Fatal("client verified a host tag before finishing CPace")
	}
	hostMsg, err := hostMgr.StartResponder(testClientFP, clientMsg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clientSession.FinishInitiator(hostMsg); err != nil {
		t.Fatal(err)
	}
	if _, err := clientSession.FinishInitiator(hostMsg); err == nil {
		t.Fatal("client finished CPace more than once")
	}
}
