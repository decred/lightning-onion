package sphinx

import (
	"crypto/sha256"
	"errors"
)

const (
	// HashPrefixSize is the size in bytes of the keys we will be storing
	// in the ReplayLog. It represents the first 20 bytes of a truncated
	// sha-256 hash of a secret generated by ECDH.
	HashPrefixSize = 20
)

// HashPrefix is a statically size, 20-byte array containing the prefix
// of a Hash256, and is used to detect duplicate sphinx packets.
type HashPrefix [HashPrefixSize]byte

var (
	// errReplayLogNotStarted is an error returned when methods other than Start()
	// are called on a ReplayLog before it is started or after it is stopped.
	errReplayLogNotStarted = errors.New("replay log has not been started")
)

// hashSharedSecret Sha-256 hashes the shared secret and returns the first
// HashPrefixSize bytes of the hash.
func hashSharedSecret(sharedSecret *Hash256) *HashPrefix {
	// Sha256 hash of sharedSecret
	h := sha256.New()
	h.Write(sharedSecret[:])

	var sharedHash HashPrefix

	// Copy bytes to sharedHash
	copy(sharedHash[:], h.Sum(nil))
	return &sharedHash
}

// ReplayLog is an interface that defines a log of incoming sphinx packets,
// enabling strong replay protection. The interface is general to allow
// implementations near-complete autonomy. All methods must be safe for
// concurrent access.
type ReplayLog interface {
	// Start starts up the log. It returns an error if one occurs.
	Start() error

	// Stop safely stops the log. It returns an error if one occurs.
	Stop() error

	// Get retrieves an entry from the log given its hash prefix. It returns the
	// value stored and an error if one occurs. It returns ErrLogEntryNotFound
	// if the entry is not in the log.
	Get(*HashPrefix) (uint32, error)

	// Put stores an entry into the log given its hash prefix and an
	// accompanying purposefully general type. It returns ErrReplayedPacket if
	// the provided hash prefix already exists in the log.
	Put(*HashPrefix, uint32) error

	// Delete deletes an entry from the log given its hash prefix.
	Delete(*HashPrefix) error

	// PutBatch stores a batch of sphinx packets into the log given their hash
	// prefixes and accompanying values. Returns the set of entries in the batch
	// that are replays and an error if one occurs.
	PutBatch(*Batch) (*ReplaySet, error)
}

// MemoryReplayLog is a simple ReplayLog implementation that stores all added
// sphinx packets and processed batches in memory with no persistence.
//
// This is designed for use just in testing.
type MemoryReplayLog struct {
	batches map[string]*ReplaySet
	entries map[HashPrefix]uint32
}

// NewMemoryReplayLog constructs a new MemoryReplayLog.
func NewMemoryReplayLog() *MemoryReplayLog {
	return &MemoryReplayLog{}
}

// Start initializes the log and must be called before any other methods.
func (rl *MemoryReplayLog) Start() error {
	rl.batches = make(map[string]*ReplaySet)
	rl.entries = make(map[HashPrefix]uint32)
	return nil
}

// Stop wipes the state of the log.
func (rl *MemoryReplayLog) Stop() error {
	if rl.entries == nil || rl.batches == nil {
		return errReplayLogNotStarted
	}

	rl.batches = nil
	rl.entries = nil
	return nil
}

// Get retrieves an entry from the log given its hash prefix. It returns the
// value stored and an error if one occurs. It returns ErrLogEntryNotFound
// if the entry is not in the log.
func (rl *MemoryReplayLog) Get(hash *HashPrefix) (uint32, error) {
	if rl.entries == nil || rl.batches == nil {
		return 0, errReplayLogNotStarted
	}

	cltv, exists := rl.entries[*hash]
	if !exists {
		return 0, ErrLogEntryNotFound
	}

	return cltv, nil
}

// Put stores an entry into the log given its hash prefix and an accompanying
// purposefully general type. It returns ErrReplayedPacket if the provided hash
// prefix already exists in the log.
func (rl *MemoryReplayLog) Put(hash *HashPrefix, cltv uint32) error {
	if rl.entries == nil || rl.batches == nil {
		return errReplayLogNotStarted
	}

	_, exists := rl.entries[*hash]
	if exists {
		return ErrReplayedPacket
	}

	rl.entries[*hash] = cltv
	return nil
}

// Delete deletes an entry from the log given its hash prefix.
func (rl *MemoryReplayLog) Delete(hash *HashPrefix) error {
	if rl.entries == nil || rl.batches == nil {
		return errReplayLogNotStarted
	}

	delete(rl.entries, *hash)
	return nil
}

// PutBatch stores a batch of sphinx packets into the log given their hash
// prefixes and accompanying values. Returns the set of entries in the batch
// that are replays and an error if one occurs.
func (rl *MemoryReplayLog) PutBatch(batch *Batch) (*ReplaySet, error) {
	if rl.entries == nil || rl.batches == nil {
		return nil, errReplayLogNotStarted
	}

	// Return the result when the batch was first processed to provide
	// idempotence.
	replays, exists := rl.batches[string(batch.ID)]

	if !exists {
		replays = NewReplaySet()
		err := batch.ForEach(func(seqNum uint16, hashPrefix *HashPrefix, cltv uint32) error {
			err := rl.Put(hashPrefix, cltv)
			if errors.Is(err, ErrReplayedPacket) {
				replays.Add(seqNum)
				return nil
			}

			// An error would be bad because we have already updated the entries
			// map, but no errors other than ErrReplayedPacket should occur.
			return err
		})
		if err != nil {
			return nil, err
		}

		replays.Merge(batch.ReplaySet)
		rl.batches[string(batch.ID)] = replays
	}

	batch.ReplaySet = replays
	batch.IsCommitted = true

	return replays, nil
}

// A compile time asserting *MemoryReplayLog implements the RelayLog interface.
var _ ReplayLog = (*MemoryReplayLog)(nil)
