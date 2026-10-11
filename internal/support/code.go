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

