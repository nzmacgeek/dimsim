package repo

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// RepoKey holds both the public and private halves of a repository signing key.
// The file is stored in <repo>/keys/root.key and must be kept confidential.
type RepoKey struct {
	KeyID   string `json:"keyid"`
	Public  string `json:"public"`
	Private string `json:"private"`
}

// GenerateKey creates a fresh ed25519 keypair and computes the TUF key ID.
func GenerateKey() (RepoKey, TUFKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return RepoKey{}, TUFKey{}, fmt.Errorf("generate ed25519 key: %w", err)
	}

	tufKey := TUFKey{
		KeyType: "ed25519",
		Scheme:  "ed25519",
		KeyVal:  TUFKeyVal{Public: hex.EncodeToString(pub)},
	}
	keyID, err := ComputeKeyID(tufKey)
	if err != nil {
		return RepoKey{}, TUFKey{}, err
	}

	rk := RepoKey{
		KeyID:   keyID,
		Public:  hex.EncodeToString(pub),
		Private: hex.EncodeToString(priv),
	}
	return rk, tufKey, nil
}

// ComputeKeyID returns the TUF key ID (SHA-256 of the canonical key JSON).
func ComputeKeyID(key TUFKey) (string, error) {
	// Canonical JSON: fields in alphabetical order — Go's json.Marshal sorts
	// struct fields by their json tags, which matches alphabetical order here.
	data, err := json.Marshal(key)
	if err != nil {
		return "", fmt.Errorf("marshal TUF key for key ID computation: %w", err)
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

// signPayload signs payload bytes with an ed25519 private key and returns a
// TUFSignature using the given keyID.
func signPayload(payload []byte, keyID string, privHex string) (TUFSignature, error) {
	privBytes, err := hex.DecodeString(privHex)
	if err != nil {
		return TUFSignature{}, fmt.Errorf("decode private key: %w", err)
	}
	sig := ed25519.Sign(ed25519.PrivateKey(privBytes), payload)
	return TUFSignature{KeyID: keyID, Sig: hex.EncodeToString(sig)}, nil
}

// marshalSigned encodes signed as compact JSON, signs it, and returns the full
// TUF envelope JSON bytes (pretty-printed for readability).
func marshalSigned(signed interface{}, rk RepoKey) ([]byte, error) {
	// Compact JSON of the signed payload — this is what gets signed.
	signedBytes, err := json.Marshal(signed)
	if err != nil {
		return nil, fmt.Errorf("marshal signed: %w", err)
	}

	sig, err := signPayload(signedBytes, rk.KeyID, rk.Private)
	if err != nil {
		return nil, err
	}

	envelope := TUFSigned{
		Signed:     json.RawMessage(signedBytes),
		Signatures: []TUFSignature{sig},
	}
	return json.MarshalIndent(envelope, "", "  ")
}

// BuildRoot creates a signed root.json covering all four roles with a single key.
func BuildRoot(tufKey TUFKey, keyID string, version int, expiry time.Time, rk RepoKey) ([]byte, error) {
	role := TUFRole{KeyIDs: []string{keyID}, Threshold: 1}
	signed := TUFRootSigned{
		Type:        "root",
		SpecVersion: "1.0",
		Version:     version,
		Expires:     expiry,
		Keys:        map[string]TUFKey{keyID: tufKey},
		Roles: map[string]TUFRole{
			"root":      role,
			"targets":   role,
			"snapshot":  role,
			"timestamp": role,
		},
	}
	return marshalSigned(signed, rk)
}

// BuildTargets creates a signed targets.json from the given targets map.
func BuildTargets(targets map[string]TUFTargetMeta, version int, expiry time.Time, rk RepoKey) ([]byte, error) {
	if targets == nil {
		targets = map[string]TUFTargetMeta{}
	}
	signed := TUFTargetsSigned{
		Type:        "targets",
		SpecVersion: "1.0",
		Version:     version,
		Expires:     expiry,
		Targets:     targets,
	}
	return marshalSigned(signed, rk)
}

// BuildSnapshot creates a signed snapshot.json referencing the given targets bytes.
func BuildSnapshot(targetsData []byte, targetsVersion int, version int, expiry time.Time, rk RepoKey) ([]byte, error) {
	h := sha256.Sum256(targetsData)
	signed := TUFSnapshotSigned{
		Type:        "snapshot",
		SpecVersion: "1.0",
		Version:     version,
		Expires:     expiry,
		Meta: map[string]TUFMetaRef{
			"targets.json": {
				Version: targetsVersion,
				Length:  len(targetsData),
				Hashes:  map[string]string{"sha256": hex.EncodeToString(h[:])},
			},
		},
	}
	return marshalSigned(signed, rk)
}

// BuildTimestamp creates a signed timestamp.json referencing the given snapshot bytes.
func BuildTimestamp(snapshotData []byte, snapshotVersion int, version int, expiry time.Time, rk RepoKey) ([]byte, error) {
	h := sha256.Sum256(snapshotData)
	signed := TUFTimestampSigned{
		Type:        "timestamp",
		SpecVersion: "1.0",
		Version:     version,
		Expires:     expiry,
		Meta: map[string]TUFMetaRef{
			"snapshot.json": {
				Version: snapshotVersion,
				Length:  len(snapshotData),
				Hashes:  map[string]string{"sha256": hex.EncodeToString(h[:])},
			},
		},
	}
	return marshalSigned(signed, rk)
}

// DpkTargetMeta builds TUFTargetMeta for a raw .dpk file and its parsed manifest.
func DpkTargetMeta(dpkData []byte, m *TUFTargetCustom) TUFTargetMeta {
	h := sha256.Sum256(dpkData)
	return TUFTargetMeta{
		Length: len(dpkData),
		Hashes: map[string]string{"sha256": hex.EncodeToString(h[:])},
		Custom: m,
	}
}
