//go:build cgo

package ffmpeg

import (
	"errors"
	"fmt"
	"net/url"
	"sync"

	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/asticode/go-astiav"
)

// NativeTranscoder provides hardware-free H264/H265 to JPEG transcoding
// using FFmpeg libraries directly via CGo instead of spawning ffmpeg binary.
type NativeTranscoder struct {
	mu sync.Mutex

	// Decoder
	decCodec        *astiav.Codec
	decCodecContext *astiav.CodecContext
	decFrame        *astiav.Frame
	decPacket       *astiav.Packet

	// Encoder (MJPEG)
	encCodec        *astiav.Codec
	encCodecContext *astiav.CodecContext
	encPacket       *astiav.Packet

	// Scaler (optional, for resizing)
	swsContext *astiav.SoftwareScaleContext
	scaledFrame *astiav.Frame

	// Configuration
	codecName   string
	width       int
	height      int
	targetWidth int
	targetHeight int
	initialized bool
}

var (
	nativeTranscoderPool = sync.Pool{
		New: func() interface{} {
			return &NativeTranscoder{}
		},
	}
)

// NativeTranscodingAvailable returns true when CGo is enabled and native transcoding is available.
func NativeTranscodingAvailable() bool {
	return true
}

// JPEGWithQueryNative transcodes H264/H265 frame data to JPEG using native FFmpeg libraries.
// This avoids spawning an external ffmpeg process.
func JPEGWithQueryNative(b []byte, codecName string, query url.Values) ([]byte, error) {
	var width, height int

	for k, v := range query {
		switch k {
		case "width", "w":
			width = core.Atoi(v[0])
		case "height", "h":
			height = core.Atoi(v[0])
		}
	}

	return TranscodeToJPEGNative(b, codecName, width, height)
}

// TranscodeToJPEGNative transcodes raw H264/H265 frame data to JPEG.
func TranscodeToJPEGNative(data []byte, codecName string, targetWidth, targetHeight int) ([]byte, error) {
	t := nativeTranscoderPool.Get().(*NativeTranscoder)
	defer func() {
		t.Reset()
		nativeTranscoderPool.Put(t)
	}()

	// Initialize decoder based on codec
	if err := t.initDecoder(codecName); err != nil {
		return nil, fmt.Errorf("failed to init decoder: %w", err)
	}

	// Decode frame
	if err := t.decode(data); err != nil {
		return nil, fmt.Errorf("failed to decode: %w", err)
	}

	// Get source dimensions
	srcWidth := t.decFrame.Width()
	srcHeight := t.decFrame.Height()

	// Calculate target dimensions if needed
	if targetWidth <= 0 && targetHeight <= 0 {
		targetWidth = srcWidth
		targetHeight = srcHeight
	} else if targetWidth <= 0 {
		targetWidth = srcWidth * targetHeight / srcHeight
	} else if targetHeight <= 0 {
		targetHeight = srcHeight * targetWidth / srcWidth
	}

	// Initialize encoder
	if err := t.initEncoder(targetWidth, targetHeight); err != nil {
		return nil, fmt.Errorf("failed to init encoder: %w", err)
	}

	// Scale if needed, then encode
	frameToEncode := t.decFrame
	if srcWidth != targetWidth || srcHeight != targetHeight {
		if err := t.initScaler(srcWidth, srcHeight, targetWidth, targetHeight); err != nil {
			return nil, fmt.Errorf("failed to init scaler: %w", err)
		}
		if err := t.scale(); err != nil {
			return nil, fmt.Errorf("failed to scale: %w", err)
		}
		frameToEncode = t.scaledFrame
	}

	// Encode to JPEG
	jpegData, err := t.encode(frameToEncode)
	if err != nil {
		return nil, fmt.Errorf("failed to encode: %w", err)
	}

	return jpegData, nil
}

func (t *NativeTranscoder) initDecoder(codecName string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	var codecID astiav.CodecID
	switch codecName {
	case core.CodecH264:
		codecID = astiav.CodecIDH264
	case core.CodecH265:
		codecID = astiav.CodecIDHevc
	default:
		return fmt.Errorf("unsupported codec: %s", codecName)
	}

	t.decCodec = astiav.FindDecoder(codecID)
	if t.decCodec == nil {
		return errors.New("decoder not found")
	}

	t.decCodecContext = astiav.AllocCodecContext(t.decCodec)
	if t.decCodecContext == nil {
		return errors.New("failed to allocate decoder context")
	}

	if err := t.decCodecContext.Open(t.decCodec, nil); err != nil {
		return fmt.Errorf("failed to open decoder: %w", err)
	}

	t.decFrame = astiav.AllocFrame()
	t.decPacket = astiav.AllocPacket()
	t.codecName = codecName

	return nil
}

