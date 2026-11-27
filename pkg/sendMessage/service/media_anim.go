package send_service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gabriel-vasile/mimetype"
)

// GIF, figurinha animada e figurinha com som. Tudo passa pelo ffmpeg que já
// vem na imagem (apk add ffmpeg). Arquivos temporários são usados porque MP4
// precisa de "seek" para gravar o índice (moov) no começo do arquivo.

const (
	ffmpegTimeout       = 90 * time.Second
	animStickerMaxBytes = 500 << 10 // limite do WhatsApp para figurinha animada
	animStickerMaxSecs  = "8"
)

// markGif: type "gif" é enviado como vídeo com GifPlayback=true.
func markGif(data *MediaStruct) {
	if strings.EqualFold(strings.TrimSpace(data.Type), "gif") {
		data.Type = "video"
		data.asGif = true
	}
}

// runFFmpeg grava in num arquivo temporário, roda o ffmpeg e devolve a saída.
func runFFmpeg(in []byte, inExt, outExt string, args ...string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "evogo-ffmpeg-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	src := filepath.Join(dir, "in"+inExt)
	dst := filepath.Join(dir, "out"+outExt)
	if err := os.WriteFile(src, in, 0o600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), ffmpegTimeout)
	defer cancel()
	full := append([]string{"-hide_banner", "-loglevel", "error", "-y", "-i", src}, args...)
	full = append(full, dst)
	cmd := exec.CommandContext(ctx, "ffmpeg", full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("ffmpeg falhou: %v %s", err, msg)
	}
	return os.ReadFile(dst)
}

func extFor(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/gif"):
		return ".gif"
	case strings.HasPrefix(mime, "image/webp"):
		return ".webp"
	case strings.HasPrefix(mime, "video/webm"):
		return ".webm"
	case strings.HasPrefix(mime, "video/quicktime"):
		return ".mov"
	case strings.HasPrefix(mime, "video/"):
		return ".mp4"
	}
	return ".bin"
}

// prepareGif devolve um MP4 pronto para GifPlayback. MP4 passa direto; GIF
// (ou WebM/MOV) é convertido, sem áudio, com dimensões pares (exigência do H.264).
func prepareGif(data []byte, mime string) ([]byte, string, error) {
	if strings.HasPrefix(mime, "video/mp4") {
		return data, "video/mp4", nil
	}
	if !strings.HasPrefix(mime, "image/gif") && !strings.HasPrefix(mime, "video/") {
		return nil, "", fmt.Errorf("GIF precisa ser .gif ou .mp4 (recebido %s)", mime)
	}
	out, err := runFFmpeg(data, extFor(mime), ".mp4",
		"-movflags", "+faststart", "-pix_fmt", "yuv420p",
		"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2", "-c:v", "libx264", "-preset", "veryfast", "-crf", "28", "-an")
	if err != nil {
		return nil, "", err
	}
	return out, "video/mp4", nil
}

// isAnimatedSource: GIF com mais de um quadro, vídeo, ou WebP animado.
func isAnimatedSource(raw []byte) bool {
	m := mimetype.Detect(raw).String()
	switch {
	case strings.HasPrefix(m, "video/"):
		return true
	case strings.HasPrefix(m, "image/gif"):
		return bytes.Count(raw, []byte{0x21, 0xF9, 0x04}) > 1 // >1 Graphic Control Extension
	case strings.HasPrefix(m, "image/webp"):
		return len(raw) > 21 && string(raw[12:16]) == "VP8X" && raw[20]&0x02 != 0
	}
	return false
}

// buildAnimatedSticker: GIF/MP4/WebM/WebP animado -> WebP animado 512x512,
// fundo transparente, até 8s, reduzindo qualidade/fps até caber em 500 KB.
func buildAnimatedSticker(raw []byte, meta stickerMeta) ([]byte, error) {
	ext := extFor(mimetype.Detect(raw).String())
	var out []byte
	var err error
	for _, try := range []struct{ fps, q string }{{"15", "60"}, {"12", "45"}, {"10", "30"}, {"8", "20"}} {
		vf := "fps=" + try.fps + ",scale=512:512:force_original_aspect_ratio=decrease:flags=lanczos," +
			"format=rgba,pad=512:512:(ow-iw)/2:(oh-ih)/2:color=0x00000000"
		out, err = runFFmpeg(raw, ext, ".webp", "-t", animStickerMaxSecs, "-vf", vf,
			"-c:v", "libwebp_anim", "-lossless", "0", "-q:v", try.q, "-loop", "0", "-an", "-vsync", "0")
		if err != nil {
			return nil, err
		}
		if len(out) <= animStickerMaxBytes {
			break
		}
	}
	if len(out) > animStickerMaxBytes {
		return nil, fmt.Errorf("figurinha animada ficou com %d KB (máximo 500 KB): use um trecho menor", len(out)>>10)
	}
	if meta.empty() {
		return out, nil
	}
	return addWebpExif(out, stickerExif(meta), stickerSize, stickerSize)
}

