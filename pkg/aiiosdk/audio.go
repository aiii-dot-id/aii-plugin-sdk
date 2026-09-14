//go:build !wasm_unknown

package aiiosdk

// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
)

// .
type AudioKind uint8

const (
	AudioPCM           AudioKind = 1
	AudioDiscontinuity AudioKind = 2
	AudioEnd           AudioKind = 3
)

// .
type AudioFrame struct {
	Kind   AudioKind
	Stream uint32
	Seq    uint32
	Start  int64
	PCM    []byte
}

// .
func (fr AudioFrame) Samples(channels int) int64 {
	if channels <= 0 {
		channels = 1
	}
	return int64(len(fr.PCM) / (2 * channels))
}

// .
const MaxAudioFramePayload = 64 * 1024

const audioHeaderBytes = 28

var audioMagic = [4]byte{'A', 'U', 'D', '1'}

var (
	ErrAudioBadMagic    = errors.New("aiiosdk: audio frame does not start with the magic")
	ErrAudioBadKind     = errors.New("aiiosdk: audio frame kind is not pcm, discontinuity or end")
	ErrAudioFrameTooBig = errors.New("aiiosdk: audio frame payload over the ceiling")
	// .
	ErrNoAudioPair = errors.New("aiiosdk: no audio pair: AII_AUDIO_IN_FD and AII_AUDIO_OUT_FD are not set")
)

// .
// .
// .
func WriteAudioFrame(w io.Writer, fr AudioFrame) error {
	if fr.Kind != AudioPCM && fr.Kind != AudioDiscontinuity && fr.Kind != AudioEnd {
		return ErrAudioBadKind
	}
	if fr.Kind != AudioPCM && len(fr.PCM) != 0 {
		return fmt.Errorf("aiiosdk: an audio frame of kind %d carries no payload", fr.Kind)
	}
	if len(fr.PCM) > MaxAudioFramePayload {
		return ErrAudioFrameTooBig
	}
	var h [audioHeaderBytes]byte
	copy(h[0:4], audioMagic[:])
	h[4] = byte(fr.Kind)
	binary.BigEndian.PutUint32(h[8:12], fr.Stream)
	binary.BigEndian.PutUint32(h[12:16], fr.Seq)
	binary.BigEndian.PutUint64(h[16:24], uint64(fr.Start))
	binary.BigEndian.PutUint32(h[24:28], uint32(len(fr.PCM)))
	if _, err := w.Write(h[:]); err != nil {
		return err
	}
	if len(fr.PCM) > 0 {
		if _, err := w.Write(fr.PCM); err != nil {
			return err
		}
	}
	return nil
}

// .
// .
func ReadAudioFrame(r io.Reader) (AudioFrame, error) {
	var h [audioHeaderBytes]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return AudioFrame{}, err
	}
	if [4]byte(h[0:4]) != audioMagic {
		return AudioFrame{}, ErrAudioBadMagic
	}
	fr := AudioFrame{Kind: AudioKind(h[4]), Stream: binary.BigEndian.Uint32(h[8:12]), Seq: binary.BigEndian.Uint32(h[12:16]), Start: int64(binary.BigEndian.Uint64(h[16:24]))}
	n := binary.BigEndian.Uint32(h[24:28])
	if fr.Kind != AudioPCM && fr.Kind != AudioDiscontinuity && fr.Kind != AudioEnd {
		return AudioFrame{}, ErrAudioBadKind
	}
	if n > MaxAudioFramePayload {
		return AudioFrame{}, ErrAudioFrameTooBig
	}
	if fr.Kind != AudioPCM && n != 0 {
		return AudioFrame{}, fmt.Errorf("aiiosdk: an audio frame of kind %d carries no payload", fr.Kind)
	}
	if n > 0 {
		fr.PCM = make([]byte, n)
		if _, err := io.ReadFull(r, fr.PCM); err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return AudioFrame{}, err
		}
	}
	return fr, nil
}

// .
// .
// .
type AudioPair struct {
	in  *os.File
	out *os.File
	wmu sync.Mutex
}

// .
// .
// .
func NewAudioPair(in, out *os.File) *AudioPair { return &AudioPair{in: in, out: out} }

// .
// .
func (p *AudioPair) Read() (AudioFrame, error) { return ReadAudioFrame(p.in) }

// .
func (p *AudioPair) Write(fr AudioFrame) error {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	return WriteAudioFrame(p.out, fr)
}

var (
	audioOnce sync.Once
	audioPair *AudioPair
	audioErr  error
)

// .
// .
// .
func (s *Session) Audio() (*AudioPair, error) {
	audioOnce.Do(func() {
		inFD, err1 := strconv.Atoi(os.Getenv("AII_AUDIO_IN_FD"))
		outFD, err2 := strconv.Atoi(os.Getenv("AII_AUDIO_OUT_FD"))
		if err1 != nil || err2 != nil {
			audioErr = ErrNoAudioPair
			return
		}
		audioPair = &AudioPair{in: os.NewFile(uintptr(inFD), "audio-in"), out: os.NewFile(uintptr(outFD), "audio-out")}
	})
	return audioPair, audioErr
}
