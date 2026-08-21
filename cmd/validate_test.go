package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckLenCountsRunesNotBytes(t *testing.T) {
	ok := strings.Repeat("字", 30)
	if p := checkLen("zh-TW", "title", ok, maxTitle); p != nil {
		t.Fatalf("30 CJK chars must pass, got %v", p)
	}
	if p := checkLen("zh-TW", "title", ok+"字", maxTitle); len(p) != 1 || !strings.Contains(p[0], "31 chars (max 30)") {
		t.Fatalf("31 chars must fail with count, got %v", p)
	}
}

func TestCheckHeader(t *testing.T) {
	if err := checkHeader(noCollectionCSV); err != nil {
		t.Fatalf("canonical CSV rejected: %v", err)
	}
	if err := checkHeader("\ufeff" + noCollectionCSV); err != nil {
		t.Fatalf("BOM from Console export must be tolerated: %v", err)
	}
	err := checkHeader("Question,Response\nx,y\n")
	if err == nil || !strings.Contains(err.Error(), "Invalid header row") {
		t.Fatalf("bad header must be caught locally: %v", err)
	}
}

func TestLoadListingsAndNotes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "zh-TW.json"), []byte(`{"title":"a","shortDescription":"b","fullDescription":"c"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("ignored"), 0o644)
	ls, err := loadListings(dir, nil)
	if err != nil || len(ls) != 1 || ls["zh-TW"].Title != "a" {
		t.Fatalf("got %v %v", ls, err)
	}
	if ls, _ := loadListings(dir, []string{"en-US"}); len(ls) != 0 {
		t.Fatal("--locale filter ignored")
	}

	os.WriteFile(filepath.Join(dir, "en-US.txt"), []byte("  notes \n"), 0o644)
	notes, err := loadReleaseNotes(dir)
	if err != nil || len(notes) != 1 || notes[0].Language != "en-US" || notes[0].Text != "notes" {
		t.Fatalf("got %+v %v", notes, err)
	}
	os.WriteFile(filepath.Join(dir, "ja-JP.txt"), []byte(strings.Repeat("あ", 501)), 0o644)
	if _, err := loadReleaseNotes(dir); err == nil || !strings.Contains(err.Error(), "max 500") {
		t.Fatalf("501-char notes must fail: %v", err)
	}
	if n, _ := loadReleaseNotes(""); n != nil {
		t.Fatal("empty dir means no notes")
	}
}

func TestParseCodes(t *testing.T) {
	got, err := parseCodes([]string{"10", " 11"})
	if err != nil || len(got) != 2 || got[1] != 11 {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := parseCodes(nil); err == nil {
		t.Fatal("empty must error")
	}
	if _, err := parseCodes([]string{"x"}); err == nil {
		t.Fatal("non-numeric must error")
	}
}
