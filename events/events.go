package events

import (
	"sync"

	log "github.com/Sirupsen/logrus"
)

// Event ...
type Event struct {
	Code   EventCode
	Source string
}

// go:generate stringer -type EventCode

// EventCode ...
type EventCode int

// EventCode enum
const (
	None        EventCode = iota // placeholder nil-event
	ExitSuccess                  // emitted when a Runner's exec completes with 0 exit code
	ExitFailed                   // emitted when a Runner's exec completes with non-0 exit code
	Stopping                     // emitted when a Runner is about to stop
	Stopped                      // emitted when a Runner has stopped
	StatusHealthy
	StatusUnhealthy
	StatusChanged
	TimerExpired
	EnterMaintenance
	ExitMaintenance
	Error
	Quit
	Startup  // fired once after events are set up and event loop is started
	Shutdown // fired once after all jobs exit or on receiving SIGTERM
)

// global events
var (
	GlobalStartup  = Event{Code: Startup, Source: "global"}
	GlobalShutdown = Event{Code: Shutdown, Source: "global"}
	QuitByClose    = Event{Code: Quit, Source: "closed"}
	NonEvent       = Event{Code: None, Source: ""}
)

// EventBus ...
type EventBus struct {
	registry  map[Subscriber]bool
	lock      *sync.RWMutex
	reloading bool
	reloaded  chan bool
	done      chan bool
}

// NewEventBus ...
func NewEventBus() *EventBus {
	lock := &sync.RWMutex{}
	reg := make(map[Subscriber]bool)
	done := make(chan bool, 1)
	reloaded := make(chan bool, 1)
	bus := &EventBus{registry: reg, lock: lock, done: done, reloaded: reloaded}
	return bus
}

// Register the Subscriber for all Events
func (bus *EventBus) Register(subscriber Subscriber) {
	bus.lock.Lock()
	defer bus.lock.Unlock()
	bus.registry[subscriber] = true
}

// Unregister the Subscriber from all Events
func (bus *EventBus) Unregister(subscriber Subscriber) {
	bus.lock.Lock()
	defer bus.lock.Unlock()
	if _, ok := bus.registry[subscriber]; ok {
		delete(bus.registry, subscriber)
	}
	if len(bus.registry) == 0 {
		if bus.reloading {
			bus.reloaded <- true
		} else {
			bus.done <- true
		}
	}
}

// Publish an Event to all Subscribers
func (bus *EventBus) Publish(event Event) {
	log.Debugf("event: %v", event)
	bus.lock.RLock()
	defer bus.lock.RUnlock()
	for subscriber := range bus.registry {
		// sending to an unsubscribed Subscriber shouldn't be a runtime
		// error, so this is in intentionally allowed to panic here
		subscriber.Receive(event)
	}
}

// Reload asks all Subscribers to halt by sending the GlobalShutdown
// message but sets a flag so we don't send to the done channel,
// which will cause us to exit entirely. Instead we'll wait until
// the EventBus registry is unpopulated.
func (bus *EventBus) Reload() {
	bus.lock.Lock()
	bus.reloading = true
	bus.lock.Unlock()

	// need this check to ensure we will finish reload even if we have
	// no running services to receive the shutdown signal and tell us
	// we're done
	if len(bus.registry) > 0 {
		bus.Publish(GlobalShutdown)
		<-bus.reloaded
	} else {
		bus.Publish(GlobalShutdown)
	}

	bus.lock.Lock()
	bus.reloading = false
	bus.lock.Unlock()
}

// Shutdown asks all Subscribers to halt by sending the GlobalShutdown
// message. Subscribers are responsible for handling this message.
func (bus *EventBus) Shutdown() {
	bus.Publish(GlobalShutdown)
}

// Wait blocks until the EventBus registry is unpopulated
func (bus *EventBus) Wait() {
	<-bus.done
	close(bus.done)
}
