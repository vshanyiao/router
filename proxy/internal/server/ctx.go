package server

import (
	"context"
	"time"
)

// contextWithoutCancel returns a context whose Done is detached from parent
// cancellation but inherits parent values. Capped by `timeout` to bound any
// post-request cleanup work (billing finalize, log push) even if the original
// request context was cancelled by client disconnect.
//
// Go 1.21+ provides context.WithoutCancel; this wrapper composes it with a
// timeout deadline so callers can defer the returned cancel func.
func contextWithoutCancel(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	detached := context.WithoutCancel(parent)
	return context.WithTimeout(detached, timeout)
}
