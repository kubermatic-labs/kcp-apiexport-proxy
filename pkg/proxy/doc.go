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

// Package proxy provides an unauthenticated reverse proxy that sits in front
// of the shard-specific virtual workspace endpoints of one or more kcp
// APIExports. For each named APIExportEndpointSlice it watches the set of
// per-shard virtual workspace URLs, watches APIBindings through each of
// those URLs to learn which logical cluster lives behind which URL, and
// forwards requests of the form
// /apiexportendpointslices/<slice>/clusters/<logical_cluster>/... to the
// right shard using the identity of a single configured kubeconfig.
package proxy
