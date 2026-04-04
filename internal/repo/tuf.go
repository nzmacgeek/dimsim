package repo

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// --- TUF data types ---

// TUFSigned is a generic TUF envelope.
type TUFSigned struct {
	Signed     json.RawMessage  `json:"signed"`
	Signatures []TUFSignature   `json:"signatures"`
}

// TUFSignature represents a single TUF signature.
type TUFSignature struct {
	KeyID string `json:"keyid"`
	Sig   string `json:"sig"`
}

// TUFKey represents a TUF public key.
type TUFKey struct {
	KeyType string       `json:"keytype"`
	Scheme  string       `json:"scheme"`
	KeyVal  TUFKeyVal    `json:"keyval"`
}

// TUFKeyVal holds the public key material.
type TUFKeyVal struct {
	Public string `json:"public"`
}

// TUFRole defines the key IDs and threshold for a role.
type TUFRole struct {
	KeyIDs    []string `json:"keyids"`
	Threshold int      `json:"threshold"`
}

// TUFRootSigned is the signed portion of root.json.
type TUFRootSigned struct {
	Type        string             `json:"_type"`
	SpecVersion string             `json:"spec_version"`
	Version     int                `json:"version"`
	Expires     time.Time          `json:"expires"`
	Keys        map[string]TUFKey  `json:"keys"`
	Roles       map[string]TUFRole `json:"roles"`
}

// TUFMetaRef holds hash+size+version information for a metadata file.
type TUFMetaRef struct {
	Version int               `json:"version"`
	Length  int               `json:"length"`
	Hashes  map[string]string `json:"hashes"`
}

// TUFTimestampSigned is the signed portion of timestamp.json.
type TUFTimestampSigned struct {
	Type        string                `json:"_type"`
	SpecVersion string                `json:"spec_version"`
	Version     int                   `json:"version"`
	Expires     time.Time             `json:"expires"`
	Meta        map[string]TUFMetaRef `json:"meta"`
}

// TUFSnapshotSigned is the signed portion of snapshot.json.
type TUFSnapshotSigned struct {
	Type        string                `json:"_type"`
	SpecVersion string                `json:"spec_version"`
	Version     int                   `json:"version"`
	Expires     time.Time             `json:"expires"`
	Meta        map[string]TUFMetaRef `json:"meta"`
}

// TUFTargetMeta is per-target metadata in targets.json.
type TUFTargetMeta struct {
	Length int               `json:"length"`
	Hashes map[string]string `json:"hashes"`
	Custom *TUFTargetCustom  `json:"custom,omitempty"`
}

// TUFTargetCustom is the custom metadata for a package target.
type TUFTargetCustom struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Arch        string   `json:"arch"`
	Description string   `json:"description"`
	Depends     []string `json:"depends,omitempty"`
	Provides    []string `json:"provides,omitempty"`
}

// TUFTargetsSigned is the signed portion of targets.json.
type TUFTargetsSigned struct {
	Type        string                   `json:"_type"`
	SpecVersion string                   `json:"spec_version"`
	Version     int                      `json:"version"`
	Expires     time.Time                `json:"expires"`
	Targets     map[string]TUFTargetMeta `json:"targets"`
}

// --- Verification helpers ---

// VerifyTUFSignatures checks that envelope has enough valid signatures for the given role.
// keys is the full key map from root; role provides key IDs and threshold.
func VerifyTUFSignatures(envelope *TUFSigned, keys map[string]TUFKey, role TUFRole) error {
	// The signed payload that is actually signed over
	payload := envelope.Signed

	valid := 0
	for _, sig := range envelope.Signatures {
		key, ok := keys[sig.KeyID]
		if !ok {
			continue
		}
		// Check if this key is authorised for the role
		authorised := false
		for _, kid := range role.KeyIDs {
			if kid == sig.KeyID {
				authorised = true
				break
			}
		}
		if !authorised {
			continue
		}

		if key.KeyType != "ed25519" || key.Scheme != "ed25519" {
			continue
		}

		pubBytes, err := hex.DecodeString(key.KeyVal.Public)
		if err != nil {
			continue
		}
		sigBytes, err := hex.DecodeString(sig.Sig)
		if err != nil {
			continue
		}

		if ed25519.Verify(ed25519.PublicKey(pubBytes), payload, sigBytes) {
			valid++
		}
	}

	if valid < role.Threshold {
		return fmt.Errorf("insufficient valid signatures: got %d, need %d", valid, role.Threshold)
	}
	return nil
}

