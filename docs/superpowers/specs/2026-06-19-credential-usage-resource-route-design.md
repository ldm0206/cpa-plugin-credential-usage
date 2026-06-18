# Credential Usage Resource Route Design

## Goal

Expose credential usage data through CPA's existing unauthenticated plugin resource route mechanism, without requiring Management API key authentication and without modifying CPA main program source code.

## Constraints

- Base behavior on the checked CPA source code/protocol.
- Implement only in this plugin repository; do not modify CPA main program source code.
- CPA currently exposes plugin HTTP resources under `/v0/resource/plugins/<pluginID>/...`.
- CPA does not currently support arbitrary plugin routes such as `/v0/credential-usage` from plugin code alone.

## CPA Source Findings

CPA `internal/pluginhost/management.go` defines:

- Management API base path: `/v0/management`
- Plugin resource base path: `/v0/resource/plugins`

CPA `ServeResourceHTTP` dispatches GET resource requests without Management API authentication. Resource routes are registered through the `ManagementAPI` capability, using the `resources` field in the `management.register` response.

## Public Endpoints

The plugin will expose:

- `GET /v0/resource/plugins/credential-usage/list`
- `GET /v0/resource/plugins/credential-usage/detail?auth_index=<auth_index>`

The plugin will not document or rely on:

- `GET /v0/management/credential-usage`
- `GET /v0/credential-usage`

## Plugin Registration

`plugin.register` keeps both capabilities:

- `usage_plugin: true` for receiving usage records.
- `management_api: true` because CPA resource routes are registered through this capability.

`management.register` returns `resources`, not `routes`, for browser/API-visible unauthenticated resources. Resource paths must be exact-match paths accepted by CPA (no `:` parameters, no bare `/` which becomes empty after TrimRight).

## Request Handling

The existing credential store and usage collection logic remains unchanged.

The management/resource handler will normalize incoming paths by stripping CPA's resource prefix for this plugin:

- `/v0/resource/plugins/credential-usage/list` maps to list all credentials.
- `/v0/resource/plugins/credential-usage/detail` maps to a single credential, using the `auth_index` query parameter from `managementRequest.Query`.

The handler returns JSON responses with `content-type: application/json`.

## Error Handling

- Unknown resource paths return HTTP 404 with a JSON error.
- Missing credential detail paths return HTTP 404 with a JSON error.
- Invalid internal requests return HTTP 400 with a JSON error.

## Documentation

README will describe the resource endpoint and explicitly state that it does not use the Management API path or Management API key.

## Verification

Run:

```bash
go test ./...
```

Tests should cover:

- `management.register` returns `resources`.
- List resource path returns a JSON array.
- Detail resource path returns the matching credential.
- Missing detail returns 404.
- Unknown resource path returns 404.
