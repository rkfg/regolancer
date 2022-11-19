//go:build plan9 || appengine || wasm
// +build plan9 appengine wasm

package helpmessage

func getTerminalColumns() int {
	return 80
}
