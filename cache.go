package main

import (
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/lightningnetwork/lnd/lnrpc"
)

func lock() *flock.Flock {
	return flock.New(filepath.Join(os.TempDir(), "regolancer.lock"))
}

func (r *regolancer) loadNodeCache(filename string, exp int, doLock bool) error {
	if filename == "" {
		return nil
	}
	if doLock {
		log.Printf("Loading node cache from %s", filename)
		l := lock()
		l.RLock()
		defer l.Unlock()
	}
	f, err := os.Open(filename)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("error opening node cache file: %s", err)
		}
		return nil
	}
	defer f.Close()
	gob.NewDecoder(f).Decode(&r.nodeCache)
	for k, v := range r.nodeCache {
		if time.Since(time.Unix(int64(v.Node.LastUpdate), 0)) >
			time.Minute*time.Duration(exp) {
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
	l.Lock()
	defer l.Unlock()

	old := regolancer{nodeCache: map[string]*lnrpc.NodeInfo{}}
	err := old.loadNodeCache(filename, exp, false)

	if err != nil {
		logErrorF("Error merging cache, saving anew: %s", err)
	}
	for k, v := range old.nodeCache {
		if n, ok := r.nodeCache[k]; !ok ||
			n.Node.LastUpdate < v.Node.LastUpdate {
			r.nodeCache[k] = v
		}
	}

	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("error creating node cache file: %s", err)
	}
	defer f.Close()
	gob.NewEncoder(f).Encode(r.nodeCache)
	return nil
}
