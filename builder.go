package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// EncodeOptions – toate setările unei encodări
type EncodeOptions struct {
	Container string `json:"container"` // mp4 mkv mov ts webm hls gif mp3 aac m4a flac wav opus

	VCodec      string `json:"vcodec"`    // copy none libx264 libx265 h264_nvenc ...
	RateMode    string `json:"rate_mode"` // quality | bitrate
	Quality     int    `json:"quality"`   // CRF / CQ / QP
	VBitrate    string `json:"vbitrate"`  // ex: 4000k
	MaxRate     string `json:"maxrate"`
	Speed       int    `json:"speed"` // 1 (cel mai rapid) .. 7 (cea mai bună calitate)
	Resolution  string `json:"resolution"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	FPS         string `json:"fps"`
	Deinterlace bool   `json:"deinterlace"`
	Aspect      string `json:"aspect"`     // "" = din sursă | 16:9 | 4:3 (corectează surse marcate greșit)
	PixelMode   string `json:"pixel_mode"` // "" = ca sursa (rezoluție păstrată) | square = pixeli pătrați (720x576 16:9 -> 1024x576)
	DeintMode   string `json:"deint_mode"` // "" = calitate (bwdif) | fast = un singur câmp (foarte ușor pe CPU)
	PixFmt      string `json:"pix_fmt"`
	GOP         int    `json:"gop"`
	Tune        string `json:"tune"`
	HWDecode    bool   `json:"hw_decode"`
	QSVMode     string `json:"qsv_mode"` // auto | d3d11 | dxva2 | hybrid

	ACodec      string `json:"acodec"`
	ABitrate    string `json:"abitrate"`
	AChannels   string `json:"achannels"`
	ASampleRate string `json:"asamplerate"`
	Volume      string `json:"volume"`   // dB, ex: 3 sau -2
	Loudnorm    string `json:"loudnorm"` // "" | -23 | -16 | -14

	Start string `json:"start"`
	End   string `json:"end"`

	Subs      string `json:"subs"` // auto | copy | none | burn
	MapAll    bool   `json:"map_all"`
	FastStart bool   `json:"faststart"`
	HLSTime   int    `json:"hls_time"`

	VFilter   string `json:"vfilter"`
	AFilter   string `json:"afilter"`
	ExtraArgs string `json:"extra_args"`
	InArgs    string `json:"in_args"`

	// streaming
	OutputMode  string `json:"output_mode"` // file | stream
	StreamURL   string `json:"stream_url"`  // rtmp://… srt://… udp://…
	Loop        bool   `json:"loop"`        // -stream_loop -1
	Realtime    bool   `json:"realtime"`    // -re
	AutoRestart bool   `json:"auto_restart"`

	OutDir    string `json:"out_dir"`
	SameDir   bool   `json:"same_dir"`
	Suffix    string `json:"suffix"`
	Overwrite bool   `json:"overwrite"`
}

var audioOnly = map[string]string{
	"mp3": "libmp3lame", "aac": "aac", "m4a": "aac", "flac": "flac", "wav": "pcm_s16le", "opus": "libopus",
}

func containerExt(c string) string {
	switch c {
	case "hls":
		return "m3u8"
	case "":
		return "mp4"
	}
	return c
}

// OutputPath calculează calea fișierului rezultat
func (o *EncodeOptions) IsStream() bool { return o.OutputMode == "stream" }

// IsURL – intrare/ieșire de rețea (nu fișier local)
func IsURL(s string) bool {
	i := strings.Index(s, "://")
	return i > 1 && !strings.ContainsAny(s[:i], `\/`)
}

// streamFormat alege muxer-ul după protocolul URL-ului de ieșire
func streamFormat(url string) string {
	u := strings.ToLower(url)
	switch {
	case strings.HasPrefix(u, "rtmp://"), strings.HasPrefix(u, "rtmps://"), strings.HasPrefix(u, "rtmpt://"):
		return "flv"
	case strings.HasPrefix(u, "rtsp://"), strings.HasPrefix(u, "rtsps://"):
		return "rtsp"
	case strings.HasPrefix(u, "http://"), strings.HasPrefix(u, "https://"):
		if strings.Contains(u, ".m3u8") {
			return "hls"
		}
		return "mpegts"
	}
	return "mpegts" // srt, udp, tcp, rtp
}

func OutputPath(input string, o *EncodeOptions) string {
	if o.IsStream() {
		return cleanURL(o.StreamURL)
	}
	dir := o.OutDir
	if o.SameDir {
		dir = filepath.Dir(input)
	}
	if dir == "" {
		dir = getCfg().OutputDir
	}
	base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	if IsURL(input) {
		base = urlBaseName(input)
	}
	ext := containerExt(o.Container)
	name := base + o.Suffix
	out := filepath.Join(dir, name+"."+ext)
	if o.Container == "hls" {
		out = filepath.Join(dir, name, "index.m3u8")
	}
	if samePath(out, input) {
		name += "_enc"
		out = filepath.Join(dir, name+"."+ext)
	}
	if !o.Overwrite {
		for i := 1; fileExists(out) && i < 1000; i++ {
			if o.Container == "hls" {
				out = filepath.Join(dir, fmt.Sprintf("%s (%d)", name, i), "index.m3u8")
			} else {
				out = filepath.Join(dir, fmt.Sprintf("%s (%d).%s", name, i, ext))
			}
		}
	}
	return out
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ParseTime: "90", "1:30", "00:01:30.5" -> secunde
func ParseTime(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	if s == "" {
		return 0, false
	}
	parts := strings.Split(s, ":")
	var total float64
	for _, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 {
			return 0, false
		}
		total = total*60 + v
	}
	return total, true
}

func speedPreset(codec string, speed int) []string {
	if speed < 1 || speed > 7 {
		speed = 5
	}
	i := speed - 1
	switch {
	case codec == "libx264" || codec == "libx265":
		p := []string{"ultrafast", "veryfast", "faster", "fast", "medium", "slow", "veryslow"}
		return []string{"-preset", p[i]}
	case strings.HasSuffix(codec, "_nvenc"):
		return []string{"-preset", "p" + strconv.Itoa(speed)}
	case strings.HasSuffix(codec, "_qsv"):
		p := []string{"veryfast", "veryfast", "faster", "fast", "medium", "slow", "veryslow"}
		return []string{"-preset", p[i]}
	case strings.HasSuffix(codec, "_amf"):
		q := "balanced"
		if speed <= 2 {
			q = "speed"
		} else if speed >= 6 {
			q = "quality"
		}
		return []string{"-quality", q}
	case codec == "libsvtav1":
		p := []int{12, 10, 8, 7, 6, 4, 2}
		return []string{"-preset", strconv.Itoa(p[i])}
	case codec == "libvpx-vp9":
		p := []int{8, 5, 4, 3, 2, 1, 0}
		return []string{"-deadline", "good", "-cpu-used", strconv.Itoa(p[i]), "-row-mt", "1"}
	}
	return nil
}

func rateControl(codec string, o *EncodeOptions) []string {
	q := o.Quality
	if o.RateMode == "bitrate" && o.VBitrate != "" {
		a := []string{"-b:v", o.VBitrate}
		switch {
		case strings.HasSuffix(codec, "_nvenc"):
			a = append([]string{"-rc", "vbr"}, a...)
		case strings.HasSuffix(codec, "_amf"):
			// CBR: cu vbr_peak, AMF (mai ales VCE 1-3) folosea doar ~1/3 din bitrate
			// (ex. 375k din 1000k) -> imagine fără detaliu. CBR folosește tot bitrate-ul setat.
			mr := o.MaxRate
			if mr == "" {
				mr = o.VBitrate
			}
			return []string{"-rc", "cbr", "-b:v", o.VBitrate, "-maxrate", mr, "-bufsize", o.VBitrate}
		}
		if o.MaxRate != "" {
			a = append(a, "-maxrate", o.MaxRate, "-bufsize", doubleRate(o.MaxRate))
		}
		return a
	}
	if q <= 0 {
		q = 23
	}
	qs := strconv.Itoa(q)
	switch {
	case codec == "libx264" || codec == "libx265" || codec == "libsvtav1":
		return []string{"-crf", qs}
	case codec == "libvpx-vp9":
		return []string{"-crf", qs, "-b:v", "0"}
	case strings.HasSuffix(codec, "_nvenc"):
		return []string{"-rc", "vbr", "-cq", qs, "-b:v", "0"}
	case strings.HasSuffix(codec, "_qsv"):
		return []string{"-global_quality", qs}
	case strings.HasSuffix(codec, "_amf"):
		return []string{"-rc", "cqp", "-qp_i", qs, "-qp_p", qs, "-qp_b", qs}
	case codec == "mpeg2video" || codec == "mpeg4":
		v := q / 6
		if v < 2 {
			v = 2
		}
		if v > 31 {
			v = 31
		}
		return []string{"-q:v", strconv.Itoa(v)}
	}
	return nil
}

func doubleRate(r string) string {
	r = strings.TrimSpace(r)
	if r == "" {
		return r
	}
	unit := ""
	last := r[len(r)-1]
	if last < '0' || last > '9' {
		unit = string(last)
		r = r[:len(r)-1]
	}
	v, err := strconv.ParseFloat(r, 64)
	if err != nil {
		return r + unit
	}
	return strconv.FormatFloat(v*2, 'f', -1, 64) + unit
}

// escapare cale pentru filtrul subtitles
func filterPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.ReplaceAll(p, ":", "\\:")
	p = strings.ReplaceAll(p, "'", "\\'")
	return p
}

// cleanURL elimină spațiile (ex. "rtmp://host/live /cheie" -> "rtmp://host/live/cheie")
func cleanURL(u string) string {
	return strings.Join(strings.Fields(u), "")
}

// NormRate: "1000" -> "1000k" (valorile fără unitate sunt luate ca kbps), "2.5 M" -> "2.5M"
func NormRate(r string) string {
	r = strings.ReplaceAll(strings.TrimSpace(r), " ", "")
	if r == "" {
		return r
	}
	low := strings.ToLower(r)
	low = strings.TrimSuffix(strings.TrimSuffix(low, "bps"), "b")
	if v, err := strconv.ParseFloat(low, 64); err == nil {
		if v < 100000 { // nimeni nu vrea sub 100 kbit/s în biți – e kbps
			return strconv.FormatFloat(v, 'f', -1, 64) + "k"
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	if n := len(low); n > 1 {
		num, unit := low[:n-1], low[n-1:]
		if _, err := strconv.ParseFloat(num, 64); err == nil && (unit == "k" || unit == "m") {
			if unit == "m" {
				unit = "M"
			}
			return num + unit
		}
	}
	return r
}

// useQSVPipeline – decodare + filtre + encodare complet pe GPU Intel (ideal pentru procesoare slabe)
func useQSVPipeline(o *EncodeOptions) bool {
	m := effectiveQSVMode(o)
	return strings.HasSuffix(o.VCodec, "_qsv") && o.HWDecode && m != "hybrid" && m != "cpudec" && o.Subs != "burn" &&
		strings.TrimSpace(o.VFilter) == "" && o.Container != "gif" && !audioOnlyCont(o)
}

// effectiveQSVMode: modul ales sau, pe „auto”, ultimul mod care a funcționat pe acest PC
func effectiveQSVMode(o *EncodeOptions) string {
	switch o.QSVMode {
	case "d3d11", "dxva2", "hybrid", "cpudec":
		return o.QSVMode
	}
	if m := getCfg().QSVModeOK; m != "" {
		return m
	}
	return "d3d11"
}

// nextQSVMode – ordinea de rezervă pentru „auto”
// ca serviciu Windows (sesiunea 0) DXVA2/Direct3D 9 nu funcționează – îl sărim
func nextQSVMode(m string) string {
	switch m {
	case "d3d11":
		if isService {
			return "hybrid"
		}
		return "dxva2"
	case "dxva2":
		return "hybrid"
	case "hybrid":
		return "cpudec"
	}
	return ""
}

func audioOnlyCont(o *EncodeOptions) bool {
	if o.IsStream() {
		return false
	}
	_, ok := audioOnly[o.Container]
	return ok
}

// BuildArgs construiește argumentele ffmpeg (fără executabil)
func BuildArgs(input, output string, o *EncodeOptions) []string {
	o.VBitrate, o.MaxRate, o.ABitrate = NormRate(o.VBitrate), NormRate(o.MaxRate), NormRate(o.ABitrate)
	if o.IsStream() {
		o.StreamURL = cleanURL(o.StreamURL)
		output = cleanURL(output)
	}
	qsvFull := useQSVPipeline(o)
	a := []string{"-hide_banner", "-nostdin"}
	stream := o.IsStream()
	if o.Overwrite || stream {
		a = append(a, "-y")
	} else {
		a = append(a, "-n")
	}
	if qsvFull {
		if effectiveQSVMode(o) == "dxva2" {
			a = append(a, "-init_hw_device", "qsv=hw:hw_any,child_device_type=dxva2",
				"-hwaccel", "qsv", "-hwaccel_device", "hw", "-hwaccel_output_format", "qsv", "-filter_hw_device", "hw")
		} else {
			a = append(a, "-hwaccel", "qsv", "-hwaccel_output_format", "qsv")
		}
	} else if o.HWDecode && strings.HasSuffix(o.VCodec, "_qsv") {
		// hibrid: decodare D3D11VA (merge și ca serviciu), filtre pe CPU, encodare QSV
		if effectiveQSVMode(o) != "cpudec" {
			a = append(a, "-hwaccel", "d3d11va")
		}
	} else if o.HWDecode && strings.HasSuffix(o.VCodec, "_amf") && runtime.GOOS == "windows" {
		// AMD: D3D11VA (-hwaccel auto alege DXVA2, care nu merge ca serviciu)
		a = append(a, "-hwaccel", "d3d11va")
	} else if o.HWDecode {
		a = append(a, "-hwaccel", "auto")
	}
	start, hasStart := ParseTime(o.Start)
	end, hasEnd := ParseTime(o.End)
	if hasStart && start > 0 {
		a = append(a, "-ss", fmtSec(start))
	}
	if stream {
		if o.Loop && !IsURL(input) {
			a = append(a, "-stream_loop", "-1")
		}
		if o.Realtime && !IsURL(input) {
			a = append(a, "-re")
		}
		if IsURL(input) {
			l := strings.ToLower(input)
			if strings.HasPrefix(l, "http") {
				a = append(a, "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5")
			}
			if strings.HasPrefix(l, "rtsp") {
				a = append(a, "-rtsp_transport", "tcp")
			}
		}
	}
	a = append(a, splitArgs(o.InArgs)...)
	a = append(a, "-i", UDPInput(input))
	if hasEnd && end > start {
		a = append(a, "-t", fmtSec(end-start))
	}

	cont := o.Container
	if cont == "" {
		cont = "mp4"
	}
	if stream {
		cont = "stream"
	}
	_, isAudio := audioOnly[cont]

	if o.MapAll && !isAudio && cont != "gif" {
		a = append(a, "-map", "0")
		if o.Subs == "none" || o.Subs == "burn" {
			a = append(a, "-map", "-0:s")
		}
		a = append(a, "-map", "-0:d?", "-map", "-0:t?")
	}

	// ---------- VIDEO ----------
	vc := o.VCodec
	if isAudio || vc == "none" {
		a = append(a, "-vn")
	} else if cont == "gif" {
		fps := o.FPS
		if fps == "" {
			fps = "12"
		}
		scale := "480:-1"
		if o.Resolution == "custom" && o.Width > 0 {
			scale = strconv.Itoa(o.Width) + ":-1"
		} else if h, err := strconv.Atoi(o.Resolution); err == nil && h > 0 {
			scale = "-2:" + strconv.Itoa(h)
		}
		a = append(a, "-filter_complex",
			fmt.Sprintf("fps=%s,scale=%s:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse", fps, scale),
			"-loop", "0", "-an")
	} else if vc == "copy" || vc == "" {
		a = append(a, "-c:v", "copy")
	} else {
		a = append(a, "-c:v", vc)
		a = append(a, speedPreset(vc, o.Speed)...)
		a = append(a, rateControl(vc, o)...)
		if o.Tune != "" && (vc == "libx264" || vc == "libx265") {
			a = append(a, "-tune", o.Tune)
		}
		if vc == "prores_ks" {
			a = append(a, "-profile:v", "3")
		}
		var vf []string
		if qsvFull {
			// totul pe GPU: vpp_qsv face deîntrețesere + scalare + format
			var vpp []string
			if o.Deinterlace {
				vpp = append(vpp, "deinterlace=2")
			}
			switch o.Resolution {
			case "", "keep":
			case "custom":
				switch {
				case o.Width > 0 && o.Height > 0:
					vpp = append(vpp, fmt.Sprintf("w=%d:h=%d", o.Width, o.Height))
				case o.Height > 0:
					vpp = append(vpp, fmt.Sprintf("w=trunc(%d*dar/2)*2:h=%d", o.Height, o.Height))
				case o.Width > 0:
					vpp = append(vpp, fmt.Sprintf("w=%d:h=trunc(%d/dar/2)*2", o.Width, o.Width))
				}
			default:
				if h, err := strconv.Atoi(o.Resolution); err == nil && h > 0 {
					vpp = append(vpp, fmt.Sprintf("w=trunc(%d*dar/2)*2:h=%d", h, h))
				}
			}
			if strings.Contains(o.PixFmt, "10") {
				vpp = append(vpp, "format=p010")
			}
			if af := aspectFilter(o.Aspect); af != "" && len(vpp) == 0 {
				a = append(a, "-vf", af)
			}
			if len(vpp) > 0 {
				f := "vpp_qsv=" + strings.Join(vpp, ":")
				if af := aspectFilter(o.Aspect); af != "" {
					f = af + "," + f
				}
				if o.Resolution != "" && o.Resolution != "keep" {
					f += ",setsar=1" // lățimea e calculată din DAR -> pixeli pătrați
				}
				a = append(a, "-vf", f)
			}
			if o.FPS != "" && o.FPS != "keep" {
				a = append(a, "-r", o.FPS)
			}
			if o.GOP > 0 {
				a = append(a, "-g", strconv.Itoa(o.GOP))
			}
			goto audio
		}
		fastDeint := o.Deinterlace && o.DeintMode == "fast"
		square := o.PixelMode == "square"
		if f := aspectFilter(o.Aspect); f != "" {
			vf = append(vf, f) // întâi corectăm raportul declarat de sursă
		}
		if o.Deinterlace && !fastDeint {
			// bwdif: calitate mai bună și ~2x mai rapid decât yadif (SIMD)
			vf = append(vf, "bwdif=mode=send_frame:parity=auto:deint=all")
		}
		if fastDeint {
			// un singur câmp (jumătate din linii) + scalare: de ~7x mai ieftin decât bwdif
			sf := scaleFlags(vc)
			vf = append(vf, "field=top")
			h, err := strconv.Atoi(o.Resolution)
			switch {
			// după field, "dar" e dublu față de imaginea reală (jumătate din linii) -> împărțim la 2
			case o.Resolution == "custom" && o.Width > 0 && o.Height > 0:
				vf = append(vf, fmt.Sprintf("scale=%d:%d:flags=%s,setsar=1", o.Width, o.Height, sf))
			case o.Resolution == "custom" && o.Height > 0:
				vf = append(vf, fieldScale(strconv.Itoa(o.Height), square, sf))
			case o.Resolution == "custom" && o.Width > 0:
				vf = append(vf, fmt.Sprintf("scale=%d:trunc(%d/dar)*2:flags=%s,setsar=1", o.Width, o.Width, sf))
			case err == nil && h > 0:
				vf = append(vf, fieldScale(strconv.Itoa(h), square, sf))
			default:
				vf = append(vf, fieldScale("ih*2", square, sf))
			}
		}
		sf := scaleFlags(vc)
		switch o.Resolution {
		case "", "keep":
			// rezoluția sursei se păstrează; doar la cerere pixeli pătrați (SD 16:9 720x576 -> 1024x576)
			if !fastDeint && square {
				vf = append(vf, squareScale("ih", false, sf))
			}
		case "custom":
			if fastDeint {
				break
			}
			switch {
			case o.Width > 0 && o.Height > 0:
				vf = append(vf, fmt.Sprintf("scale=%d:%d:flags=%s,setsar=1", o.Width, o.Height, sf))
			case o.Height > 0 && square:
				vf = append(vf, squareScale(strconv.Itoa(o.Height), false, sf))
			case o.Height > 0:
				vf = append(vf, fmt.Sprintf("scale=-2:%d:flags=%s", o.Height, sf))
			case o.Width > 0:
				vf = append(vf, fmt.Sprintf("scale=%d:trunc(%d/dar/2)*2:flags=%s,setsar=1", o.Width, o.Width, sf))
			}
		default:
			if h, err := strconv.Atoi(o.Resolution); err == nil && h > 0 && !fastDeint {
				if square {
					vf = append(vf, squareScale(strconv.Itoa(h), false, sf))
				} else {
					vf = append(vf, fmt.Sprintf("scale=-2:%d:flags=%s", h, sf)) // păstrează forma pixelilor sursei
				}
			}
		}
		if o.Subs == "burn" {
			vf = append(vf, fmt.Sprintf("subtitles='%s':si=0", filterPath(input)))
		}
		if strings.TrimSpace(o.VFilter) != "" {
			vf = append(vf, strings.TrimSpace(o.VFilter))
		}
		if strings.HasSuffix(vc, "_amf") {
			// AMD AMF: dacă sursa / decodarea D3D11 vine în gamă „full”, o aducem în gama TV (16-235)
			vf = append(vf, "scale=out_range=tv")
		}
		if len(vf) > 0 {
			a = append(a, "-vf", strings.Join(vf, ","))
		}
		if o.FPS != "" && o.FPS != "keep" {
			a = append(a, "-r", o.FPS)
		}
		if pf := hwPixFmt(vc, o.PixFmt); pf != "" {
			a = append(a, "-pix_fmt", pf)
		}
		// AMD AMF (mai ales VCE 1-3, ex. Futro S930) marchează streamul ca „full range”:
		// playerul nu mai întinde 16-235 → negru gri, imagine spălăcită. Forțăm marcajul TV range.
		switch vc {
		case "h264_amf":
			a = append(a, "-color_range", "tv", "-bsf:v", "h264_metadata=video_full_range_flag=0")
		case "hevc_amf":
			a = append(a, "-color_range", "tv", "-bsf:v", "hevc_metadata=video_full_range_flag=0")
		}
		if o.GOP > 0 {
			a = append(a, "-g", strconv.Itoa(o.GOP))
		}
	}
audio:

	// ---------- AUDIO ----------
	ac := o.ACodec
	if isAudio && (ac == "" || ac == "none") {
		ac = audioOnly[cont]
	}
	if cont == "gif" {
		// deja -an
	} else if ac == "none" {
		a = append(a, "-an")
	} else if ac == "copy" || ac == "" {
		a = append(a, "-c:a", "copy")
	} else {
		a = append(a, "-c:a", ac)
		if o.ABitrate != "" && ac != "flac" && !strings.HasPrefix(ac, "pcm_") {
			a = append(a, "-b:a", o.ABitrate)
		}
		if o.AChannels != "" {
			a = append(a, "-ac", o.AChannels)
		}
		if o.ASampleRate != "" {
			a = append(a, "-ar", o.ASampleRate)
		}
		var af []string
		if v := strings.TrimSpace(o.Volume); v != "" && v != "0" {
			af = append(af, "volume="+v+"dB")
		}
		if o.Loudnorm != "" {
			tp := "-1.5"
			if o.Loudnorm == "-23" {
				tp = "-1"
			}
			af = append(af, fmt.Sprintf("loudnorm=I=%s:TP=%s:LRA=11", o.Loudnorm, tp))
		}
		if strings.TrimSpace(o.AFilter) != "" {
			af = append(af, strings.TrimSpace(o.AFilter))
		}
		if len(af) > 0 {
			a = append(a, "-af", strings.Join(af, ","))
		}
	}

	// ---------- SUBTITRĂRI ----------
	if !isAudio && cont != "gif" {
		switch o.Subs {
		case "none", "burn":
			a = append(a, "-sn")
		case "copy":
			if cont == "mp4" || cont == "mov" {
				a = append(a, "-c:s", "mov_text")
			} else if cont == "webm" {
				a = append(a, "-c:s", "webvtt")
			} else if cont == "ts" || cont == "hls" || cont == "stream" {
				a = append(a, "-sn")
			} else {
				a = append(a, "-c:s", "copy")
			}
		}
	}

	// ---------- CONTAINER ----------
	switch cont {
	case "mp4", "mov", "m4a":
		if o.FastStart {
			a = append(a, "-movflags", "+faststart")
		}
	case "hls":
		t := o.HLSTime
		if t <= 0 {
			t = 6
		}
		seg := filepath.Join(filepath.Dir(output), "seg_%05d.ts")
		a = append(a, "-f", "hls", "-hls_time", strconv.Itoa(t), "-hls_playlist_type", "vod",
			"-hls_segment_filename", seg)
	case "ts":
		a = append(a, "-f", "mpegts")
	case "stream":
		f := streamFormat(output)
		switch f {
		case "flv":
			a = append(a, "-f", "flv", "-flvflags", "no_duration_filesize")
		case "rtsp":
			a = append(a, "-f", "rtsp", "-rtsp_transport", "tcp")
		case "hls":
			a = append(a, "-f", "hls", "-hls_time", "4", "-hls_list_size", "6", "-hls_flags", "delete_segments", "-method", "PUT")
		default:
			a = append(a, "-f", "mpegts")
			if strings.HasPrefix(strings.ToLower(output), "udp://") {
				a = append(a, "-mpegts_flags", "+resend_headers")
			}
		}
	}

	a = append(a, splitArgs(o.ExtraArgs)...)
	a = append(a, output)
	return a
}

func fmtSec(s float64) string {
	return strconv.FormatFloat(s, 'f', 3, 64)
}

// splitArgs împarte un șir în argumente, respectând ghilimelele
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQ := rune(0)
	has := false
	for _, r := range s {
		switch {
		case inQ != 0:
			if r == inQ {
				inQ = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			inQ = r
			has = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if cur.Len() > 0 || has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 || has {
		out = append(out, cur.String())
	}
	return out
}

// CommandString – pentru afișare / copiere
func CommandString(exe string, args []string) string {
	parts := []string{quoteArg(exe)}
	for _, a := range args {
		parts = append(parts, quoteArg(a))
	}
	return strings.Join(parts, " ")
}

func quoteArg(a string) string {
	if a == "" || strings.ContainsAny(a, " \t\"&|<>^;,()[]'") {
		return `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
	}
	return a
}

