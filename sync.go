package main

import "sync"

// sync.go — keyed mutex mirroring OSL sync.lock / sync.unlock.

var keyedLocks = struct {
	sync.Mutex
	m map[string]*sync.Mutex
}{m: map[string]*sync.Mutex{}}

func lockKey(name string) {
	keyedLocks.Lock()
	m, ok := keyedLocks.m[name]
	if !ok {
		m = &sync.Mutex{}
		keyedLocks.m[name] = m
	}
	keyedLocks.Unlock()
	m.Lock()
}

func unlockKey(name string) {
	keyedLocks.Lock()
	m, ok := keyedLocks.m[name]
	keyedLocks.Unlock()
	if ok {
		m.Unlock()
	}
}