// Package httpapi is the REST surface: chi handlers and nothing else.
//
// The API is the product (R-261). Handlers contain no business logic — it lives in a service
// layer under internal/core that both httpapi and mcp call. A capability that exists in one
// surface and not the other means someone put logic in a handler.
package httpapi