// hwPixFmt adaptează formatul pixel la ce acceptă encoderele hardware
func hwPixFmt(codec, pf string) string {
	if pf == "" {
		return ""
	}
	if strings.HasSuffix(codec, "_qsv") || strings.HasSuffix(codec, "_amf") {
		switch pf {
		case "yuv420p":
			return "nv12"
		case "yuv420p10le":
			return "p010le"
		}
	}
	return pf
}

// scaleFlags: cu encodere hardware procesorul face doar filtrele – folosim scalare ușoară
// (bilinear e de ~3-4 ori mai rapid decât lanczos, diferența vizuală la 1080→720 e minimă)
func scaleFlags(codec string) string {
	if strings.HasSuffix(codec, "_qsv") || strings.HasSuffix(codec, "_nvenc") || strings.HasSuffix(codec, "_amf") {
		return "bilinear"
	}
	return "lanczos"
}

// UDPInput completează URL-urile udp:// / rtp:// (multicast) cu opțiunile necesare:
// interfața de rețea (localaddr), buffer mare și timeout (ca stream-ul să repornească dacă sursa cade)
func UDPInput(in string) string {
	l := strings.ToLower(in)
	if !strings.HasPrefix(l, "udp://") && !strings.HasPrefix(l, "rtp://") {
		return in
	}
	in = strings.Replace(in, "://@", "://", 1) // stil VLC: udp://@239.1.1.1:1234
	base, q, _ := strings.Cut(in, "?")
	opts := map[string]bool{}
	for _, kv := range strings.Split(q, "&") {
		if k, _, _ := strings.Cut(kv, "="); k != "" {
			opts[strings.ToLower(k)] = true
		}
	}
	var add []string
	if ip := getCfg().McastIface; ip != "" && !opts["localaddr"] {
		add = append(add, "localaddr="+ip)
	}
	if !opts["fifo_size"] {
		add = append(add, "fifo_size=1000000")
	}
	if !opts["overrun_nonfatal"] {
		add = append(add, "overrun_nonfatal=1")
	}
	if !opts["buffer_size"] {
		add = append(add, "buffer_size=8388608")
	}
	if !opts["timeout"] {
		add = append(add, "timeout=10000000") // 10 s fără date -> eroare -> repornire automată
	}
	if len(add) == 0 {
		return in
	}
	if q != "" {
		return base + "?" + q + "&" + strings.Join(add, "&")
	}
	return base + "?" + strings.Join(add, "&")
}

