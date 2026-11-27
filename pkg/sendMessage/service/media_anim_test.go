package send_service

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
)

func gifBytes(t *testing.T, frames int) []byte {
	t.Helper()
	pal := color.Palette{color.Black, color.White}
	g := &gif.GIF{}
	for i := 0; i < frames; i++ {
		g.Image = append(g.Image, image.NewPaletted(image.Rect(0, 0, 4, 4), pal))
		g.Delay = append(g.Delay, 10)
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestMarkGif(t *testing.T) {
	d := &MediaStruct{Type: "GIF"}
	markGif(d)
	if d.Type != "video" || !d.asGif {
		t.Fatalf("gif deveria virar video com asGif: %+v", d)
	}
	v := &MediaStruct{Type: "video"}
	markGif(v)
	if v.asGif {
		t.Fatal("video comum não é gif")
	}
}

func TestIsAnimatedSource(t *testing.T) {
	if !isAnimatedSource(gifBytes(t, 3)) {
		t.Fatal("GIF com 3 quadros é animado")
	}
	if isAnimatedSource(gifBytes(t, 1)) {
		t.Fatal("GIF de 1 quadro é estático")
	}
	animWebp := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00"), 0x02, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	if !isAnimatedSource(animWebp) {
		t.Fatal("WebP com flag ANIM é animado")
	}
}

func TestPrepareGifPassThroughAndReject(t *testing.T) {
	mp4 := []byte("fake-mp4")
	out, mime, err := prepareGif(mp4, "video/mp4")
	if err != nil || mime != "video/mp4" || !bytes.Equal(out, mp4) {
		t.Fatalf("mp4 deve passar direto: %v %s", err, mime)
	}
	if _, _, err := prepareGif([]byte("x"), "image/png"); err == nil {
		t.Fatal("png não é gif")
	}
}
