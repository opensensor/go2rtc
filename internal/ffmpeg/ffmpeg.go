package ffmpeg

import (
	"errors"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/AlexxIT/go2rtc/internal/api"
	"github.com/AlexxIT/go2rtc/internal/app"
	"github.com/AlexxIT/go2rtc/internal/exec"
	"github.com/AlexxIT/go2rtc/internal/ffmpeg/device"
	"github.com/AlexxIT/go2rtc/internal/ffmpeg/hardware"
	"github.com/AlexxIT/go2rtc/internal/ffmpeg/virtual"
	"github.com/AlexxIT/go2rtc/internal/rtsp"
	"github.com/AlexxIT/go2rtc/internal/streams"
	"github.com/AlexxIT/go2rtc/pkg/core"
	"github.com/AlexxIT/go2rtc/pkg/ffmpeg"
	"github.com/rs/zerolog"
)

func init() {
	streams.HandleValidator("ffmpeg", func(source string) error { return validateSource(source[7:]) })
}

func Init() {
	var cfg struct {
		Mod map[string]string `yaml:"ffmpeg"`
		Log struct {
			Level string `yaml:"ffmpeg"`
		} `yaml:"log"`
	}

	cfg.Mod = defaults // will be overriden from yaml
	cfg.Log.Level = "error"

	app.LoadConfig(&cfg)

	log = app.GetLogger("ffmpeg")

	// zerolog levels: trace debug         info warn    error fatal panic disabled
	// FFmpeg  levels: trace debug verbose info warning error fatal panic quiet
	if cfg.Log.Level == "warn" {
		cfg.Log.Level = "warning"
	}
	defaults["global"] += " -v " + cfg.Log.Level

	streams.HandleFunc("ffmpeg", streamProducer)

	api.HandleFunc("api/ffmpeg", apiFFmpeg)

	device.Init(defaults["bin"])
	hardware.Init(defaults["bin"])
}

