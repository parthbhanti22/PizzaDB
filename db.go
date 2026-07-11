package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
)

// tombstoneValue is the sentinel written to disk when a key is deleted.
// On recovery and Get, entries with this value are treated as non-existent.
const tombstoneValue = "__PIZZADB_TOMBSTONE__"

//DB struct is thr database instance
type DB struct {
	file *os.File
	keyDir map[string]LogEntry  //index/hashing in the RAM
	mu sync.RWMutex //Thread safety locks
}

//LogEntry will tell us how to find the data on the disk
type LogEntry struct {
	Offset int64
	TotalSize int64
}

//NewDB will initialize the database
func NewDB(filename string) (*DB, error) {
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	db := &DB{
		file:   file,
		keyDir: make(map[string]LogEntry),
	}

	// --- NEW CODE START ---
	// Rebuild the index from disk
	err = db.recover()
	if err != nil {
		return nil, fmt.Errorf("failed to recover db: %v", err)
	}
	// --- NEW CODE END ---
	
	return db, nil
}

//set writes a key/value pair to the log
func(db *DB) Set(key, value string) error {
	db.mu.Lock() //lock database
	defer db.mu.Unlock() //unlock database

	//prepare the data
	data := Encode(key, value)

	//find where we are currently at in the file(offset)
	stat, _ := db.file.Stat()
	offset := stat.Size()

	//write to the disk
	_, err := db.file.Write(data)
	if err != nil {
		return err
	}

	db.keyDir[key] = LogEntry{
		Offset: offset,
		TotalSize: int64(len(data)),
	}

	return nil
	
}

// Get retrieves a value from the log.
// Returns an error if the key does not exist or has been tombstoned (deleted).
func (db *DB) Get(key string) (string, error) {
	db.mu.RLock()         // Read Lock (allows multiple readers, blocks writers)
	defer db.mu.RUnlock()

	// 1. Check if we have the key in RAM
	entry, exists := db.keyDir[key]
	if !exists {
		return "", fmt.Errorf("key not found: %s", key)
	}

	// 2. Jump to the exact spot in the file
	data := make([]byte, entry.TotalSize)
	_, err := db.file.ReadAt(data, entry.Offset)
	if err != nil {
		return "", err
	}

	// 3. Decode the raw bytes
	decoded := Decode(data)

	// 4. Check for tombstone — treat as deleted
	if decoded.Value == tombstoneValue {
		return "", fmt.Errorf("key not found: %s", key)
	}

	return decoded.Value, nil
}

// Delete removes a key by writing a tombstone marker.
// This is the standard Bitcask approach: the old value remains on disk
// but the in-memory index now points to the tombstone entry, causing
// subsequent Gets to return "key not found".
func (db *DB) Delete(key string) error {
	return db.Set(key, tombstoneValue)
}

// recover reads the entire file and populates the keyDir
func (db *DB) recover() error {
	// 1. Go to the beginning of the file
	_, err := db.file.Seek(0, 0)
	if err != nil {
		return err
	}

	var offset int64 = 0
	
	for {
		// 2. Read the Header first (to know how big the entry is)
		headerBuf := make([]byte, HeaderSize)
		_, err := db.file.Read(headerBuf)
		if err != nil {
			// End of File (EOF) means we are done
			break 
		}

		// 3. Decode header to get sizes
		// We can reuse the Decode logic or just peek at the bytes manually
		// We need the sizes to know how much more to read
		// Recall: [0-4] is KeySize, [4-8] is ValSize
		
		// Note: We need binary package imported in db.go now, or move this logic
		// Let's assume you imported "encoding/binary" in db.go
		keyLen := int(binary.LittleEndian.Uint32(headerBuf[0:4]))
		valLen := int(binary.LittleEndian.Uint32(headerBuf[4:8]))
		
		totalSize := HeaderSize + keyLen + valLen

		// 4. Read the Key and Value
		dataBuf := make([]byte, keyLen+valLen)
		_, err = db.file.Read(dataBuf)
		if err != nil {
			return fmt.Errorf("corrupted file at offset %d", offset)
		}

		// 5. Extract the Key and Value (we need them for the map)
		key := string(dataBuf[:keyLen])
		value := string(dataBuf[keyLen:])

		// 6. Update the In-Memory Map (skip tombstoned entries)
		if value == tombstoneValue {
			// Key was deleted — remove from index so it stays dead
			delete(db.keyDir, key)
		} else {
			db.keyDir[key] = LogEntry{
				Offset:    offset,
				TotalSize: int64(totalSize),
			}
		}

		// 7. Move offset forward
		offset += int64(totalSize)
	}
	
	return nil
}

// Close ensures we close the file handle
func (db *DB) Close() {
	db.file.Close()

}
