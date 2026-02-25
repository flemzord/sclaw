package channel

import "errors"

// Sentinel errors for channel operations.
var (
	// ErrNoChannel indicates the outbound message targets a channel that is
	// not registered in the dispatcher.
	ErrNoChannel = errors.New("channel: unknown channel")

	// ErrDuplicateChannel indicates a channel with the same name is already
	// registered in the dispatcher.
	ErrDuplicateChannel = errors.New("channel: duplicate channel name")

	// ErrNoInbox indicates a channel's inbox callback has not been set.
	ErrNoInbox = errors.New("channel: inbox not set")

	// ErrDenied indicates the message was blocked by the allow-list.
	ErrDenied = errors.New("channel: sender not allowed")
)
