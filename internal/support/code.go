package support

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/url"
	"strings"
)

// codeAlphabet omits 0, 1, I, L, O and U, which are easily confused.
const codeAlphabet = "ABCDEFGHJKMNPQRSTVWXYZ23456789"

const (
	slotLen   = 4
	secretLen = 8
	// kdfIterations makes an offline guess of the secret part from an
	// observed proof expensive; the online path is limited separately by
	// the attempt counter and the owner's acceptance.
	kdfIterations = 120_000
)

// PairingCode is the one-time code shown to the computer owner. The first
// group only finds the offer (rendezvous); the remaining groups are a
// secret that never leaves either computer and is only proven.
type PairingCode struct {
	Slot   string
	Secret string
}

// NewPairingCode draws an unbiased code from crypto/rand.
func NewPairingCode() (PairingCode, error) {
	draw := func(n int) (string, error) {
		var b strings.Builder
		limit := big.NewInt(int64(len(codeAlphabet)))
		for range n {
			i, err := rand.Int(rand.Reader, limit)
			if err != nil {
				return "", err
			}
			b.WriteByte(codeAlphabet[i.Int64()])
		}
		return b.String(), nil
	}
	slot, err := draw(slotLen)
	if err != nil {
		return PairingCode{}, err
	}
	secret, err := draw(secretLen)
	if err != nil {
		return PairingCode{}, err
	}
	return PairingCode{Slot: slot, Secret: secret}, nil
}

// String formats the code in groups of four, for example "K7QM-3XRA-9PTD".
func (c PairingCode) String() string {
	return c.Slot + "-" + c.Secret[:4] + "-" + c.Secret[4:]
}

// ParsePairingCode accepts any spacing, dashes and case.
func ParsePairingCode(s string) (PairingCode, error) {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch {
		case r == '-' || r == ' ' || r == '\t':
		case strings.ContainsRune(codeAlphabet, r):
			b.WriteRune(r)
		default:
			return PairingCode{}, Errorf(CodePairingInvalid, "the code contains %q, which is not used in support codes", r)
		}
	}
	clean := b.String()
	if len(clean) != slotLen+secretLen {
		return PairingCode{}, Errorf(CodePairingInvalid, "a support code has %d characters", slotLen+secretLen)
	}
	return PairingCode{Slot: clean[:slotLen], Secret: clean[slotLen:]}, nil
}

// SlotID is what a relay or a discovery query sees. It identifies the
// offer, never the secret part of the code.
func (c PairingCode) SlotID() string {
	sum := sha256.Sum256([]byte("atlas-support-slot-v1|" + c.Slot))
	return hex.EncodeToString(sum[:16])
}

// key derives the proof key from the secret part, salted by the slot.
func (c PairingCode) key() []byte {
	k, err := pbkdf2.Key(sha256.New, c.Secret, []byte("atlas-support-v1|"+c.Slot), kdfIterations, 32)
	if err != nil {
		panic(err) // Only on invalid parameters, which are constant here.
	}
	return k
}

// proof binds knowledge of the code to one TLS session: the exporter is
// unique to the connection and both certificates, so a relay that runs two
// TLS sessions cannot forward a proof from one to the other.
func proof(key []byte, role string, exporter []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("atlas-support-proof-v1|" + role + "|"))
	m.Write(exporter)
	return m.Sum(nil)
}

// VerificationCode is a six-digit value both owners can compare.
func VerificationCode(exporter []byte) string {
	sum := sha256.Sum256(append([]byte("atlas-support-sas-v1|"), exporter...))
	n := binary.BigEndian.Uint32(sum[:4]) % 1_000_000
	return fmt.Sprintf("%03d %03d", n/1000, n%1000)
}

// JoinLink is an atlas-support:// link carrying the code and optionally a
// relay URL or a local address. It contains the secret code: share it only
// with the person who should support this computer.
func JoinLink(code PairingCode, relayURL, address string) string {
	q := url.Values{"code": {code.String()}}
	if relayURL != "" {
		q.Set("relay", relayURL)
	}
	if address != "" {
		q.Set("addr", address)
	}
	return "atlas-support://join?" + q.Encode()
}

// Target is a parsed connection request from user input.
type Target struct {
	Code    PairingCode
	Relay   string
	Address string
	Funnel  string
}

// ParseTarget accepts a bare code, "CODE@host:port" or a join link.
func ParseTarget(input string) (Target, error) {
	input = strings.TrimSpace(input)
	if rest, ok := strings.CutPrefix(input, "atlas-support://join?"); ok {
		q, err := url.ParseQuery(rest)
		if err != nil {
			return Target{}, Errorf(CodePairingInvalid, "invalid support link")
		}
		code, err := ParsePairingCode(q.Get("code"))
		if err != nil {
			return Target{}, err
		}
		if funnel := q.Get("funnel"); funnel != "" {
			if _, err := funnelEndpoint(funnel); err != nil || q.Get("relay") != "" || q.Get("addr") != "" {
				return Target{}, Errorf(CodePairingInvalid, "invalid or conflicting Funnel destination")
			}
		}
		return Target{Code: code, Relay: q.Get("relay"), Address: q.Get("addr"), Funnel: q.Get("funnel")}, nil
	}
	codePart, addr, _ := strings.Cut(input, "@")
	code, err := ParsePairingCode(codePart)
	if err != nil {
		return Target{}, err
	}
	return Target{Code: code, Address: strings.TrimSpace(addr)}, nil
}
