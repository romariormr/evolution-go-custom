package send_service

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // figurinha a partir de GIF (usa o primeiro quadro)
	"strings"

	"github.com/EvolutionAPI/evolution-go/pkg/safefetch"
	"github.com/chai2010/webp"
	"github.com/google/uuid"
	xdraw "golang.org/x/image/draw"
)

// Figurinha estática do WhatsApp: WebP 512x512, de preferência até 100 KB,
// com metadados de pacote/autor num chunk EXIF.
const (
	stickerSize       = 512
	stickerMaxBytes   = 100 << 10
	stickerMaxSource  = 10 << 20 // base64 decodificado
	stickerMaxPixels  = 40_000_000
	stickerMaxEmojis  = 3
	stickerMaxMetaLen = 128
)

var stickerQualities = []float32{80, 65, 50, 35}

// loadStickerSource aceita URL http(s), data URI (data:image/...;base64,...) ou base64 puro.
func loadStickerSource(src string) ([]byte, error) {
	src = strings.TrimSpace(src)
	switch {
	case strings.HasPrefix(src, "http://"), strings.HasPrefix(src, "https://"):
		b, err := safefetch.Fetch(src)
		if err != nil {
			return nil, fmt.Errorf("download da imagem falhou: %v", err)
		}
		return b, nil
	case strings.HasPrefix(src, "data:"):
		i := strings.Index(src, ",")
		if i < 0 || !strings.Contains(src[:i], ";base64") {
			return nil, errors.New("data URI inválida: use data:image/...;base64,...")
		}
		src = src[i+1:]
	}
	if base64.StdEncoding.DecodedLen(len(src)) > stickerMaxSource {
		return nil, fmt.Errorf("imagem muito grande (máximo %d MB)", stickerMaxSource>>20)
	}
	b, err := base64.StdEncoding.DecodeString(strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' {
			return -1
		}
		return r
	}, src))
	if err != nil {
		return nil, errors.New("sticker deve ser uma URL http(s), data URI ou base64 válido")
	}
	return b, nil
}

// fitSticker centraliza a imagem num quadrado 512x512 transparente, sem distorcer.
func fitSticker(src image.Image) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, stickerSize, stickerSize))
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return dst
	}
	nw, nh := stickerSize, stickerSize
	if w >= h {
		nh = h * stickerSize / w
	} else {
		nw = w * stickerSize / h
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	x0, y0 := (stickerSize-nw)/2, (stickerSize-nh)/2
	xdraw.CatmullRom.Scale(dst, image.Rect(x0, y0, x0+nw, y0+nh), src, b, xdraw.Over, nil)
	return dst
}

type stickerMeta struct {
	PackName      string
	PackPublisher string
	Emojis        []string
}

func (m stickerMeta) empty() bool {
	return m.PackName == "" && m.PackPublisher == "" && len(m.Emojis) == 0
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// stickerExif monta o payload EXIF que o WhatsApp lê (pacote, autor, emojis).
func stickerExif(m stickerMeta) []byte {
	emojis := make([]string, 0, stickerMaxEmojis)
	for _, e := range m.Emojis {
		if e = strings.TrimSpace(e); e != "" && len(emojis) < stickerMaxEmojis {
			emojis = append(emojis, e)
		}
	}
	js, _ := json.Marshal(map[string]any{
		"sticker-pack-id":        uuid.NewString(),
		"sticker-pack-name":      clip(m.PackName, stickerMaxMetaLen),
		"sticker-pack-publisher": clip(m.PackPublisher, stickerMaxMetaLen),
		"emojis":                 emojis,
	})
	// TIFF little-endian, 1 entrada (tag 0x5741, tipo UNDEFINED) apontando para o JSON no offset 22.
	head := []byte{0x49, 0x49, 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00, 0x01, 0x00, 0x41, 0x57, 0x07, 0x00,
		0, 0, 0, 0, 0x16, 0x00, 0x00, 0x00}
	binary.LittleEndian.PutUint32(head[14:18], uint32(len(js)))
	return append(head, js...)
}

type riffChunk struct {
	id   string
	data []byte
}

// addWebpExif insere um chunk EXIF no WebP, convertendo para o formato estendido (VP8X) se preciso.
func addWebpExif(src []byte, exif []byte, width, height int) ([]byte, error) {
	if len(src) < 12 || string(src[0:4]) != "RIFF" || string(src[8:12]) != "WEBP" {
		return nil, errors.New("webp inválido")
	}
	var chunks []riffChunk
	for p := 12; p+8 <= len(src); {
		id := string(src[p : p+4])
		size := int(binary.LittleEndian.Uint32(src[p+4 : p+8]))
		if p+8+size > len(src) {
			return nil, errors.New("webp truncado")
		}
		if id != "EXIF" {
			chunks = append(chunks, riffChunk{id, src[p+8 : p+8+size]})
		}
		p += 8 + size + size%2
	}
	if len(chunks) == 0 {
		return nil, errors.New("webp sem dados")
	}
	if chunks[0].id != "VP8X" {
		vp8x := make([]byte, 10)
		if chunks[0].id == "VP8L" {
			vp8x[0] |= 0x10 // VP8L pode ter alfa
		}
		putUint24(vp8x[4:7], width-1)
		putUint24(vp8x[7:10], height-1)
		chunks = append([]riffChunk{{"VP8X", vp8x}}, chunks...)
	} else {
		chunks[0].data = append([]byte(nil), chunks[0].data...)
	}
	chunks[0].data[0] |= 0x08 // flag EXIF
	chunks = append(chunks, riffChunk{"EXIF", exif})

	var body bytes.Buffer
	body.WriteString("WEBP")
	for _, c := range chunks {
		body.WriteString(c.id)
		_ = binary.Write(&body, binary.LittleEndian, uint32(len(c.data)))
		body.Write(c.data)
		if len(c.data)%2 == 1 {
			body.WriteByte(0)
		}
	}
	out := make([]byte, 0, 8+body.Len())
	out = append(out, "RIFF"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(body.Len()))
	return append(out, body.Bytes()...), nil
}

func putUint24(b []byte, v int) {
	b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16)
}

// buildSticker: imagem (PNG/JPG/WebP/GIF) -> WebP 512x512 com metadados.
func buildSticker(raw []byte, meta stickerMeta) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("formato de imagem não suportado (use PNG, JPG, WebP ou GIF): %v", err)
	}
	if cfg.Width*cfg.Height > stickerMaxPixels {
		return nil, fmt.Errorf("imagem grande demais (%dx%d)", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("falha ao ler a imagem: %v", err)
	}
	canvas := fitSticker(img)

	var out []byte
	for _, q := range stickerQualities {
		var buf bytes.Buffer
		if err := webp.Encode(&buf, canvas, &webp.Options{Quality: q}); err != nil {
			return nil, fmt.Errorf("falha ao gerar WebP: %v", err)
		}
		out = buf.Bytes()
		if len(out) <= stickerMaxBytes {
			break
		}
	}
	if meta.empty() {
		return out, nil
	}
	return addWebpExif(out, stickerExif(meta), stickerSize, stickerSize)
}
