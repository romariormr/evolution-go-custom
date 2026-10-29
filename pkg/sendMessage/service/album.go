package send_service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
)

// Álbum: várias fotos/vídeos agrupados numa única bolha no WhatsApp.
//
// O protocolo é uma mensagem "âncora" (AlbumMessage, com a quantidade
// esperada de imagens e vídeos) seguida de cada mídia marcada como parte do
// álbum (MessageAssociation MEDIA_ALBUM apontando para a âncora). Âncora e
// mídias compartilham o mesmo MessageSecret.

const (
	maxAlbumItems = 30
	minAlbumItems = 2
)

type AlbumItem struct {
	// Url: http(s) ou data URI (data:image/...;base64,...).
	Url     string `json:"url"`
	Type    string `json:"type" enums:"image,video"`
	Caption string `json:"caption,omitempty"`
}

type AlbumStruct struct {
	Number    string       `json:"number"`
	Items     []AlbumItem  `json:"items"`
	Caption   string       `json:"caption,omitempty"` // legenda da 1ª mídia, se ela não tiver uma
	Delay     int32        `json:"delay"`
	FormatJid *bool        `json:"formatJid,omitempty"`
	Quoted    QuotedStruct `json:"quoted"`
}

type AlbumSendResult struct {
	Album  *MessageSendStruct   `json:"album"`
	Items  []*MessageSendStruct `json:"items"`
	Failed []string             `json:"failed,omitempty"`
}

func validateAlbum(data *AlbumStruct) error {
	if strings.TrimSpace(data.Number) == "" {
		return errors.New("number é obrigatório")
	}
	if len(data.Items) < minAlbumItems {
		return fmt.Errorf("o álbum precisa de pelo menos %d itens", minAlbumItems)
	}
	if len(data.Items) > maxAlbumItems {
		return fmt.Errorf("o álbum aceita no máximo %d itens", maxAlbumItems)
	}
	for i, it := range data.Items {
		if strings.TrimSpace(it.Url) == "" {
			return fmt.Errorf("items[%d].url é obrigatório", i)
		}
		if it.Type != "image" && it.Type != "video" {
			return fmt.Errorf("items[%d].type deve ser image ou video", i)
		}
	}
	return nil
}

// buildAlbumMedia baixa e sobe uma mídia do álbum e devolve a mensagem pronta.
func buildAlbumMedia(client *whatsmeow.Client, it AlbumItem, caption string) (*waE2E.Message, string, error) {
	fileData, err := fetchImageBytes(it.Url)
	if err != nil {
		return nil, "", err
	}
	mimeType := mimetype.Detect(fileData).String()
	switch it.Type {
	case "image":
		if mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" {
			return nil, "", fmt.Errorf("imagem em formato não aceito: %s", mimeType)
		}
		if mimeType == "image/webp" {
			mimeType = "image/jpeg"
		}
		up, err := client.Upload(context.Background(), fileData, whatsmeow.MediaImage)
		if err != nil {
			return nil, "", err
		}
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			Caption:       proto.String(caption),
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(mimeType),
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(fileData))),
			JPEGThumbnail: gerarThumbnail(fileData),
		}}, "ImageMessage", nil
	default:
		if mimeType != "video/mp4" {
			return nil, "", fmt.Errorf("vídeo em formato não aceito: %s (use mp4)", mimeType)
		}
		up, err := client.Upload(context.Background(), fileData, whatsmeow.MediaVideo)
		if err != nil {
			return nil, "", err
		}
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			Caption:       proto.String(caption),
			URL:           proto.String(up.URL),
			DirectPath:    proto.String(up.DirectPath),
			MediaKey:      up.MediaKey,
			Mimetype:      proto.String(mimeType),
			FileEncSHA256: up.FileEncSHA256,
			FileSHA256:    up.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(fileData))),
		}}, "VideoMessage", nil
	}
}

func albumCounts(items []AlbumItem) (images, videos uint32) {
	for _, it := range items {
		if it.Type == "video" {
			videos++
		} else {
			images++
		}
	}
	return
}

func (s *sendService) SendAlbum(data *AlbumStruct, instance *instance_model.Instance) (*AlbumSendResult, error) {
	if err := validateAlbum(data); err != nil {
		return nil, err
	}
	client, err := s.ensureClientConnectedWithRetry(instance.Id, 2)
	if err != nil {
		return nil, err
	}

	// Sobe todas as mídias antes de mandar a âncora: se uma URL falhar, nada sai.
	type prepared struct {
		msg   *waE2E.Message
		mtype string
	}
	items := make([]prepared, 0, len(data.Items))
	for i, it := range data.Items {
		caption := it.Caption
		if i == 0 && caption == "" {
			caption = data.Caption
		}
		msg, mtype, err := buildAlbumMedia(client, it, caption)
		if err != nil {
			return nil, fmt.Errorf("items[%d]: %v", i, err)
		}
		items = append(items, prepared{msg, mtype})
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	images, videos := albumCounts(data.Items)
	anchor := &waE2E.Message{
		AlbumMessage: &waE2E.AlbumMessage{
			ExpectedImageCount: proto.Uint32(images),
			ExpectedVideoCount: proto.Uint32(videos),
		},
		MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: secret},
	}
	anchorSent, err := s.SendMessage(instance, anchor, "AlbumMessage", &SendDataStruct{
		Number:    data.Number,
		Delay:     data.Delay,
		FormatJid: data.FormatJid,
		Quoted:    data.Quoted,
	})
	if err != nil {
		return nil, fmt.Errorf("falha ao enviar o álbum: %v", err)
	}

	result := &AlbumSendResult{Album: anchorSent}
	chat := anchorSent.Info.Chat.String()
	noFormat := false
	for i, p := range items {
		p.msg.MessageContextInfo = &waE2E.MessageContextInfo{
			MessageSecret: secret,
			MessageAssociation: &waE2E.MessageAssociation{
				AssociationType: waE2E.MessageAssociation_MEDIA_ALBUM.Enum(),
				ParentMessageKey: &waCommon.MessageKey{
					RemoteJID: proto.String(chat),
					FromMe:    proto.Bool(true),
					ID:        proto.String(anchorSent.Info.ID),
				},
			},
		}
		sent, err := s.SendMessage(instance, p.msg, p.mtype, &SendDataStruct{Number: chat, FormatJid: &noFormat})
		if err != nil {
			result.Failed = append(result.Failed, fmt.Sprintf("items[%d]: %v", i, err))
			continue
		}
		result.Items = append(result.Items, sent)
	}
	if len(result.Items) == 0 {
		return result, errors.New("nenhuma mídia do álbum foi enviada: " + strings.Join(result.Failed, "; "))
	}
	return result, nil
}
