package utils

import "sync"

type DomainCache struct {
	domains map[string]bool
	mu      sync.RWMutex
}

type UrlMemory struct {
	urls map[string]struct{}
	mu   sync.RWMutex
}

func InitStore() (*DomainCache, *UrlMemory) {
	return &DomainCache{make(map[string]bool), sync.RWMutex{}},
		&UrlMemory{make(map[string]struct{}), sync.RWMutex{}}
}

func (d *DomainCache) CheckExisting(domain string) (bool, bool) {
	d.mu.RLock()
	allowed, exists := d.domains[domain]
	if !exists {
		d.mu.RUnlock()
		return false, false
	}
	d.mu.RUnlock()
	return allowed, true
}

func (d *DomainCache) Add(domain string, allowed bool) {
	d.mu.Lock()
	d.domains[domain] = allowed
	d.mu.Unlock()
}

func (u *UrlMemory) CheckExisting(url string) bool {
	u.mu.RLock()
	_, exists := u.urls[url]
	if !exists {
		u.mu.RUnlock()
		return false
	}
	u.mu.RUnlock()
	return true
}

func (u *UrlMemory) Add(url string) {
	u.mu.Lock()
	u.urls[url] = struct{}{}
	u.mu.Unlock()
}
