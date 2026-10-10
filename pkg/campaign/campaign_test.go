package campaign

import (
	"errors"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
	send_service "github.com/EvolutionAPI/evolution-go/pkg/sendMessage/service"
)

var now = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func TestBuildDedupAndDefaults(t *testing.T) {
	req := &CreateRequest{
		Message:    Message{Text: "Oi {{name}}"},
		Recipients: []RecipientInput{{Number: "+55 (11) 99999-0001", Name: "Ana"}, {Number: "5511999990001"}},
		Numbers:    "5511999990002\n5511999990002, 123;5511999990003@s.whatsapp.net",
	}
	c, rs, err := req.build("inst", now)
	if err != nil {
		t.Fatal(err)
	}
	if c.Type != "text" || c.Status != StatusDraft || c.DelayMinMs != defaultDelayMin || c.DelayMaxMs != defaultDelayMax {
		t.Fatalf("defaults: %+v", c)
	}
	if len(rs) != 3 || c.Total != 3 {
		t.Fatalf("esperava 3 destinatários (dedup + inválido fora), veio %d: %+v", len(rs), rs)
	}
	if rs[0].Number != "5511999990001" || rs[0].Name != "Ana" {
		t.Fatalf("normalização: %+v", rs[0])
	}
	if rs[2].Number != "5511999990003@s.whatsapp.net" {
		t.Fatalf("JID deve ficar como veio: %q", rs[2].Number)
	}
}

func TestBuildValidation(t *testing.T) {
	bad := []*CreateRequest{
		{Message: Message{Text: ""}, Numbers: "5511999990001"},
		{Type: "media", Message: Message{MediaUrl: "https://x/y.png", MediaType: "gif"}, Numbers: "5511999990001"},
		{Type: "sticker", Message: Message{Text: "x"}, Numbers: "5511999990001"},
		{Message: Message{Text: "x"}, Numbers: "abc"},
	}
	for i, r := range bad {
		if _, _, err := r.build("inst", now); err == nil {
			t.Errorf("caso %d: esperava erro", i)
		}
	}
	r := &CreateRequest{Message: Message{Text: "x"}, Numbers: "5511999990001", DelayMinMs: 9000, DelayMaxMs: 1000, Start: true}
	c, _, err := r.build("inst", now)
	if err != nil {
		t.Fatal(err)
	}
	if c.DelayMaxMs != 9000 || c.Status != StatusRunning {
		t.Fatalf("max < min deve virar min; start deve rodar: %+v", c)
	}
}

func TestRender(t *testing.T) {
	r := &Recipient{Name: "Ana", Vars: `{"pedido":"123"}`}
	if got := render("Oi {{name}}, pedido {{pedido}}", r); got != "Oi Ana, pedido 123" {
		t.Errorf("render: %q", got)
	}
	if got := render("Oi {{name}}, tudo bem?", &Recipient{}); got != "Oi, tudo bem?" {
		t.Errorf("sem nome: %q", got)
	}
}

func TestOptOut(t *testing.T) {
	for _, s := range []string{"SAIR", " parar ", "Stop!", "descadastrar"} {
		if !isOptOutText(s) {
			t.Errorf("%q deveria ser descadastro", s)
		}
	}
	for _, s := range []string{"quero sair mais cedo", "", "ok"} {
		if isOptOutText(s) {
			t.Errorf("%q não é descadastro", s)
		}
	}
	if optOutKey("5511999990001@s.whatsapp.net") != "5511999990001" || optOutKey("5511999990001:12@s.whatsapp.net") != "5511999990001" {
		t.Error("optOutKey deve tirar servidor e device")
	}
}

type fakeSender struct {
	text  *send_service.TextStruct
	media *send_service.MediaStruct
}

func (f *fakeSender) SendText(d *send_service.TextStruct, _ *instance_model.Instance) (*send_service.MessageSendStruct, error) {
	f.text = d
	return &send_service.MessageSendStruct{Info: types.MessageInfo{ID: "T1"}}, nil
}
func (f *fakeSender) SendMediaUrl(d *send_service.MediaStruct, _ *instance_model.Instance) (*send_service.MessageSendStruct, error) {
	f.media = d
	return &send_service.MessageSendStruct{Info: types.MessageInfo{ID: "M1"}}, nil
}

type fakeInstances struct{ err error }

func (f fakeInstances) GetInstanceByID(id string) (*instance_model.Instance, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &instance_model.Instance{Id: id}, nil
}

func TestSend(t *testing.T) {
	f := &fakeSender{}
	s := NewService(nil, f, fakeInstances{}, nil)
	r := &Recipient{Number: "5511999990001", Name: "Ana"}

	id, err := s.send(&Campaign{InstanceId: "i", Type: "text"}, &Message{Text: "Oi {{name}}"}, r)
	if err != nil || id != "T1" || f.text.Text != "Oi Ana" || f.text.Number != r.Number {
		t.Fatalf("texto: id=%q err=%v %+v", id, err, f.text)
	}
	id, err = s.send(&Campaign{InstanceId: "i", Type: "media"}, &Message{MediaUrl: "https://x/y.png", MediaType: "image", Caption: "{{name}}!"}, r)
	if err != nil || id != "M1" || f.media.Caption != "Ana!" || f.media.Type != "image" {
		t.Fatalf("mídia: id=%q err=%v %+v", id, err, f.media)
	}
	s = NewService(nil, f, fakeInstances{err: errors.New("x")}, nil)
	if _, err := s.send(&Campaign{InstanceId: "i", Type: "text"}, &Message{Text: "a"}, r); err == nil {
		t.Fatal("instância inexistente deve dar erro")
	}
}
