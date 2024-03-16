package quicktar

import (
	"math/rand"
	"os"
	"sync"
	"time"
)

type fdTimer struct {
	fd    *os.File
	timer *time.Timer
}

type fdCache struct {
	size    int
	timeout time.Duration
	mux     sync.Mutex

	// cache stores at most `size` fds.
	cache []*os.File

	// gcQueue stores fds that cannot be put into cache.
	// Fds in gcQueue will be closed after `timeout`.
	gcQueue []fdTimer
}

func newFdCache(fd ...*os.File) *fdCache {
	cache := make([]*os.File, len(fd))
	copy(cache, fd)
	return &fdCache{
		size:    len(fd),
		cache:   cache,
		gcQueue: []fdTimer{},
	}
}

func (c *fdCache) acquire() *os.File {
	c.mux.Lock()
	defer c.mux.Unlock()

	// Pop from the back of gcQueue until an fd that hasn't been closed is found.
	for n := len(c.gcQueue); n > 0; n-- {
		item := c.gcQueue[n-1]
		c.gcQueue = c.gcQueue[:n-1]
		if item.timer.Stop() {
			return item.fd
		}
	}

	// Pick a random fd from cache.
	if n := len(c.cache); n > 0 {
		k := rand.Intn(n)
		fd := c.cache[k]
		c.cache[k] = c.cache[n-1]
		c.cache = c.cache[:n-1]
		return fd
	}

	return nil
}

func (c *fdCache) release(fd *os.File) error {
	c.mux.Lock()
	defer c.mux.Unlock()

	// Put into cache if not full.
	if len(c.cache) < c.size {
		c.cache = append(c.cache, fd)
		return nil
	}

	// Close immediately if timeout is set to zero.
	if c.timeout <= time.Duration(0) {
		return fd.Close()
	}

	// Push to the head of gcQueue and setup timer.
	c.gcQueue = append(c.gcQueue, fdTimer{})
	copy(c.gcQueue[1:], c.gcQueue[0:])
	c.gcQueue[0] = fdTimer{
		fd: fd,
		timer: time.AfterFunc(c.timeout, func() {
			fd.Close()
		}),
	}
	return nil
}
