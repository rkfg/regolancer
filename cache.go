package main

import (
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

func lock() *flock.Flock {
	return flock.New(filepath.Join(os.TempDir(), "regolancer.lock"))
}

func (r *regolancer) loadNodeCache(filename string, exp int, doLock bool) error {
	if filename == "" {
		return nil
	}
	defer func() {
		err := recover()
		if err == nil {
			return
		}
		log.Printf("Loading failed, cache format might be outdated: %s", err)
		r.nodeCache = map[string]cachedNodeInfo{}
	}()
	if doLock {
		log.Printf("Loading node cache from %s", filename)
		l := lock()
		err := l.RLock()
		defer l.Unlock()

		if err != nil {
			return fmt.Errorf("error take shared lock on file %s: %s", filename, err)
		}
	}
	f, err := os.Open(filename)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("error opening node cache file: %s", err)
		}
		return nil
	}
	defer f.Close()
	err = gob.NewDecoder(f).Decode(&r.nodeCache)
	if err != nil {
		return err
	}
	for k, v := range r.nodeCache {
		since := time.Since(v.Timestamp)
		if since > time.Minute*time.Duration(exp) {
			delete(r.nodeCache, k)
		}
	}
	return nil
}

func (r *regolancer) saveNodeCache(filename string, exp int) error {
	if filename == "" {
		return nil
	}
	log.Printf("Saving node cache to %s", filename)

	l := lock()
	err := l.Lock()
	defer l.Unlock()

	if err != nil {
		return fmt.Errorf("error take exclusive lock on file %s: %s", filename, err)
	}

	old := regolancer{nodeCache: map[string]cachedNodeInfo{}}
	err = old.loadNodeCache(filename, exp, false)

	if err != nil {
		logErrorF("Error merging cache, saving anew: %s", err)
	}
	for k, v := range old.nodeCache {
		if n, ok := r.nodeCache[k]; !ok ||
			n.Timestamp.Before(v.Timestamp) {
			r.nodeCache[k] = v
		}
	}

	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("error creating node cache file: %s", err)
	}
	defer f.Close()
	err = gob.NewEncoder(f).Encode(r.nodeCache)
	return err
}
