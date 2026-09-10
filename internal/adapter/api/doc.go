// Package api defines the seven adapter interfaces. Definitions only, no implementations.
//
// The app declares requirements; adapters translate (R-250). Core never learns a provider's
// vocabulary (R-251). If a Docker-shaped concept appears in a signature here, the design has
// failed. Everything is compiled in-tree — these are ordinary Go interfaces, freely
// refactorable, and there is no wire protocol. See design 03.
package api
