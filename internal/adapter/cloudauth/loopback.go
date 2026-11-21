package cloudauth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// callback is the outcome of the first callback with the right state.
type callback struct {
	code string
	err  error
}

// loopbackLogin ends with the browser's redirect to its receiver (spec §6.2).
// It owns the goroutine that serves the receiver: Close and the end of Wait
// stop the server and wait for that goroutine.
type loopbackLogin struct {
	session

	deadline time.Time // of the wait for the browser
	srv      *http.Server
	ln       net.Listener
	served   chan struct{} // closed when Serve returns
	result   chan callback // the first right callback; buffered, sent once

	// life is the lifetime of the login, not of a request: Close ends it,
	// which cancels a running Wait together with its exchange.
	life context.Context
	end  context.CancelFunc

	answered atomic.Bool // the right callback has been taken
	waited   atomic.Bool // Wait has been called
	stopOnce sync.Once
}

func startLoopback(s session, ln net.Listener, deadline time.Time) *loopbackLogin {
	l := &loopbackLogin{
		session:  s,
		deadline: deadline,
		ln:       ln,
		served:   make(chan struct{}),
		result:   make(chan callback, 1),
	}
	l.life, l.end = context.WithCancel(context.Background())
	l.srv = &http.Server{
		Handler:           l,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          log.New(io.Discard, "", 0), // its messages may quote requests
	}

	go func() {
		defer close(l.served)

		_ = l.srv.Serve(l.ln) // ends with stop, or when the handler closes the listener
	}()

	return l
}

// Wait waits for the callback, at most until the deadline counted from
// BeginLoopback, then exchanges the code. A refusal in the browser and a
// Close return context.Canceled. Only the first Wait waits; later ones
// return domain.ErrNoPendingOperation. The receiver is stopped when Wait
// returns.
func (l *loopbackLogin) Wait(ctx context.Context) (usecase.CloudToken, error) {
	if l.life.Err() != nil {
		return usecase.CloudToken{}, context.Canceled
	}

	if !l.waited.CompareAndSwap(false, true) {
		return usecase.CloudToken{}, fmt.Errorf("wait for the browser: %w", domain.ErrNoPendingOperation)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stopAfter := context.AfterFunc(l.life, cancel)
	defer stopAfter()

	cb, err := l.receive(ctx)
	l.stop()

	switch {
	case err != nil:
		return usecase.CloudToken{}, err
	case cb.err != nil:
		return usecase.CloudToken{}, cb.err
	}

	tok, err := l.redeem(ctx, cb.code)
	if l.life.Err() != nil {
		// Closed during the exchange: even a finished token is dropped.
		return usecase.CloudToken{}, context.Canceled
	}

	return tok, err
}

// receive returns the right callback, or the error of ctx or of the deadline.
func (l *loopbackLogin) receive(ctx context.Context) (callback, error) {
	wait, cancel := context.WithDeadline(ctx, l.deadline)
	defer cancel()

	select {
	case cb := <-l.result:
		return cb, nil
	case <-wait.Done():
		if l.life.Err() != nil {
			return callback{}, context.Canceled
		}

		return callback{}, fmt.Errorf("wait for the browser: %w", wait.Err())
	}
}

// Exchange is not part of a loopback login.
func (*loopbackLogin) Exchange(context.Context, string) (usecase.CloudToken, error) {
	return usecase.CloudToken{}, fmt.Errorf("exchange the code: %w", domain.ErrNoPendingOperation)
}

// Close cancels a running Wait and stops the receiver. It is idempotent.
func (l *loopbackLogin) Close() error {
	l.end()
	l.stop()

	return nil
}

// stop shuts the server down and waits for its goroutine; later calls wait
// for the first one to finish.
func (l *loopbackLogin) stop() {
	l.stopOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := l.srv.Shutdown(ctx); err != nil {
			_ = l.srv.Close() // connections still busy after shutdownTimeout
		}

		<-l.served
	})
}

// ServeHTTP answers GET /callback. The state is checked first, in constant
// time, for every callback: a wrong or missing one is 400 and the wait goes
// on, so another page cannot end the sign-in (spec §6.2, §10). The first
// right callback with a code or an error ends it and closes the listener.
func (l *loopbackLogin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != l.a.p.CallbackPath {
		pageNotFound.write(w, l.a, false)
		return
	}

	q := r.URL.Query()
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(l.state)) != 1 {
		pageExpired.write(w, l.a, false)
		return
	}

	cb, ok := l.callbackOf(q)
	if !ok || !l.answered.CompareAndSwap(false, true) {
		pageExpired.write(w, l.a, false)
		return
	}

	l.result <- cb

	switch {
	case cb.err == nil:
		pageSignedIn.write(w, l.a, true)
	case errors.Is(cb.err, context.Canceled):
		pageCancelled.write(w, l.a, true)
	default:
		pageFailed.write(w, l.a, true)
	}

	_ = l.ln.Close() // no more connections; stop ends the open ones
}

// callbackOf reads the outcome of a callback with the right state: a code,
// a refusal (context.Canceled) or another OAuth error by its known name. It
// reports false for a callback with neither.
func (l *loopbackLogin) callbackOf(q url.Values) (callback, bool) {
	switch name := q.Get("error"); {
	case name == errorAccessDenied:
		return callback{err: context.Canceled}, true
	case name != "":
		return callback{err: fmt.Errorf("sign-in refused: %w", cloudhttp.NewAnswerError(l.a.p.Kind, 0, knownCode(name), cloudhttp.ReasonNone))}, true
	case q.Get(paramCode) != "":
		return callback{code: q.Get(paramCode)}, true
	}

	return callback{}, false
}

// onceCloseListener lets the handler and the server both close the
// listener; only the first Close reaches it.
type onceCloseListener struct {
	net.Listener

	once sync.Once
	err  error
}

func (l *onceCloseListener) Close() error {
	l.once.Do(func() { l.err = l.Listener.Close() })
	return l.err
}
