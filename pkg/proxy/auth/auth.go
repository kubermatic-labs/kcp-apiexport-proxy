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

// Package auth authenticates requests to the proxy.
package auth

import (
	"bytes"
	"crypto/subtle"
	"net/http"
	"strings"

	"k8s.io/klog/v2"

	"github.com/kubermatic-labs/kcp-apiexport-proxy/pkg/proxy/filereload"
)

// WithToken only lets requests through to delegate that carry the token from
// tokenFile as "Authorization: Bearer <token>". The token file is re-read
// whenever it changes, and leading and trailing whitespace is ignored. An
// empty token never matches.
func WithToken(delegate http.Handler, tokenFile *filereload.File) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, _, err := tokenFile.Load()
		if err != nil {
			klog.FromContext(req.Context()).Error(err, "Failed to load token")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		token := bytes.TrimSpace(data)
		got, found := strings.CutPrefix(req.Header.Get("Authorization"), "Bearer ")

		if !found || len(token) == 0 || subtle.ConstantTimeCompare([]byte(got), token) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		delegate.ServeHTTP(w, req)
	})
}
