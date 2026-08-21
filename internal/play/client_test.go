package play

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/api/googleapi"
)

func TestResolveServiceAccountOrder(t *testing.T) {
	dir := t.TempDir()
	flag := filepath.Join(dir, "flag.json")
	env := filepath.Join(dir, "env.json")
	for _, p := range []string{flag, env} {
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GPC_SERVICE_ACCOUNT", env)

	got, err := ResolveServiceAccount(flag)
	if err != nil || got != flag {
		t.Fatalf("flag should win: got %q err %v", got, err)
	}
	got, err = ResolveServiceAccount("")
	if err != nil || got != env {
		t.Fatalf("env should be second: got %q err %v", got, err)
	}
	if _, err := ResolveServiceAccount(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("explicit missing flag path must error, not silently fall through")
	}
	t.Setenv("GPC_SERVICE_ACCOUNT", "")
	t.Setenv("HOME", dir)
	if _, err := ResolveServiceAccount(""); err == nil || !strings.Contains(err.Error(), "no service account found") {
		t.Fatalf("expected guidance error, got %v", err)
	}
}

func TestWrapKeepsGoogleMessageAndAddsHint(t *testing.T) {
	g := &googleapi.Error{Code: 400, Message: "Only releases with status draft may be created on draft app."}
	err := Wrap("set track production", g)
	s := err.Error()
	for _, want := range []string{"HTTP 400", "Only releases with status draft", "hint:", "--status draft"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %q", want, s)
		}
	}
	if Wrap("x", nil) != nil {
		t.Error("nil must stay nil")
	}
	plain := Wrap("op", errors.New("A change was made to the application outside of this Edit"))
	if !strings.Contains(plain.Error(), "Console tab") {
		t.Errorf("conflict hint missing: %v", plain)
	}
	vc := Wrap("upload bundle", &googleapi.Error{Code: 403, Message: "Version code 10 has already been used."})
	if !strings.Contains(vc.Error(), "versionCode") {
		t.Errorf("version code hint missing (case-insensitive match): %v", vc)
	}
	if strings.Contains(Wrap("op", errors.New("unrelated")).Error(), "hint:") {
		t.Error("no hint expected for unknown errors")
	}
}
