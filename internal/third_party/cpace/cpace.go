// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cpace implements the CPace balanced password-authenticated key
// exchange, which lets two parties that share a low-entropy secret (a
// password) derive a strong shared key without exposing the password to
// offline dictionary attacks.
//
// This package implements the cipher suite CPACE-X25519-SHA512 of
// “CPace, a balanced composable PAKE”, draft-irtf-cfrg-cpace-21,
// https://datatracker.ietf.org/doc/draft-irtf-cfrg-cpace/21/.
package cpace

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"errors"
	"io"
)

// Suite constants for CPACE-X25519-SHA512.
const (
	dsi         = "CPace255"       // domain separation identifier
	dsiISK      = dsi + "_ISK"     // domain separation identifier for the session key
	dsiSID      = "CPaceSidOutput" // domain separation identifier for the session identifier
	dsiMac      = "CPaceMac"       // domain separation identifier for the confirmation key
	blockSize   = sha512.BlockSize // input block size of H
	elementSize = 32               // size of a group element and of a scalar
)

// A Role identifies the part a party plays in a CPace exchange.
type Role int

const (
	Initiator Role = 1 + iota
	Responder
	Symmetric
)

// A Config holds the inputs to a CPace exchange.
type Config struct {
	Role      Role
	Password  []byte
	ChannelID []byte
	SessionID []byte
	AD        []byte
	Rand      io.Reader
}

// A State holds one party's state during a CPace exchange.
type State struct {
	role Role
	sid  []byte
	ad   []byte
	msg  []byte
	priv *ecdh.PrivateKey

	sidOutput []byte
	tag       []byte // this party's key confirmation tag
	peerTag   []byte // the other party's expected key confirmation tag
}

// Start starts a CPace exchange, returning the state needed to finish it
// and the message to send to the other party.
func Start(cfg *Config) (*State, []byte, error) {
	switch cfg.Role {
	case Initiator, Responder, Symmetric:
	default:
		return nil, nil, errors.New("cpace: invalid role")
	}

	scalar := make([]byte, elementSize)
	r := cfg.Rand
	if r == nil {
		r = rand.Reader
	}
	if _, err := io.ReadFull(r, scalar); err != nil {
		return nil, nil, err
	}
	return start(cfg, scalar)
}

func start(cfg *Config, scalar []byte) (*State, []byte, error) {
	priv, err := ecdh.X25519().NewPrivateKey(scalar)
	if err != nil {
		return nil, nil, err
	}
	g, err := ecdh.X25519().NewPublicKey(generator(cfg.Password, cfg.ChannelID, cfg.SessionID))
	if err != nil {
		return nil, nil, err
	}
	msg, err := priv.ECDH(g)
	if err != nil {
		return nil, nil, errors.New("cpace: invalid generator: " + err.Error())
	}
	s := &State{
		role: cfg.Role,
		sid:  bytes.Clone(cfg.SessionID),
		ad:   bytes.Clone(cfg.AD),
		msg:  msg,
		priv: priv,
	}
	return s, bytes.Clone(msg), nil
}

// Finish completes a CPace exchange using the message and associated data
// received from the other party, returning the intermediate session key.
func (s *State) Finish(msg, ad []byte) ([]byte, error) {
	if s.priv == nil {
		return nil, errors.New("cpace: exchange already finished")
	}
	priv := s.priv
	s.priv = nil

	if bytes.Equal(msg, s.msg) {
		return nil, errors.New("cpace: invalid peer message: our own message")
	}

	peer, err := ecdh.X25519().NewPublicKey(msg)
	if err != nil {
		return nil, errors.New("cpace: invalid peer message")
	}
	k, err := priv.ECDH(peer)
	if err != nil {
		return nil, errors.New("cpace: invalid peer message: low order point")
	}
	defer clear(k)

	ya, ada, yb, adb := s.msg, s.ad, msg, ad
	if s.role == Responder {
		ya, ada, yb, adb = yb, adb, ya, ada
	}

	n := 7*maxLenPrefix + 2 + len(dsiISK) + len(s.sid) + len(k) +
		len(ya) + len(ada) + len(yb) + len(adb)
	b := lvCat(make([]byte, 0, n), []byte(dsiISK), s.sid, k)
	b = s.transcript(b, ya, ada, yb, adb)
	isk := sha512.Sum512(b)
	clear(b)

	b = s.transcript([]byte(dsiSID), ya, ada, yb, adb)
	sidOutput := sha512.Sum512(b)
	s.sidOutput = sidOutput[:]

	b = make([]byte, 0, len(dsiMac)+len(s.sid)+len(isk))
	b = append(b, dsiMac...)
	b = append(b, s.sid...)
	b = append(b, isk[:]...)
	macKey := sha512.Sum512(b)
	clear(b)
	s.tag = mac(macKey[:], s.msg, s.ad)
	s.peerTag = mac(macKey[:], msg, ad)
	clear(macKey[:])

	return isk[:], nil
}

func mac(key, msg, ad []byte) []byte {
	h := hmac.New(sha512.New, key)
	h.Write(lvCat(nil, msg, ad))
	return h.Sum(nil)
}

// Tag returns this party's key confirmation tag
func (s *State) Tag() []byte {
	return bytes.Clone(s.tag)
}

// Verify checks the other party's key confirmation tag
func (s *State) Verify(tag []byte) error {
	if s.peerTag == nil {
		return errors.New("cpace: exchange not finished")
	}
	if !hmac.Equal(tag, s.peerTag) {
		return errors.New("cpace: key confirmation failed")
	}
	return nil
}

// SessionID returns a public identifier for the exchange
func (s *State) SessionID() []byte {
	return bytes.Clone(s.sidOutput)
}

func generatorString(prs, ci, sid []byte) []byte {
	n := 5*maxLenPrefix + len(dsi) + len(prs) + blockSize + len(ci) + len(sid)
	b := lvCat(make([]byte, 0, n), []byte(dsi), prs)
	zpad := max(0, blockSize-1-len(b))
	return lvCat(b, make([]byte, zpad), ci, sid)
}

func generator(prs, ci, sid []byte) []byte {
	b := generatorString(prs, ci, sid)
	h := sha512.Sum512(b)
	clear(b)

	var u [32]byte
	copy(u[:], h[:elementSize])
	clear(h[:])
	var r fe
	feSetBytes(&r, &u)
	clear(u[:])
	g := elligator2(&r)
	return g[:]
}

func (s *State) transcript(b, ya, ada, yb, adb []byte) []byte {
	if s.role == Symmetric {
		return oCat(b, lvCat(nil, ya, ada), lvCat(nil, yb, adb))
	}
	return lvCat(lvCat(b, ya, ada), yb, adb)
}

const maxLenPrefix = 10

func prependLen(b, v []byte) []byte {
	n := len(v)
	for {
		if n < 128 {
			b = append(b, byte(n))
		} else {
			b = append(b, byte(n)&0x7f|0x80)
		}
		n >>= 7
		if n == 0 {
			break
		}
	}
	return append(b, v...)
}

func lvCat(b []byte, vs ...[]byte) []byte {
	for _, v := range vs {
		b = prependLen(b, v)
	}
	return b
}

func oCat(b, v1, v2 []byte) []byte {
	b = append(b, "oc"...)
	if bytes.Compare(v1, v2) < 0 {
		v1, v2 = v2, v1
	}
	b = append(b, v1...)
	return append(b, v2...)
}
