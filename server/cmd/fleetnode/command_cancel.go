package main

import (
	"context"
	"sync"
)

// commandCancels holds the cancel function of each in-flight command that a
// ControlCancel from the server may stop. Commands that must run to their end,
// such as physical writes, are never registered, so a cancel for one is
// ignored. See fleetnodecontrol.CommandCancellable for the rule.
type commandCancels struct {
	mu      sync.Mutex
	running map[string]*commandCancel
}

type commandCancel struct{ cancel context.CancelFunc }

// track returns the context a command runs under and a function to call when
// it finishes. A cancellable command's context ends on a cancel for commandID;
// any other command runs under parent unchanged.
func (c *commandCancels) track(parent context.Context, commandID string, cancellable bool) (context.Context, func()) {
	if !cancellable || commandID == "" {
		return parent, func() {}
	}
	ctx, cancel := context.WithCancel(parent)
	entry := &commandCancel{cancel: cancel}
	c.mu.Lock()
	if c.running == nil {
		c.running = make(map[string]*commandCancel)
	}
	// A repeated command_id keeps the first command cancellable.
	if _, dup := c.running[commandID]; !dup {
		c.running[commandID] = entry
	}
	c.mu.Unlock()
	return ctx, func() {
		cancel()
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.running[commandID] == entry {
			delete(c.running, commandID)
		}
	}
}

// cancel stops the command running under commandID, if it is cancellable and
// still in flight.
func (c *commandCancels) cancel(commandID string) {
	c.mu.Lock()
	entry := c.running[commandID]
	c.mu.Unlock()
	if entry != nil {
		entry.cancel()
	}
}