var defaults = map[string]string{
	"bin":     "ffmpeg",
	"global":  "-hide_banner",
	"timeout": "5",

	// inputs
	"file":     "-re -i {input}",
	"http":     "-fflags nobuffer -flags low_delay -i {input}",
	"rtsp":     "-fflags nobuffer -flags low_delay -timeout {timeout} -user_agent go2rtc/ffmpeg -rtsp_flags prefer_tcp -i {input}",
	"rtsp/udp": "-fflags nobuffer -flags low_delay -timeout {timeout} -user_agent go2rtc/ffmpeg -i {input}",

	// output
	"output":       "-user_agent ffmpeg/go2rtc -rtsp_transport tcp -f rtsp {output}",
	"output/mjpeg": "-f mjpeg -",
	"output/raw":   "-f yuv4mpegpipe -",
	"output/aac":   "-f adts -",
	"output/wav":   "-f wav -",

	// `-preset superfast` - we can't use ultrafast because it doesn't support `-profile main -level 4.1`
	// `-tune zerolatency` - for minimal latency
	// `-profile high -level 4.1` - most used streaming profile
	// `-pix_fmt:v yuv420p` - important for Telegram
	"h264":  "-c:v libx264 -g 50 -profile:v high -level:v 4.1 -preset:v superfast -tune:v zerolatency -pix_fmt:v yuv420p",
	"h265":  "-c:v libx265 -g 50 -profile:v main -x265-params level=5.1:high-tier=0 -preset:v superfast -tune:v zerolatency -pix_fmt:v yuv420p",
	"mjpeg": "-c:v mjpeg",
	//"mjpeg": "-c:v mjpeg -force_duplicated_matrix:v 1 -huffman:v 0 -pix_fmt:v yuvj420p",

	"raw":         "-c:v rawvideo",
	"raw/gray8":   "-c:v rawvideo -pix_fmt:v gray8",
	"raw/yuv420p": "-c:v rawvideo -pix_fmt:v yuv420p",
	"raw/yuv422p": "-c:v rawvideo -pix_fmt:v yuv422p",
	"raw/yuv444p": "-c:v rawvideo -pix_fmt:v yuv444p",

	// https://ffmpeg.org/ffmpeg-codecs.html#libopus-1
	// https://github.com/pion/webrtc/issues/1514
	// https://ffmpeg.org/ffmpeg-resampler.html
	// `-async 1` or `-min_comp 0` - force resampling for static timestamp inc, important for WebRTC audio quality
	"opus":       "-c:a libopus -application:a lowdelay -min_comp 0",
	"opus/16000": "-c:a libopus -application:a lowdelay -min_comp 0 -ar:a 16000 -ac:a 1",
	"pcmu":       "-c:a pcm_mulaw -ar:a 8000 -ac:a 1",
	"pcmu/8000":  "-c:a pcm_mulaw -ar:a 8000 -ac:a 1",
	"pcmu/16000": "-c:a pcm_mulaw -ar:a 16000 -ac:a 1",
	"pcmu/48000": "-c:a pcm_mulaw -ar:a 48000 -ac:a 1",
	"pcma":       "-c:a pcm_alaw -ar:a 8000 -ac:a 1",
	"pcma/8000":  "-c:a pcm_alaw -ar:a 8000 -ac:a 1",
	"pcma/16000": "-c:a pcm_alaw -ar:a 16000 -ac:a 1",
	"pcma/48000": "-c:a pcm_alaw -ar:a 48000 -ac:a 1",
	"aac":        "-c:a aac", // keep sample rate and channels
	"aac/16000":  "-c:a aac -ar:a 16000 -ac:a 1",
	"mp3":        "-c:a libmp3lame -q:a 8",
	"pcm":        "-c:a pcm_s16be -ar:a 8000 -ac:a 1",
	"pcm/8000":   "-c:a pcm_s16be -ar:a 8000 -ac:a 1",
	"pcm/16000":  "-c:a pcm_s16be -ar:a 16000 -ac:a 1",
	"pcm/48000":  "-c:a pcm_s16be -ar:a 48000 -ac:a 1",
	"pcml":       "-c:a pcm_s16le -ar:a 8000 -ac:a 1",
	"pcml/8000":  "-c:a pcm_s16le -ar:a 8000 -ac:a 1",
	"pcml/16000": "-c:a pcm_s16le -ar:a 16000 -ac:a 1",
	"pcml/44100": "-c:a pcm_s16le -ar:a 44100 -ac:a 1",

	// hardware Intel and AMD on Linux
	// better not to set `-async_depth:v 1` like for QSV, because framedrops
	// `-bf 0` - disable B-frames is very important
	"h264/vaapi":  "-c:v h264_vaapi -g 50 -bf 0 -profile:v high -level:v 4.1 -sei:v 0",
	"h265/vaapi":  "-c:v hevc_vaapi -g 50 -bf 0 -profile:v main -level:v 5.1 -sei:v 0",
	"mjpeg/vaapi": "-c:v mjpeg_vaapi",

	// hardware Raspberry
	"h264/v4l2m2m": "-c:v h264_v4l2m2m -g 50 -bf 0",
	"h265/v4l2m2m": "-c:v hevc_v4l2m2m -g 50 -bf 0",

	// hardware Rockchip
	// important to use custom ffmpeg https://github.com/AlexxIT/go2rtc/issues/768
	// hevc - doesn't have a profile setting
	"h264/rkmpp":  "-c:v h264_rkmpp -g 50 -bf 0 -profile:v high -level:v 4.1",
	"h265/rkmpp":  "-c:v hevc_rkmpp -g 50 -bf 0 -profile:v main -level:v 5.1",
	"mjpeg/rkmpp": "-c:v mjpeg_rkmpp",

	// hardware NVidia on Linux and Windows
	// preset=p2 - faster, tune=ll - low latency
	"h264/cuda": "-c:v h264_nvenc -g 50 -bf 0 -profile:v high -level:v auto -preset:v p2 -tune:v ll",
	"h265/cuda": "-c:v hevc_nvenc -g 50 -bf 0 -profile:v main -level:v auto",

	// hardware Intel on Windows
	"h264/dxva2":  "-c:v h264_qsv -g 50 -bf 0 -profile:v high -level:v 4.1 -async_depth:v 1",
	"h265/dxva2":  "-c:v hevc_qsv -g 50 -bf 0 -profile:v main -level:v 5.1 -async_depth:v 1",
	"mjpeg/dxva2": "-c:v mjpeg_qsv",

	// hardware macOS
	"h264/videotoolbox": "-c:v h264_videotoolbox -g 50 -bf 0 -profile:v high -level:v 4.1",
	"h265/videotoolbox": "-c:v hevc_videotoolbox -g 50 -bf 0 -profile:v main -level:v 5.1",
}

