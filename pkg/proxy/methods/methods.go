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

// Package methods restricts the HTTP methods the proxy accepts.
package methods

import (
	"net/http"
	"slices"
	"strings"
)

// WithAllowed only lets requests through to delegate whose method is one of
// allowed. Methods are case-sensitive, so allowed must be in their canonical
// (usually uppercase) form. Other requests get a 405 with an Allow header.
func WithAllowed(delegate http.Handler, allowed []string) http.Handler {
	allowHeader := strings.Join(allowed, ", ")

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !slices.Contains(allowed, req.Method) {
			w.Header().Set("Allow", allowHeader)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		delegate.ServeHTTP(w, req)
	})
}
