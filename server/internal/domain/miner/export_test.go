package miner

import (
	"time"

	"github.com/block/proto-fleet/server/internal/domain/miner/interfaces"
	lru "github.com/hashicorp/golang-lru/v2/expirable"
)

// SetReadCacheTTL changes only the test binary's cache before its first lookup.
func SetReadCacheTTL(service *Service, ttl time.Duration) {
	service.cache = lru.NewLRU[string, interfaces.Miner](minerCacheSize, nil, ttl)
}
