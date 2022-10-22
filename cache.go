package main

import (
	"encoding/gob"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

func lock() *flock.Flock {
	return flock.New(filepath.Join(os.TempDir(), "regolancer.lock"))
}

func (r *regolancer) loadNodeCache(filename string, exp int) {
	if filename == "" {
		return
	}
	l := lock()
	l.RLock()
	defer l.Unlock()
	f, err := os.Open(filename)
	if err != nil {
		if !os.IsNotExist(err) {
			logErrorF("Error opening node cache file: %s", err)
		}
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		logErrorF("Error getting node cache information: %s", err)
		return
	}
	if time.Since(fi.ModTime()) > time.Minute*time.Duration(exp) {
		log.Print("Node cache expired, not loading")
		return
	}
	log.Printf("Loading node cache from %s", filename)
	gob.NewDecoder(f).Decode(&r.nodeCache)
}

func (r *regolancer) saveNodeCache(filename string) {
	if filename == "" {
		return
	}
	log.Printf("Saving node cache to %s", filename)
	l := lock()
	l.Lock()
	defer l.Unlock()
	f, err := os.Create(filename)
	if err != nil {
		logErrorF("Error creating node cache file %s: %s", filename, err)
		return
	}
	defer f.Close()
	gob.NewEncoder(f).Encode(r.nodeCache)
}
