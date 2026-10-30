package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/nikchis/smartnote/internal/domain"
)

var testKeyPair = domain.KeyPair{PrivatePEM: []byte("private"), PublicAuthorized: "ssh-ed25519 AAAA test"}

func TestNewKeysNil(t *testing.T) {
	if _, err := NewKeys(nil, &fakeKeyTool{}); err == nil {
		t.Error("nil store: want an error")
	}
	if _, err := NewKeys(&fakeKeyStore{}, nil); err == nil {
		t.Error("nil tool: want an error")
	}
}

func TestKeysGenerateAndPublicKey(t *testing.T) {
	store := &fakeKeyStore{}
	k, err := NewKeys(store, &fakeKeyTool{key: testKeyPair})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if has, err := k.HasKey(ctx); err != nil || has {
		t.Fatalf("HasKey before Generate = %v, %v; want false, nil", has, err)
	}
	if _, err := k.PublicKey(ctx); !errors.Is(err, domain.ErrStorageMissing) {
		t.Errorf("PublicKey without a key = %v, want ErrStorageMissing", err)
	}
	if err := k.Generate(ctx); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if has, err := k.HasKey(ctx); err != nil || !has {
		t.Fatalf("HasKey after Generate = %v, %v; want true, nil", has, err)
	}
	pub, err := k.PublicKey(ctx)
	if err != nil {
		t.Fatalf("PublicKey: %v", err)
	}
	if pub != testKeyPair.PublicAuthorized {
		t.Errorf("PublicKey = %q, want %q", pub, testKeyPair.PublicAuthorized)
	}
}

func TestKeysImport(t *testing.T) {
	store := &fakeKeyStore{}
	tool := &fakeKeyTool{key: testKeyPair}
	k, err := NewKeys(store, tool)
	if err != nil {
		t.Fatal(err)
	}

	if err := k.Import(context.Background(), []byte("pem"), "phrase"); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if string(tool.imported) != "pem" || tool.passphrase != "phrase" {
		t.Errorf("tool got %q, %q", tool.imported, tool.passphrase)
	}
	if store.saves != 1 || string(store.key.PrivatePEM) != "private" {
		t.Errorf("store saves = %d", store.saves)
	}
}

func TestKeysImportPassphraseRequired(t *testing.T) {
	store := &fakeKeyStore{}
	k, err := NewKeys(store, &fakeKeyTool{err: domain.ErrKeyPassphraseRequired})
	if err != nil {
		t.Fatal(err)
	}

	if err := k.Import(context.Background(), []byte("pem"), ""); !errors.Is(err, domain.ErrKeyPassphraseRequired) {
		t.Fatalf("Import = %v, want ErrKeyPassphraseRequired", err)
	}
	if err := k.Generate(context.Background()); !errors.Is(err, domain.ErrKeyPassphraseRequired) {
		t.Fatalf("Generate = %v, want the tool error", err)
	}
	if store.saves != 0 {
		t.Errorf("store saves = %d, want 0", store.saves)
	}
}
