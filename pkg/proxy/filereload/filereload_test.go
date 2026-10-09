/*
Copyright The kcp-apiexport-proxy Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package filereload

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/internal/testcerts"
)

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func load(t *testing.T, f *File) (string, bool) {
	t.Helper()

	data, changed, err := f.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return string(data), changed
}

func TestFileLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	writeFile(t, path, []byte("first"))

	f := New(path)

	if data, changed := load(t, f); data != "first" || !changed {
		t.Fatalf("first load: got %q (changed %v), want %q (changed true)", data, changed, "first")
	}
	if data, changed := load(t, f); data != "first" || changed {
		t.Fatalf("second load: got %q (changed %v), want %q (changed false)", data, changed, "first")
	}

	writeFile(t, path, []byte("second value"))
	if data, changed := load(t, f); data != "second value" || !changed {
		t.Fatalf("load after update: got %q (changed %v), want %q (changed true)", data, changed, "second value")
	}
}

func TestFileLoadSymlinkSwap(t *testing.T) {
	dir := t.TempDir()

	for name, data := range map[string]string{"a": "old", "b": "new"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}
		writeFile(t, filepath.Join(dir, name, "token"), []byte(data))
	}

	if err := os.Symlink("a", filepath.Join(dir, "..data")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	if err := os.Symlink(filepath.Join("..data", "token"), filepath.Join(dir, "token")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	f := New(filepath.Join(dir, "token"))
	if data, _ := load(t, f); data != "old" {
		t.Fatalf("got %q, want %q", data, "old")
	}

	if err := os.Symlink("b", filepath.Join(dir, "..data_tmp")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	if err := os.Rename(filepath.Join(dir, "..data_tmp"), filepath.Join(dir, "..data")); err != nil {
		t.Fatalf("failed to swap symlink: %v", err)
	}

	if data, changed := load(t, f); data != "new" || !changed {
		t.Fatalf("load after swap: got %q (changed %v), want %q (changed true)", data, changed, "new")
	}
}

func TestFileLoadMissing(t *testing.T) {
	if _, _, err := New(filepath.Join(t.TempDir(), "missing")).Load(); err == nil {
		t.Fatalf("expected an error for a missing file")
	}
}

func TestKeyPair(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")

	firstCert, firstKey := testcerts.Generate(t)
	writeFile(t, certPath, firstCert)
	writeFile(t, keyPath, firstKey)

	k := NewKeyPair(certPath, keyPath)

	first, err := k.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if again, err := k.GetCertificate(nil); err != nil || again != first {
		t.Fatalf("expected the cached key pair, got %v (err %v)", again, err)
	}

	secondCert, secondKey := testcerts.Generate(t)
	writeFile(t, certPath, secondCert)

	if got, err := k.GetCertificate(nil); err != nil || got != first {
		t.Fatalf("expected the previous key pair while only the certificate is updated, got %v (err %v)", got, err)
	}

	writeFile(t, keyPath, secondKey)

	second, err := k.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate after rotation: %v", err)
	}
	if bytes.Equal(second.Certificate[0], first.Certificate[0]) {
		t.Fatalf("expected the rotated certificate")
	}

	if err := os.Remove(certPath); err != nil {
		t.Fatalf("failed to remove certificate: %v", err)
	}
	if got, err := k.GetCertificate(nil); err != nil || got != second {
		t.Fatalf("expected the previous key pair when the certificate is missing, got %v (err %v)", got, err)
	}
}

func TestKeyPairInvalid(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")

	if _, err := NewKeyPair(certPath, keyPath).GetCertificate(nil); err == nil {
		t.Fatalf("expected an error for missing files")
	}

	writeFile(t, certPath, []byte("not a certificate"))
	writeFile(t, keyPath, []byte("not a key"))

	if _, err := NewKeyPair(certPath, keyPath).GetCertificate(nil); err == nil {
		t.Fatalf("expected an error for an invalid key pair")
	}
}