// SHA256Hex returns the sha256 hex digest of data.
func SHA256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// VerifyHash checks that data matches the expected hash in the hashes map.
func VerifyHash(data []byte, hashes map[string]string) error {
	expected, ok := hashes["sha256"]
	if !ok {
		return fmt.Errorf("no sha256 hash in metadata")
	}
	got := SHA256Hex(data)
	if got != expected {
		return fmt.Errorf("hash mismatch: got %s, want %s", got, expected)
	}
	return nil
}

// ParseRoot parses a raw root.json envelope, verifies self-signatures, and returns
// the signed root structure plus the raw bytes (for caching).
func ParseRoot(data []byte) (*TUFRootSigned, *TUFSigned, error) {
	var envelope TUFSigned
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, nil, fmt.Errorf("parse root envelope: %w", err)
	}

	var signed TUFRootSigned
	if err := json.Unmarshal(envelope.Signed, &signed); err != nil {
		return nil, nil, fmt.Errorf("parse root signed: %w", err)
	}

	if signed.Type != "root" {
		return nil, nil, fmt.Errorf("expected root metadata, got %q", signed.Type)
	}

	rootRole, ok := signed.Roles["root"]
	if !ok {
		return nil, nil, fmt.Errorf("root.json missing root role")
	}

	if err := VerifyTUFSignatures(&envelope, signed.Keys, rootRole); err != nil {
		return nil, nil, fmt.Errorf("root signature verification: %w", err)
	}

	return &signed, &envelope, nil
}

// ParseTimestamp parses and verifies a timestamp.json against the root.
func ParseTimestamp(data []byte, root *TUFRootSigned) (*TUFTimestampSigned, error) {
	var envelope TUFSigned
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("parse timestamp envelope: %w", err)
	}

	role, ok := root.Roles["timestamp"]
	if !ok {
		return nil, fmt.Errorf("root missing timestamp role")
	}

	if err := VerifyTUFSignatures(&envelope, root.Keys, role); err != nil {
		return nil, fmt.Errorf("timestamp signature: %w", err)
	}

	var signed TUFTimestampSigned
	if err := json.Unmarshal(envelope.Signed, &signed); err != nil {
		return nil, fmt.Errorf("parse timestamp signed: %w", err)
	}

	if time.Now().After(signed.Expires) {
		return nil, fmt.Errorf("timestamp metadata expired at %s", signed.Expires.Format(time.RFC3339))
	}

	return &signed, nil
}

// ParseSnapshot parses and verifies a snapshot.json against the root.
func ParseSnapshot(data []byte, root *TUFRootSigned) (*TUFSnapshotSigned, error) {
	var envelope TUFSigned
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("parse snapshot envelope: %w", err)
	}

	role, ok := root.Roles["snapshot"]
	if !ok {
		return nil, fmt.Errorf("root missing snapshot role")
	}

	if err := VerifyTUFSignatures(&envelope, root.Keys, role); err != nil {
		return nil, fmt.Errorf("snapshot signature: %w", err)
	}

	var signed TUFSnapshotSigned
	if err := json.Unmarshal(envelope.Signed, &signed); err != nil {
		return nil, fmt.Errorf("parse snapshot signed: %w", err)
	}

	if time.Now().After(signed.Expires) {
		return nil, fmt.Errorf("snapshot metadata expired at %s", signed.Expires.Format(time.RFC3339))
	}

	return &signed, nil
}

// ParseTargets parses and verifies a targets.json against the root.
func ParseTargets(data []byte, root *TUFRootSigned) (*TUFTargetsSigned, error) {
	var envelope TUFSigned
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("parse targets envelope: %w", err)
	}

	role, ok := root.Roles["targets"]
	if !ok {
		return nil, fmt.Errorf("root missing targets role")
	}

	if err := VerifyTUFSignatures(&envelope, root.Keys, role); err != nil {
		return nil, fmt.Errorf("targets signature: %w", err)
	}

	var signed TUFTargetsSigned
	if err := json.Unmarshal(envelope.Signed, &signed); err != nil {
		return nil, fmt.Errorf("parse targets signed: %w", err)
	}

	if time.Now().After(signed.Expires) {
		return nil, fmt.Errorf("targets metadata expired at %s", signed.Expires.Format(time.RFC3339))
	}

	return &signed, nil
}
