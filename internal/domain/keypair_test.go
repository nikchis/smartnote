package domain

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestKeyPair_Redacted(t *testing.T) {
	k := KeyPair{PrivatePEM: []byte("private-marker"), PublicAuthorized: "ssh-ed25519 public-marker"}
	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("key", "key", k, "ptr", &k)

	outputs := map[string]string{
		"%v":   fmt.Sprintf("%v", k),
		"%+v":  fmt.Sprintf("%+v", k),
		"%#v":  fmt.Sprintf("%#v", k),
		"ptr":  fmt.Sprintf("%+v", &k),
		"slog": logged.String(),
	}
	for verb, out := range outputs {
		for _, m := range []string{"private-marker", "public-marker"} {
			if strings.Contains(out, m) {
				t.Errorf("%s output %q contains %q", verb, out, m)
			}
		}
	}
}