// urlBaseName: "udp://@239.1.1.1:1234?x" -> "udp_239.1.1.1_1234", "http://h:8000/play/M9TV" -> "M9TV"
func urlBaseName(u string) string {
	scheme, rest, _ := strings.Cut(u, "://")
	rest, _, _ = strings.Cut(rest, "?")
	rest = strings.TrimPrefix(rest, "@")
	if i := strings.LastIndex(rest, "/"); i >= 0 && i < len(rest)-1 {
		rest = rest[i+1:] // ultimul segment din cale (numele canalului)
		if ext := filepath.Ext(rest); len(ext) >= 2 && len(ext) <= 6 && strings.ContainsAny(strings.ToLower(ext), "abcdefghijklmnopqrstuvwxyz") && !strings.Contains(ext, ":") {
			rest = strings.TrimSuffix(rest, ext) // .ts, .m3u8, .mp4…
		}
	} else {
		rest = scheme + "_" + strings.TrimSuffix(rest, "/")
	}
	var b strings.Builder
	for _, r := range rest {
		if r == '.' || r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "stream"
	}
	return b.String()
}

func isHWEnc(c string) bool {
	return strings.HasSuffix(c, "_qsv") || strings.HasSuffix(c, "_nvenc") || strings.HasSuffix(c, "_amf")
}

