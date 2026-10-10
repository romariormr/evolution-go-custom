package history

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func TestDescribe(t *testing.T) {
	cases := []struct {
		name                    string
		msg                     *waE2E.Message
		typ, content, mime, quo string
	}{
		{"texto", &waE2E.Message{Conversation: proto.String("oi")}, "text", "oi", "", ""},
		{"resposta", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("ok"), ContextInfo: &waE2E.ContextInfo{StanzaID: proto.String("ABC")}}}, "text", "ok", "", "ABC"},
		{"imagem", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("foto"), Mimetype: proto.String("image/jpeg")}}, "image", "foto", "image/jpeg", ""},
		{"documento com legenda", &waE2E.Message{DocumentWithCaptionMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
			DocumentMessage: &waE2E.DocumentMessage{Caption: proto.String("nf"), Mimetype: proto.String("application/pdf"), FileName: proto.String("nf.pdf")}}}},
			"document", "nf", "application/pdf", ""},
		{"temporária", &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{Conversation: proto.String("some")}}}, "text", "some", "", ""},
		{"enquete", &waE2E.Message{PollCreationMessageV3: &waE2E.PollCreationMessage{Name: proto.String("Qual?")}}, "poll", "Qual?", "", ""},
		{"nil", nil, "unknown", "", "", ""},
	}
	for _, c := range cases {
		typ, content, mime, _, quoted := describe(c.msg)
		if typ != c.typ || content != c.content || mime != c.mime || quoted != c.quo {
			t.Errorf("%s: got (%s,%q,%s,%s)", c.name, typ, content, mime, quoted)
		}
	}
}

func TestReceiptStatusAndRank(t *testing.T) {
	if receiptStatus(types.ReceiptTypeDelivered) != StatusDelivered ||
		receiptStatus(types.ReceiptTypeRead) != StatusRead ||
		receiptStatus(types.ReceiptTypePlayed) != StatusPlayed ||
		receiptStatus(types.ReceiptTypeReadSelf) != "" {
		t.Fatal("mapeamento de recibos errado")
	}
	if !(statusRank[StatusSent] < statusRank[StatusDelivered] && statusRank[StatusDelivered] < statusRank[StatusRead] && statusRank[StatusRead] < statusRank[StatusPlayed]) {
		t.Fatal("ordem de status errada")
	}
}

func TestNormalizeChat(t *testing.T) {
	if got := normalizeChat("5511999990001"); len(got) != 3 || got[0] != "5511999990001@s.whatsapp.net" {
		t.Fatalf("número: %v", got)
	}
	if got := normalizeChat("123@g.us"); len(got) != 1 || got[0] != "123@g.us" {
		t.Fatalf("jid: %v", got)
	}
	if normalizeChat(" ") != nil {
		t.Fatal("vazio deve ser nil")
	}
	if escapeLike(`50%_a\b`) != `50\%\_a\\b` {
		t.Fatalf("escapeLike: %s", escapeLike(`50%_a\b`))
	}
}

func TestSkipChat(t *testing.T) {
	if !skipChat(types.StatusBroadcastJID) {
		t.Fatal("status deve ser ignorado")
	}
	if skipChat(types.NewJID("5511", types.DefaultUserServer)) {
		t.Fatal("conversa normal não deve ser ignorada")
	}
}
