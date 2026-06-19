//go:build !cgo

package main

func callHost(method string, payload []byte) {}

func callHostWithResponse(method string, payload []byte) ([]byte, error) {
	return nil, errHostCallFailed
}

func callHostLog(level, message string) {}
