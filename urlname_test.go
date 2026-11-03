package main

import "testing"

func TestURLBaseName(t *testing.T) {
	for in, want := range map[string]string{
		"udp://@239.1.1.1:1234":                    "udp_239.1.1.1_1234",
		"udp://239.1.1.1:1234?pkt_size=1316":       "udp_239.1.1.1_1234",
		"http://178.83.113.122:8000/play/M9TV":     "M9TV",
		"http://127.0.0.1:4022/udp/239.1.1.1:1234": "239.1.1.1_1234",
		"http://h/live/canal.m3u8":                 "canal",
		"rtmp://s2.netstreaming.eu/live/test":      "test",
	} {
		if got := urlBaseName(in); got != want {
			t.Errorf("%s: %s != %s", in, got, want)
		}
	}
	if got := UDPInput("udp://@239.1.1.1:1234?pkt_size=1316"); got != "udp://239.1.1.1:1234?pkt_size=1316&fifo_size=1000000&overrun_nonfatal=1&buffer_size=8388608&timeout=10000000" {
		t.Errorf("UDPInput: %s", got)
	}
}
