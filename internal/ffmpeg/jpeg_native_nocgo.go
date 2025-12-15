//go:build !cgo

package ffmpeg

import (
	"errors"
	"net/url"
)

// JPEGWithQueryNative is not available without CGo - falls back to spawning ffmpeg binary.
func JPEGWithQueryNative(b []byte, codecName string, query url.Values) ([]byte, error) {
	return nil, errors.New("native transcoding requires CGo build")
}

// TranscodeToJPEGNative is not available without CGo.
func TranscodeToJPEGNative(data []byte, codecName string, targetWidth, targetHeight int) ([]byte, error) {
	return nil, errors.New("native transcoding requires CGo build")
}

// NativeTranscodingAvailable returns false when CGo is not enabled.
func NativeTranscodingAvailable() bool {
	return false
}