func (t *NativeTranscoder) decode(data []byte) error {
	// Set packet data
	if err := t.decPacket.FromData(data); err != nil {
		return fmt.Errorf("failed to set packet data: %w", err)
	}
	defer t.decPacket.Unref()

	// Send packet to decoder
	if err := t.decCodecContext.SendPacket(t.decPacket); err != nil {
		return fmt.Errorf("send packet failed: %w", err)
	}

	// Receive decoded frame
	if err := t.decCodecContext.ReceiveFrame(t.decFrame); err != nil {
		return fmt.Errorf("receive frame failed: %w", err)
	}

	return nil
}

func (t *NativeTranscoder) initEncoder(width, height int) error {
	t.encCodec = astiav.FindEncoder(astiav.CodecIDMjpeg)
	if t.encCodec == nil {
		return errors.New("MJPEG encoder not found")
	}

	t.encCodecContext = astiav.AllocCodecContext(t.encCodec)
	if t.encCodecContext == nil {
		return errors.New("failed to allocate encoder context")
	}

	// Configure encoder
	t.encCodecContext.SetWidth(width)
	t.encCodecContext.SetHeight(height)
	t.encCodecContext.SetTimeBase(astiav.NewRational(1, 25))
	t.encCodecContext.SetPixelFormat(astiav.PixelFormatYuvj420P) // JPEG uses YUVJ420P

	if err := t.encCodecContext.Open(t.encCodec, nil); err != nil {
		return fmt.Errorf("failed to open encoder: %w", err)
	}

	t.encPacket = astiav.AllocPacket()
	t.targetWidth = width
	t.targetHeight = height

	return nil
}

func (t *NativeTranscoder) initScaler(srcW, srcH, dstW, dstH int) error {
	t.scaledFrame = astiav.AllocFrame()
	t.scaledFrame.SetWidth(dstW)
	t.scaledFrame.SetHeight(dstH)
	t.scaledFrame.SetPixelFormat(astiav.PixelFormatYuvj420P)

	if err := t.scaledFrame.AllocBuffer(1); err != nil {
		return fmt.Errorf("failed to allocate scaled frame buffer: %w", err)
	}

	var err error
	t.swsContext, err = astiav.CreateSoftwareScaleContext(
		srcW, srcH, t.decFrame.PixelFormat(),
		dstW, dstH, astiav.PixelFormatYuvj420P,
		astiav.NewSoftwareScaleContextFlags(astiav.SoftwareScaleContextFlagBilinear),
	)
	if err != nil {
		return fmt.Errorf("failed to create scaler: %w", err)
	}

	return nil
}

func (t *NativeTranscoder) scale() error {
	if err := t.swsContext.ScaleFrame(t.decFrame, t.scaledFrame); err != nil {
		return fmt.Errorf("scale failed: %w", err)
	}
	return nil
}

func (t *NativeTranscoder) encode(frame *astiav.Frame) ([]byte, error) {
	// Send frame to encoder
	if err := t.encCodecContext.SendFrame(frame); err != nil {
		return nil, fmt.Errorf("send frame failed: %w", err)
	}

	// Receive encoded packet
	if err := t.encCodecContext.ReceivePacket(t.encPacket); err != nil {
		return nil, fmt.Errorf("receive packet failed: %w", err)
	}
	defer t.encPacket.Unref()

	// Copy data out
	data := make([]byte, t.encPacket.Size())
	copy(data, t.encPacket.Data())

	return data, nil
}

// Reset cleans up resources for reuse
func (t *NativeTranscoder) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.decFrame != nil {
		t.decFrame.Unref()
	}
	if t.decPacket != nil {
		t.decPacket.Unref()
	}
	if t.encPacket != nil {
		t.encPacket.Unref()
	}
	if t.scaledFrame != nil {
		t.scaledFrame.Free()
		t.scaledFrame = nil
	}
	if t.swsContext != nil {
		t.swsContext.Free()
		t.swsContext = nil
	}
	if t.decCodecContext != nil {
		t.decCodecContext.Free()
		t.decCodecContext = nil
	}
	if t.encCodecContext != nil {
		t.encCodecContext.Free()
		t.encCodecContext = nil
	}
	if t.decFrame != nil {
		t.decFrame.Free()
		t.decFrame = nil
	}
	if t.decPacket != nil {
		t.decPacket.Free()
		t.decPacket = nil
	}
	if t.encPacket != nil {
		t.encPacket.Free()
		t.encPacket = nil
	}

	t.initialized = false
}

// Free releases all resources
func (t *NativeTranscoder) Free() {
	t.Reset()
}

