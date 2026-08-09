package send_service

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/chai2010/webp"
)

func amostra(w, h int) image.Image {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			m.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 120, 255})
		}
	}
	return m
}

// TestGerarThumbnail trava o contrato que importa: entrada ruim devolve nil em vez
// de erro. Miniatura ausente é degradação aceitável; envio que falha por causa de
// uma imagem quebrada seria regressão.
func TestGerarThumbnail(t *testing.T) {
	var jpgBuf, pngBuf bytes.Buffer
	if err := jpeg.Encode(&jpgBuf, amostra(800, 600), nil); err != nil {
		t.Fatalf("preparo do jpeg falhou: %v", err)
	}
	if err := png.Encode(&pngBuf, amostra(640, 480)); err != nil {
		t.Fatalf("preparo do png falhou: %v", err)
	}
	// WebP só decodifica porque github.com/chai2010/webp registra o decoder no
	// image.Decode. Se esse import cair, este caso quebra e avisa.
	webpData, err := webp.EncodeRGBA(amostra(500, 500), 80)
	if err != nil {
		t.Fatalf("preparo do webp falhou: %v", err)
	}

	jpegDe := func(w, h int) []byte {
		var b bytes.Buffer
		if err := jpeg.Encode(&b, amostra(w, h), nil); err != nil {
			t.Fatalf("preparo do jpeg %dx%d falhou: %v", w, h, err)
		}
		return b.Bytes()
	}

	casos := []struct {
		nome    string
		entrada []byte
		querNil bool
	}{
		{"jpeg 800x600", jpgBuf.Bytes(), false},
		{"png 640x480", pngBuf.Bytes(), false},
		{"webp 500x500", webpData, false},
		{"1x1", jpegDe(1, 1), false},
		{"panorâmico 2000x100", jpegDe(2000, 100), false},

		// Abaixo: tudo que precisa virar nil sem derrubar o envio.
		{"html de 404 no lugar da imagem", []byte("<html>404 Not Found</html>"), true},
		{"mp4 (image.Decode não lê vídeo)", []byte("\x00\x00\x00\x20ftypmp42\x00\x00\x00\x00"), true},
		{"vazio", []byte{}, true},
		{"nil", nil, true},
		{"jpeg truncado", jpgBuf.Bytes()[:20], true},
	}

	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := gerarThumbnail(c.entrada)

			if c.querNil {
				if got != nil {
					t.Errorf("esperava nil, veio %d bytes", len(got))
				}
				return
			}

			if got == nil {
				t.Fatal("veio nil, esperava miniatura")
			}

			img, format, err := image.Decode(bytes.NewReader(got))
			if err != nil {
				t.Fatalf("miniatura não decodifica: %v", err)
			}
			if format != "jpeg" {
				t.Errorf("formato %q, esperava jpeg", format)
			}
			if w := img.Bounds().Dx(); w != 72 {
				t.Errorf("largura %d, esperava 72", w)
			}
			if h := img.Bounds().Dy(); h < 1 {
				t.Errorf("altura %d, esperava >= 1", h)
			}
			// Miniatura tem de ser pequena — ela viaja dentro da mensagem.
			if len(got) > 16*1024 {
				t.Errorf("miniatura com %d bytes, grande demais para embutir", len(got))
			}
		})
	}
}
