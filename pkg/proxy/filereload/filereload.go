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

// Package filereload provides files and TLS key pairs that pick up changes
// on disk, for example when kubelet updates a mounted Secret.
package filereload

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"os"
	"sync"

	"k8s.io/klog/v2"
)

// File reads a file and reports whether its content has changed since it
// was last read. The file is read on every call, which is cheap for the
// small files (tokens, certificates) it is meant for and, unlike comparing
// modification times, also notices quick successive in-place writes.
type File struct {
	path string

	lock sync.Mutex
	data []byte
}

// New returns a File for path. The file is not read until Load is called.
func New(path string) *File {
	return &File{path: path}
}

// Load returns the file's current content. changed reports whether it
// differs from the content returned by the previous successful call (or
// whether this is the first one).
func (f *File) Load() (data []byte, changed bool, err error) {
	f.lock.Lock()
	defer f.lock.Unlock()

	data, err = os.ReadFile(f.path)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read %q: %w", f.path, err)
	}

	changed = f.data == nil || !bytes.Equal(data, f.data)
	f.data = data

	return data, changed, nil
}

// KeyPair is a TLS certificate and private key that are reloaded whenever
// either file changes.
type KeyPair struct {
	cert *File
	key  *File

	lock sync.Mutex
	pair *tls.Certificate
}

// NewKeyPair returns a KeyPair for the given PEM encoded certificate and key
// files. The files are not read until GetCertificate is called.
func NewKeyPair(certPath, keyPath string) *KeyPair {
	return &KeyPair{
		cert: New(certPath),
		key:  New(keyPath),
	}
}

// GetCertificate returns the current key pair and can be used as
// tls.Config.GetCertificate. If the files have changed but don't form a
// valid key pair (for example because only one of them has been updated
// yet), the previous key pair is returned.
func (k *KeyPair) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	k.lock.Lock()
	defer k.lock.Unlock()

	certPEM, certChanged, certErr := k.cert.Load()
	keyPEM, keyChanged, keyErr := k.key.Load()

	if err := firstError(certErr, keyErr); err != nil {
		return k.previous(err)
	}

	if k.pair != nil && !certChanged && !keyChanged {
		return k.pair, nil
	}

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return k.previous(fmt.Errorf("failed to load key pair: %w", err))
	}

	k.pair = &pair
	return k.pair, nil
}

// previous returns the last valid key pair, or err if there is none. k.lock
// must be held by the caller.
func (k *KeyPair) previous(err error) (*tls.Certificate, error) {
	if k.pair == nil {
		return nil, err
	}

	klog.Background().Error(err, "Failed to reload TLS key pair, keeping the previous one")
	return k.pair, nil
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