var log zerolog.Logger

// configTemplate - return template from config (defaults) if exist or return raw template
func configTemplate(template string) string {
	if s := defaults[template]; s != "" {
		return s
	}
	return template
}

// inputTemplate - select input template from YAML config by template name
// if query has input param - select another template by this name
// if there is no another template - use input param as template
func inputTemplate(name, s string, query url.Values) string {
	var template string
	if input := query.Get("input"); input != "" {
		template = configTemplate(input)
	} else {
		template = defaults[name]
	}
	if strings.Contains(template, "{timeout}") {
		timeout := query.Get("timeout")
		if timeout == "" {
			timeout = defaults["timeout"]
		}
		template = strings.Replace(template, "{timeout}", timeout+"000000", 1)
	}
	return strings.Replace(template, "{input}", s, 1)
}

func validFrameRate(s string) bool {
	if strings.Count(s, "/") == 1 {
		parts := strings.SplitN(s, "/", 2)
		num, errNum := strconv.ParseUint(parts[0], 10, 64)
		den, errDen := strconv.ParseUint(parts[1], 10, 64)
		return errNum == nil && errDen == nil && num > 0 && den > 0
	}

	rate, err := strconv.ParseFloat(s, 64)
	return err == nil && rate > 0 && !math.IsInf(rate, 0) && !math.IsNaN(rate)
}

func parseArgs(s string) *ffmpeg.Args {
	return parseArgsMode(s, false)
}

