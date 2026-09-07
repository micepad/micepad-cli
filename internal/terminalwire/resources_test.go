package terminalwire

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/vmihailenco/msgpack/v5"
)

func TestFileContentPreservesMessagePackTextAndBinary(t *testing.T) {
	for _, input := range []interface{}{[]byte{0x50, 0x4b, 0, 0xff, 0x80}, "plain text", ""} {
		encoded, err := msgpack.Marshal(map[string]interface{}{"content": input})
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]interface{}
		if err := msgpack.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		got, err := fileContent(decoded)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := fileContent(map[string]interface{}{"content": input})
		if !bytes.Equal(got, want) {
			t.Fatalf("bytes changed: got %x, want %x", got, want)
		}
	}
	for _, input := range []interface{}{nil, 42, map[string]interface{}{"bad": true}} {
		if _, err := fileContent(map[string]interface{}{"content": input}); err == nil {
			t.Fatalf("accepted unsupported content %T", input)
		}
	}
}

func TestPrepareFileArgsCopiesRelativeClientFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	want := []byte{0x89, 'P', 'N', 'G', 0, 0xff}
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })
	client := &Client{storagePath: filepath.Join(dir, "storage")}
	got := client.prepareFileArgs([]string{"app", "photos", "create", "Photo", "photo.png", "--json"})
	if got[4] != "photo.png" || got[5] != "--json" {
		t.Fatalf("unexpected arguments: %v", got)
	}
	stored, err := os.ReadFile(filepath.Join(client.storagePath, "photo.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, want) {
		t.Fatalf("upload bytes changed: %x", stored)
	}
}
