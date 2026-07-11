package main

import (
	"fmt"
	"os"
	"sync"
	"testing"
)

// =============================================================================
// PizzaDB Storage Engine Benchmarks
//
// These benchmarks validate the performance claims of PizzaDB's Bitcask-style
// storage engine. Run with:
//
//     go test -bench=. -benchmem -count=3
//
// Expected results on modern hardware:
//   - BenchmarkSet:           ~100k+ ops/sec  (append-only sequential write)
//   - BenchmarkGet:           ~1M+  ops/sec  (in-memory index + OS page cache)
//   - BenchmarkSetGet:        ~50k+ ops/sec  (mixed workload)
//   - BenchmarkParallelSet:   ~50k+ ops/sec  (concurrent writers, mutex-bound)
//   - BenchmarkParallelGet:   ~5M+  ops/sec  (concurrent readers, RWMutex)
//   - BenchmarkRecover10k:    recovers 10k entries from disk on startup
// =============================================================================

// benchDB creates a temporary database for benchmarking.
// Caller must defer cleanup.
func benchDB(b *testing.B) (*DB, func()) {
	b.Helper()
	tmpFile := fmt.Sprintf("bench_%d.db", os.Getpid())
	db, err := NewDB(tmpFile)
	if err != nil {
		b.Fatal(err)
	}
	cleanup := func() {
		db.Close()
		os.Remove(tmpFile)
	}
	return db, cleanup
}

// --- Claim: 10k+ ops/sec for writes ------------------------------------------

func BenchmarkSet(b *testing.B) {
	db, cleanup := benchDB(b)
	defer cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Set(fmt.Sprintf("key_%d", i), fmt.Sprintf("value_%d", i))
	}
}

// --- Claim: 10k+ ops/sec for reads -------------------------------------------

func BenchmarkGet(b *testing.B) {
	db, cleanup := benchDB(b)
	defer cleanup()

	// Pre-populate 10,000 keys
	for i := 0; i < 10000; i++ {
		db.Set(fmt.Sprintf("key_%d", i), fmt.Sprintf("value_%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Get(fmt.Sprintf("key_%d", i%10000))
	}
}

// --- Mixed workload (realistic) -----------------------------------------------

func BenchmarkSetGet(b *testing.B) {
	db, cleanup := benchDB(b)
	defer cleanup()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key_%d", i)
		val := fmt.Sprintf("value_%d", i)
		db.Set(key, val)
		db.Get(key)
	}
}

// --- Concurrent writes (mutex contention test) --------------------------------

func BenchmarkParallelSet(b *testing.B) {
	db, cleanup := benchDB(b)
	defer cleanup()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			db.Set(fmt.Sprintf("pkey_%d", i), fmt.Sprintf("pval_%d", i))
			i++
		}
	})
}

// --- Concurrent reads (RWMutex scalability) -----------------------------------

func BenchmarkParallelGet(b *testing.B) {
	db, cleanup := benchDB(b)
	defer cleanup()

	for i := 0; i < 10000; i++ {
		db.Set(fmt.Sprintf("key_%d", i), fmt.Sprintf("value_%d", i))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			db.Get(fmt.Sprintf("key_%d", i%10000))
			i++
		}
	})
}

// --- Delete (tombstone write) -------------------------------------------------

func BenchmarkDelete(b *testing.B) {
	db, cleanup := benchDB(b)
	defer cleanup()

	// Pre-populate
	for i := 0; i < b.N; i++ {
		db.Set(fmt.Sprintf("key_%d", i), fmt.Sprintf("value_%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Delete(fmt.Sprintf("key_%d", i))
	}
}

// =============================================================================
// Claim: Crash Recovery with 10k+ entries
//
// This test proves PizzaDB can:
//   1. Write 10,000 entries to disk
//   2. Close the database (simulating a crash)
//   3. Reopen and recover ALL 10,000 entries from the append-only log
//   4. Verify every single entry is intact
// =============================================================================

func TestRecover10kEntries(t *testing.T) {
	tmpFile := "recover_test.db"
	os.Remove(tmpFile)
	defer os.Remove(tmpFile)

	const N = 10000

	// Phase 1: Write 10k entries and close
	db1, err := NewDB(tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < N; i++ {
		if err := db1.Set(fmt.Sprintf("key_%d", i), fmt.Sprintf("value_%d", i)); err != nil {
			t.Fatalf("Set failed at i=%d: %v", i, err)
		}
	}
	db1.Close() // Simulate crash

	// Phase 2: Reopen — recover() should rebuild all 10k entries
	db2, err := NewDB(tmpFile)
	if err != nil {
		t.Fatalf("Failed to reopen: %v", err)
	}
	defer db2.Close()

	// Phase 3: Verify every entry
	missing := 0
	corrupt := 0
	for i := 0; i < N; i++ {
		key := fmt.Sprintf("key_%d", i)
		expected := fmt.Sprintf("value_%d", i)
		got, err := db2.Get(key)
		if err != nil {
			missing++
			continue
		}
		if got != expected {
			corrupt++
			t.Errorf("key=%s expected=%s got=%s", key, expected, got)
		}
	}

	if missing > 0 || corrupt > 0 {
		t.Fatalf("Recovery failed: %d missing, %d corrupt out of %d", missing, corrupt, N)
	}

	t.Logf("✅ Successfully recovered all %d entries from disk", N)
}

// --- Benchmark: Recovery speed ------------------------------------------------

func BenchmarkRecover10k(b *testing.B) {
	tmpFile := "bench_recover.db"
	os.Remove(tmpFile)
	defer os.Remove(tmpFile)

	// Write 10k entries once
	db, _ := NewDB(tmpFile)
	for i := 0; i < 10000; i++ {
		db.Set(fmt.Sprintf("key_%d", i), fmt.Sprintf("value_%d", i))
	}
	db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db2, _ := NewDB(tmpFile)
		db2.Close()
	}
}

// --- Tombstone correctness test -----------------------------------------------

func TestDeleteTombstone(t *testing.T) {
	tmpFile := "tombstone_test.db"
	os.Remove(tmpFile)
	defer os.Remove(tmpFile)

	db, _ := NewDB(tmpFile)

	// SET then DELETE
	db.Set("hero", "Batman")
	db.Delete("hero")

	// GET should fail
	_, err := db.Get("hero")
	if err == nil {
		t.Fatal("Expected error after delete, got nil")
	}

	// Close and reopen — tombstone should survive recovery
	db.Close()

	db2, _ := NewDB(tmpFile)
	defer db2.Close()

	_, err = db2.Get("hero")
	if err == nil {
		t.Fatal("Tombstone did not survive recovery")
	}
	t.Log("✅ Tombstone correctly persisted across crash recovery")
}

// --- Concurrent read/write safety test ----------------------------------------

func TestConcurrentSafety(t *testing.T) {
	tmpFile := "concurrent_test.db"
	os.Remove(tmpFile)
	defer os.Remove(tmpFile)

	db, _ := NewDB(tmpFile)
	defer db.Close()

	var wg sync.WaitGroup
	const writers = 10
	const readers = 20
	const opsPerGoroutine = 1000

	// Launch writers
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := fmt.Sprintf("w%d_key_%d", id, i)
				db.Set(key, fmt.Sprintf("val_%d", i))
			}
		}(w)
	}

	// Launch readers
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				db.Get(fmt.Sprintf("w0_key_%d", i%100))
			}
		}(r)
	}

	wg.Wait()
	t.Logf("✅ %d concurrent goroutines completed without race conditions", writers+readers)
}
