package send_service

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestFitStickerKeepsAspectAndTransparency(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 800, 200))
	for x := 0; x < 800; x++ {
		for y := 0; y < 200; y++ {
			src.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	out := fitSticker(src)
	if out.Bounds().Dx() != 512 || out.Bounds().Dy() != 512 {
		t.Fatalf("tamanho %v", out.Bounds())
	}
	if _, _, _, a := out.At(256, 10).RGBA(); a != 0 {
		t.Fatalf("faixa de cima deveria ser transparente")
	}
	if r, _, _, a := out.At(256, 256).RGBA(); a == 0 || r == 0 {
		t.Fatalf("centro deveria ter a imagem")
	}
}

func TestLoadStickerSourceBase64(t *testing.T) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	for _, in := range []string{b64, "data:image/png;base64," + b64} {
		got, err := loadStickerSource(in)
		if err != nil || !bytes.Equal(got, buf.Bytes()) {
			t.Fatalf("entrada %q: err=%v", in[:20], err)
		}
	}
	if _, err := loadStickerSource("não é base64 !!"); err == nil {
		t.Fatal("esperava erro")
	}
	if _, err := loadStickerSource("data:image/png,abc"); err == nil {
		t.Fatal("data URI sem base64 deveria falhar")
	}
}

// webp simples (VP8) falso: só a estrutura RIFF importa para addWebpExif.
func fakeWebp(id string, payload []byte) []byte {
	var b bytes.Buffer
	b.WriteString("WEBP")
	b.WriteString(id)
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(payload)))
	b.Write(payload)
	if len(payload)%2 == 1 {
		b.WriteByte(0)
	}
	out := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(b.Len()))...)
	return append(out, b.Bytes()...)
}

func chunksOf(t *testing.T, w []byte) map[string][]byte {
	t.Helper()
	if int(binary.LittleEndian.Uint32(w[4:8])) != len(w)-8 {
		t.Fatalf("tamanho RIFF errado")
	}
	m := map[string][]byte{}
	var order []string
	for p := 12; p+8 <= len(w); {
		id := string(w[p : p+4])
		n := int(binary.LittleEndian.Uint32(w[p+4 : p+8]))
		m[id] = w[p+8 : p+8+n]
		order = append(order, id)
		p += 8 + n + n%2
	}
	if order[0] != "VP8X" || order[len(order)-1] != "EXIF" {
		t.Fatalf("ordem dos chunks: %v", order)
	}
	return m
}

func TestAddWebpExif(t *testing.T) {
	exif := stickerExif(stickerMeta{PackName: "Pacote", PackPublisher: "Autor", Emojis: []string{"😀", "", "👍", "a", "b"}})
	var meta map[string]any
	if err := json.Unmarshal(exif[22:], &meta); err != nil {
		t.Fatal(err)
	}
	if meta["sticker-pack-name"] != "Pacote" || meta["sticker-pack-publisher"] != "Autor" || len(meta["emojis"].([]any)) != 3 {
		t.Fatalf("meta %v", meta)
	}
	if int(binary.LittleEndian.Uint32(exif[14:18])) != len(exif)-22 {
		t.Fatal("tamanho do JSON no EXIF errado")
	}

	// VP8 simples -> ganha VP8X com flag EXIF e canvas 512x512
	out, err := addWebpExif(fakeWebp("VP8 ", []byte{1, 2, 3}), exif, 512, 512)
	if err != nil {
		t.Fatal(err)
	}
	c := chunksOf(t, out)
	x := c["VP8X"]
	if x[0]&0x08 == 0 || int(x[4])|int(x[5])<<8|int(x[6])<<16 != 511 {
		t.Fatalf("VP8X %v", x)
	}
	if !bytes.Equal(c["VP8 "], []byte{1, 2, 3}) || !bytes.Equal(c["EXIF"], exif) {
		t.Fatal("conteúdo alterado")
	}

	// já estendido (VP8X+ALPH+VP8): mantém e só liga a flag
	ext := fakeWebp("VP8X", []byte{0x10, 0, 0, 0, 0xFF, 1, 0, 0xFF, 1, 0})
	out, err = addWebpExif(ext, exif, 512, 512)
	if err != nil {
		t.Fatal(err)
	}
	if v := chunksOf(t, out)["VP8X"][0]; v != 0x18 {
		t.Fatalf("flags %x", v)
	}
	if _, err := addWebpExif([]byte("lixo"), exif, 1, 1); err == nil {
		t.Fatal("esperava erro")
	}
}
