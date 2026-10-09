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

// Package accesslog logs every request the proxy handles.
package accesslog

import (
	"net/http"
	"time"

	"k8s.io/klog/v2"
)

// WithLogging logs one line per request after delegate has handled it, with
// the method, path, response status, duration, client address and user
// agent. Request headers, and thus credentials, are never logged.
func WithLogging(delegate http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w}

		defer func() {
			klog.FromContext(req.Context()).Info("Request",
				"method", req.Method,
				"path", req.URL.Path,
				"status", rw.statusCode(),
				"duration", time.Since(start),
				"remoteAddr", req.RemoteAddr,
				"userAgent", req.UserAgent(),
			)
		}()

		delegate.ServeHTTP(rw, req)
	})
}

// statusRecorder remembers the status code written to the wrapped
// ResponseWriter.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// Flush keeps streaming responses such as watches working.
func (r *statusRecorder) Flush() {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying ResponseWriter.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// statusCode returns the status code sent to the client. A handler that
// writes nothing at all results in a 200.
func (r *statusRecorder) statusCode() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}