func parseArgsMode(s string, streamOnly bool) *ffmpeg.Args {
	// init FFmpeg arguments
	args := &ffmpeg.Args{
		Bin:     defaults["bin"],
		Global:  defaults["global"],
		Output:  defaults["output"],
		Version: verAV,
	}

	var source = s
	var query url.Values
	if i := strings.IndexByte(s, '#'); i >= 0 {
		query = streams.ParseQuery(s[i+1:])
		args.Video = len(query["video"])
		args.Audio = len(query["audio"])
		s = s[:i]
	}

	// Parse input:
	//   1. Input as xxxx:// link (http or rtsp or any other)
	//   2. Input as stream name
	//   3. Input as FFmpeg device (local USB camera)
	if i := strings.Index(s, "://"); i > 0 {
		switch s[:i] {
		case "http", "https", "rtmp":
			args.Input = inputTemplate("http", "{input}", query)
			args.InputValue = s
			// HTTP JPEG/MJPEG streams often don't carry timestamps. FFmpeg then
			// assumes 25 fps, which compresses the output timeline when frames
			// actually arrive at a lower rate. Input-side -r generates timestamps
			// at the configured rate before the video is transcoded.
			if frameRate := query.Get("framerate"); validFrameRate(frameRate) {
				args.Input = "-r " + frameRate + " " + args.Input
			}
		case "rtsp", "rtsps":
			// https://ffmpeg.org/ffmpeg-protocols.html#rtsp
			// skip unnecessary input tracks
			switch {
			case (args.Video > 0 && args.Audio > 0) || (args.Video == 0 && args.Audio == 0):
				args.Input = "-allowed_media_types video+audio "
			case args.Video > 0:
				args.Input = "-allowed_media_types video "
			case args.Audio > 0:
				args.Input = "-allowed_media_types audio "
			}

			args.Input += inputTemplate("rtsp", "{input}", query)
			args.InputValue = s
		default:
			args.Input = "-i {input}"
			args.InputValue = s
		}
	} else if (streamOnly && streamName.MatchString(s)) || streams.Get(s) != nil {
		s = "rtsp://127.0.0.1:" + rtsp.Port + "/" + s
		switch {
		case args.Video > 0 && args.Audio == 0:
			s += "?video"
		case args.Audio > 0 && args.Video == 0:
			s += "?audio"
		default:
			s += "?video&audio"
		}
		s += "&source=ffmpeg:" + url.QueryEscape(source)
		for _, v := range query["query"] {
			s += "&" + v
		}
		args.Input = inputTemplate("rtsp", "{input}", query)
		args.InputValue = s
	} else if i = strings.Index(s, "?"); i > 0 {
		switch s[:i] {
		case "device":
			args.Input = device.GetInput(s[i+1:])
		case "virtual":
			args.Input = virtual.GetInput(s[i+1:])
		case "tts":
			args.Input = virtual.GetInputTTS(s[i+1:])
		}
	} else {
		args.Input = inputTemplate("file", "{input}", query)
		args.InputValue = s
	}

	if query["async"] != nil {
		args.Input = "-use_wallclock_as_timestamps 1 -async 1 " + args.Input
	}

	// Parse query params:
	//   1. `width`/`height` params
	//   2. `rotate` param
	//   3. `video` params (support multiple)
	//   4. `audio` params (support multiple)
	//   5. `hardware` param
	if query != nil {
		// 1. Process raw params for FFmpeg
		for _, raw := range query["raw"] {
			// support templates https://github.com/AlexxIT/go2rtc/issues/487
			raw = configTemplate(raw)
			args.AddCodec(raw)
		}

		// 2. Process video filters (resize and rotation)
		if query["width"] != nil || query["height"] != nil {
			filter := "scale="
			if query["width"] != nil {
				filter += query["width"][0]
			} else {
				filter += "-1"
			}
			filter += ":"
			if query["height"] != nil {
				filter += query["height"][0]
			} else {
				filter += "-1"
			}
			args.AddFilter(filter)
		}

		if query["rotate"] != nil {
			var filter string
			switch query["rotate"][0] {
			case "90":
				filter = "transpose=1" // 90 degrees clockwise
			case "180":
				filter = "transpose=1,transpose=1"
			case "-90", "270":
				filter = "transpose=2" // 90 degrees counterclockwise
			}
			if filter != "" {
				args.AddFilter(filter)
			}
		}

		for _, drawtext := range query["drawtext"] {
			// support templates https://github.com/AlexxIT/go2rtc/issues/487
			drawtext = configTemplate(drawtext)

			// support default timestamp format
			if !strings.Contains(drawtext, "text=") {
				drawtext += `:text='%{localtime\:%Y-%m-%d %X}'`
			}

			args.AddFilter("drawtext=" + drawtext)
		}

		// 3. Process video codecs
		if args.Video > 0 {
			for _, video := range query["video"] {
				if video != "copy" {
					if codec := defaults[video]; codec != "" {
						args.AddCodec(codec)
					} else {
						args.AddCodec(video)
					}
				} else {
					args.AddCodec("-c:v copy")
				}
			}
		}

		if query["bitrate"] != nil {
			// https://trac.ffmpeg.org/wiki/Limiting%20the%20output%20bitrate
			b := query["bitrate"][0]
			args.AddCodec("-b:v " + b + " -maxrate " + b + " -bufsize " + b)
		}

		// 4. Process audio codecs
		if args.Audio > 0 {
			for _, audio := range query["audio"] {
				if audio != "copy" {
					if codec := defaults[audio]; codec != "" {
						args.AddCodec(codec)
					} else {
						args.AddCodec(audio)
					}
				} else {
					args.AddCodec("-c:a copy")
				}
			}
		}

		if query["hardware"] != nil {
			hardware.MakeHardware(args, query["hardware"][0], defaults)
		}
	}

	switch {
	case args.Video == 0 && args.Audio == 0:
		args.AddCodec("-c copy")
	case args.Video == 0:
		args.AddCodec("-vn")
	case args.Audio == 0:
		args.AddCodec("-an")
	}

	// change otput from RTSP to some other pipe format
	switch {
	case args.Video == 0 && args.Audio == 0:
		// no transcoding from mjpeg input (ffmpeg device with support output as raw MJPEG)
		if strings.Contains(args.Input, " mjpeg ") {
			args.Output = defaults["output/mjpeg"]
		}
	case args.Video == 1 && args.Audio == 0:
		switch core.Before(query.Get("video"), "/") {
		case "mjpeg":
			args.Output = defaults["output/mjpeg"]
		case "raw":
			args.Output = defaults["output/raw"]
		}
	case args.Video == 0 && args.Audio == 1:
		switch core.Before(query.Get("audio"), "/") {
		case "aac":
			args.Output = defaults["output/aac"]
		case "pcma", "pcmu", "pcml":
			args.Output = defaults["output/wav"]
		}
	}

	return args
}

