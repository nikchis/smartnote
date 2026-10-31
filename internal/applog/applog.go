// Package applog opens Smartnote's error log and describes errors for it
// without secrets.
package applog

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// logFile is the name of the log file in the state directory.
const logFile = "smartnote.log"

// errKey is the attribute key of the error in a log record.
const errKey = "err"

// Open opens smartnote.log in dir for appending, creating dir (0700) and the
// file (0600) when needed. The logger writes records of level Error and above
// in slog's text format. The closer closes the file.
func Open(dir string) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}

	f, err := os.OpenFile(filepath.Join(dir, logFile), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open log: %w", err)
	}

	h := slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelError})

	return slog.New(h), f, nil
}

// Err returns the log attribute for err with its text. An error from pgx or
// PostgreSQL is redacted as by [Redacted], since its text names the user,
// the database and the host.
func Err(err error) slog.Attr {
	if hasPgconnError(err) {
		return Redacted(err)
	}

	return slog.String(errKey, err.Error())
}

// Redacted returns the log attribute for err without any of its text: the Go
// types along its chain and, for a PostgreSQL error, the SQLSTATE. Texts of
// SSH, pgx and PostgreSQL errors carry the user, the database and the host.
func Redacted(err error) slog.Attr {
	attrs := []any{slog.String("type", typeChain(err))}

	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		attrs = append(attrs, slog.String("sqlstate", pgErr.Code))
	}

	return slog.Group(errKey, attrs...)
}

// typeChain lists the types of err and of the errors it wraps, depth first,
// for example "*fmt.wrapError > *pgconn.ConnectError > *net.OpError".
func typeChain(err error) string {
	var types []string

	var walk func(error)

	walk = func(e error) {
		if e == nil {
			return
		}

		types = append(types, reflect.TypeOf(e).String())

		switch u := e.(type) { //nolint:errorlint // walks the chain itself, wrapped errors included
		case interface{ Unwrap() error }:
			walk(u.Unwrap())
		case interface{ Unwrap() []error }:
			for _, inner := range u.Unwrap() {
				walk(inner)
			}
		}
	}
	walk(err)

	return strings.Join(types, " > ")
}

// hasPgconnError reports whether err wraps an error of pgconn whose text
// names the user, the database or the host.
func hasPgconnError(err error) bool {
	if _, ok := errors.AsType[*pgconn.PgError](err); ok {
		return true
	}

	if _, ok := errors.AsType[*pgconn.ConnectError](err); ok {
		return true
	}

	_, ok := errors.AsType[*pgconn.ParseConfigError](err)

	return ok
}
