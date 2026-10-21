package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"math/rand"
)

// OggOpusInfo returns the duration of an Ogg/Opus voice note and a 64-sample waveform
// for WhatsApp to draw. Moved from the upstream analyzeOggOpus, which read the pre-skip
// from the wrong offset and divided by the "input sample rate"; Opus granule positions
// are always 48 kHz.
func OggOpusInfo(data []byte) (seconds uint32, waveform []byte, err error) {
	if len(data) < 4 || string(data[:4]) != "OggS" {
		return 0, nil, errors.New("not an Ogg file")
	}
	var lastGranule uint64
	var preSkip uint16
	for i := 0; i+27 < len(data); {
		if string(data[i:i+4]) != "OggS" {
			i++
			continue
		}
		granule := binary.LittleEndian.Uint64(data[i+6 : i+14])
		segments := int(data[i+26])
		if i+27+segments > len(data) {
			break
		}
		size := 27 + segments
		for _, n := range data[i+27 : i+27+segments] {
			size += int(n)
		}
		page := data[i:min(i+size, len(data))]
		// OpusHead: "OpusHead"(8) version(1) channels(1) pre-skip(2) input rate(4) …
		if h := bytes.Index(page, []byte("OpusHead")); h >= 0 && h+12 <= len(page) {
			preSkip = binary.LittleEndian.Uint16(page[h+10 : h+12])
		}
		if granule != 0 && granule != math.MaxUint64 { // -1 means "no packet ends on this page"
			lastGranule = granule
		}
		i += size
	}
	if lastGranule <= uint64(preSkip) {
		return 0, nil, errors.New("no audio length in the Ogg file")
	}
	secs := math.Ceil(float64(lastGranule-uint64(preSkip)) / 48000)
	seconds = uint32(min(max(secs, 1), 3600))
	return seconds, placeholderWaveform(seconds), nil
}

// placeholderWaveform draws a plausible 0–100 waveform; WhatsApp wants 64 samples.
// It is the same for the same duration.
func placeholderWaveform(seconds uint32) []byte {
	const n = 64
	r := rand.New(rand.NewSource(int64(seconds)))
	freq := float64(min(int(seconds), 120)) / 30
	w := make([]byte, n)
	for i := range w {
		pos := float64(i) / n
		v := 35*math.Sin(pos*math.Pi*freq*8) + 17.5*math.Sin(pos*math.Pi*freq*16)
		v += (r.Float64() - 0.5) * 15
		v = v*(0.7+0.3*math.Sin(pos*math.Pi)) + 50
		w[i] = byte(min(max(v, 0), 100))
	}
	return w
}
