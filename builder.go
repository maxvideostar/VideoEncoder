package main

import (
	"fmt"
	"os"
	"path/filepath"
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
	PixFmt      string `json:"pix_fmt"`
	GOP         int    `json:"gop"`
	Tune        string `json:"tune"`
	HWDecode    bool   `json:"hw_decode"`

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
func OutputPath(input string, o *EncodeOptions) string {
	dir := o.OutDir
	if o.SameDir {
		dir = filepath.Dir(input)
	}
	if dir == "" {
		dir = getCfg().OutputDir
	}
	base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
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
			a = append([]string{"-rc", "vbr_peak"}, a...)
		}
		if o.MaxRate != "" {
			a = append(a, "-maxrate", o.MaxRate, "-bufsize", doubleRate(o.MaxRate))
		} else if strings.HasSuffix(codec, "_amf") {
			a = append(a, "-maxrate", o.VBitrate)
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

// BuildArgs construiește argumentele ffmpeg (fără executabil)
func BuildArgs(input, output string, o *EncodeOptions) []string {
	a := []string{"-hide_banner", "-nostdin"}
	if o.Overwrite {
		a = append(a, "-y")
	} else {
		a = append(a, "-n")
	}
	if o.HWDecode {
		a = append(a, "-hwaccel", "auto")
	}
	start, hasStart := ParseTime(o.Start)
	end, hasEnd := ParseTime(o.End)
	if hasStart && start > 0 {
		a = append(a, "-ss", fmtSec(start))
	}
	a = append(a, splitArgs(o.InArgs)...)
	a = append(a, "-i", input)
	if hasEnd && end > start {
		a = append(a, "-t", fmtSec(end-start))
	}

	cont := o.Container
	if cont == "" {
		cont = "mp4"
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
		if o.Deinterlace {
			vf = append(vf, "bwdif=mode=send_frame:parity=auto:deint=all")
		}
		switch o.Resolution {
		case "", "keep":
		case "custom":
			w, h := o.Width, o.Height
			if w <= 0 {
				w = -2
			}
			if h <= 0 {
				h = -2
			}
			if !(w == -2 && h == -2) {
				vf = append(vf, fmt.Sprintf("scale=%d:%d:flags=lanczos", w, h))
			}
		default:
			if h, err := strconv.Atoi(o.Resolution); err == nil && h > 0 {
				vf = append(vf, fmt.Sprintf("scale=-2:%d:flags=lanczos", h))
			}
		}
		if o.Subs == "burn" {
			vf = append(vf, fmt.Sprintf("subtitles='%s':si=0", filterPath(input)))
		}
		if strings.TrimSpace(o.VFilter) != "" {
			vf = append(vf, strings.TrimSpace(o.VFilter))
		}
		if len(vf) > 0 {
			a = append(a, "-vf", strings.Join(vf, ","))
		}
		if o.FPS != "" && o.FPS != "keep" {
			a = append(a, "-r", o.FPS)
		}
		if o.PixFmt != "" {
			a = append(a, "-pix_fmt", o.PixFmt)
		}
		if o.GOP > 0 {
			a = append(a, "-g", strconv.Itoa(o.GOP))
		}
	}

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
			} else if cont == "ts" || cont == "hls" {
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
