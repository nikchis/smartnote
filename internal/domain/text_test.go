package domain

import (
	"encoding/json"
	"os"
	"testing"
)

// lengthVector is one entry of testdata/lengths.json, shared with the frontend tests.
type lengthVector struct {
	Name  string `json:"name"`
	Text  string `json:"text"`
	UTF16 int    `json:"utf16"`
}

func TestNormalizeText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "crlf becomes lf", in: "a\r\nb\r\n", want: "a\nb\n"},
		{name: "lf unchanged", in: "a\nb", want: "a\nb"},
		{name: "empty", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeText(tt.in); got != tt.want {
				t.Errorf("NormalizeText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestUTF16Len(t *testing.T) {
	data, err := os.ReadFile("../../testdata/lengths.json")
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	var vectors []lengthVector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	if len(vectors) == 0 {
		t.Fatal("no vectors in testdata/lengths.json")
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			if got := UTF16Len(NormalizeText(v.Text)); got != v.UTF16 {
				t.Errorf("UTF16Len(NormalizeText(%q)) = %d, want %d", v.Text, got, v.UTF16)
			}
		})
	}
}

func TestNormalizeSecret(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "nfd becomes nfc", in: "é", want: "é"},
		{name: "spaces are kept", in: " x ", want: " x "},
		{name: "nfc unchanged", in: "пароль", want: "пароль"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeSecret(tt.in); got != tt.want {
				t.Errorf("NormalizeSecret(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