// squareScale scalează la înălțimea h păstrând raportul de afișare (DAR, ține cont de SAR),
// cu pixeli pătrați. field=true: intrarea are doar jumătate din linii (după filtrul field).
func squareScale(h string, field bool, flags string) string {
	div := "2"
	if field {
		div = "4"
	}
	return fmt.Sprintf("scale=trunc(%s*dar/%s)*2:%s:flags=%s,setsar=1", h, div, h, flags)
}

// aspectFilter forțează raportul de afișare (sursă marcată greșit, ex. 16:9 transmis ca 4:3)
func aspectFilter(a string) string {
	switch a {
	case "16:9":
		return "setdar=16/9"
	case "4:3":
		return "setdar=4/3"
	}
	return ""
}

// fieldScale – scalare după filtrul field (jumătate din linii)
func fieldScale(h string, square bool, flags string) string {
	if square {
		return squareScale(h, true, flags)
	}
	// păstrează forma pixelilor sursei: lățimea din geometria pixelilor, SAR readus la valoarea sursei
	if h == "ih*2" {
		return "scale=iw:ih*2:flags=" + flags + ",setsar=sar/2"
	}
	return fmt.Sprintf("scale=trunc(%s*iw/(2*ih)/2)*2:%s:flags=%s,setsar=sar/2", h, h, flags)
}