func streamProducer(source string) (core.Producer, error) {
	if err := streams.Validate(source); err != nil {
		return nil, err
	}
	if _, err := Version(); err != nil {
		return nil, err
	}
	// Treat bare names as RTSP references even if registration changes while
	// building the command. They must never fall back to local file input.
	args := parseArgsMode(source[7:], true)
	if core.Contains(args.Codecs, "auto") {
		return NewProducer(source)
	}
	return exec.NewProducer(source, args.Argv())
}

var streamName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

var positiveNumber = regexp.MustCompile(`^[0-9]+$`)
var bitrateValue = regexp.MustCompile(`^[0-9]+[kKmM]?$`)

// API and config sources may select known presets, not supply command fragments
// or arbitrary FFmpeg protocols (which can read/write local files).
func validateSource(source string) error {
	input, params, _ := strings.Cut(source, "#")
	if strings.HasPrefix(input, "virtual?") {
		if err := validateVirtual(strings.TrimPrefix(input, "virtual?")); err != nil {
			return err
		}
	} else if !streamName.MatchString(input) {
		u, err := url.Parse(input)
		if err != nil || u.Host == "" {
			return errors.New("ffmpeg: expected network URL or stream name")
		}
		switch u.Scheme {
		case "http", "https", "rtsp", "rtsps", "rtmp":
		default:
			return errors.New("ffmpeg: unsupported input protocol")
		}
	}
	for key, values := range streams.ParseQuery(params) {
		for _, value := range values {
			valid := false
			switch key {
			case "video", "audio":
				prefix := "-c:v "
				if key == "audio" {
					prefix = "-c:a "
				}
				valid = value == "copy" || (key == "audio" && value == "auto") ||
					strings.HasPrefix(defaults[value], prefix) ||
					strings.HasPrefix(defaults[value], strings.Replace(prefix, "-c:", "-codec:", 1))
			case "input":
				valid = value == "http" || value == "rtsp" || value == "rtsp/udp"
			case "width", "height", "timeout":
				valid = positiveNumber.MatchString(value)
			case "bitrate":
				valid = bitrateValue.MatchString(value)
			case "framerate":
				valid = validFrameRate(value)
			case "rotate":
				valid = value == "90" || value == "180" || value == "270" || value == "-90"
			case "hardware":
				valid = value == "" || value == "software" || value == "vaapi" || value == "cuda" || value == "dxva2" || value == "videotoolbox" || value == "rkmpp" || value == "v4l2m2m"
			case "async":
				valid = value == "" || value == "1"
			}
			if !valid {
				return errors.New("ffmpeg: unsupported source parameter: " + key)
			}
		}
	}
	return nil
}

// Synthetic test patterns remain available, but arbitrary lavfi graphs and
// percent-encoded command fragments are not accepted as virtual inputs.
func validateVirtual(rawQuery string) error {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return err
	}
	if len(query["video"]) != 1 {
		return errors.New("ffmpeg: expected one virtual video pattern")
	}
	for key, values := range query {
		if len(values) != 1 {
			return errors.New("ffmpeg: duplicate virtual parameter")
		}
		value := values[0]
		valid := false
		switch key {
		case "video":
			valid = value == "" || value == "testsrc" || value == "testsrc2" || value == "smptebars" || value == "color" || value == "mandelbrot"
		case "size":
			valid = virtualSize.MatchString(value) || value == "720" || value == "1080" || value == "2K" || value == "4K" || value == "8K"
		case "color":
			valid = virtualColor.MatchString(value)
		case "rate", "duration":
			valid = validFrameRate(value)
		case "sar":
			valid = virtualRatio.MatchString(value)
		case "decimals":
			valid = positiveNumber.MatchString(value)
		case "format":
			valid = value == "yuv420p" || value == "yuv422p" || value == "yuv444p" || value == "rgb24" || value == "gray"
		}
		if !valid {
			return errors.New("ffmpeg: unsupported virtual parameter: " + key)
		}
	}
	return nil
}

var virtualSize = regexp.MustCompile(`^[0-9]+x[0-9]+$`)
var virtualColor = regexp.MustCompile(`^[A-Za-z]+$|^0x[0-9A-Fa-f]{6}$`)
var virtualRatio = regexp.MustCompile(`^[0-9]+/[0-9]+$`)
