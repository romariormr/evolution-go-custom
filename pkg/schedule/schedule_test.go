package schedule

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"

	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
	send_service "github.com/EvolutionAPI/evolution-go/pkg/sendMessage/service"
)

var base = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC) // 09:00 em São Paulo

func TestBuildCronNextRunInTimezone(t *testing.T) {
	r := &CreateRequest{Type: "text", Cron: "0 9 * * *", Payload: json.RawMessage(`{"number":"5511999999999","text":"bom dia"}`)}
	m, err := r.build("inst", base) // exatamente 09:00 local: a próxima é amanhã
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	if m.NextRunAt == nil || !m.NextRunAt.Equal(want) {
		t.Fatalf("next = %v, want %v", m.NextRunAt, want)
	}
	if m.Number != "5511999999999" || m.Timezone != DefaultTimezone || m.Status != StatusScheduled {
		t.Fatalf("registro inesperado: %+v", m)
	}
}

func TestBuildValidation(t *testing.T) {
	future := base.Add(time.Hour)
	past := base.Add(-time.Hour)
	cases := map[string]*CreateRequest{
		"sem horario":     {Type: "text", Payload: json.RawMessage(`{"number":"1","text":"x"}`)},
		"os dois":         {Type: "text", Cron: "* * * * *", SendAt: &future, Payload: json.RawMessage(`{"number":"1","text":"x"}`)},
		"passado":         {Type: "text", SendAt: &past, Payload: json.RawMessage(`{"number":"1","text":"x"}`)},
		"sem numero":      {Type: "text", SendAt: &future, Payload: json.RawMessage(`{"text":"x"}`)},
		"texto vazio":     {Type: "text", SendAt: &future, Payload: json.RawMessage(`{"number":"1"}`)},
		"tipo invalido":   {Type: "button", SendAt: &future, Payload: json.RawMessage(`{"number":"1"}`)},
		"cron invalido":   {Type: "text", Cron: "a b c", Payload: json.RawMessage(`{"number":"1","text":"x"}`)},
		"fuso invalido":   {Type: "text", SendAt: &future, Timezone: "Lua/Base", Payload: json.RawMessage(`{"number":"1","text":"x"}`)},
		"midia sem url":   {Type: "media", SendAt: &future, Payload: json.RawMessage(`{"number":"1","type":"image"}`)},
		"enquete 1 opcao": {Type: "poll", SendAt: &future, Payload: json.RawMessage(`{"number":"1","question":"q","options":["a"]}`)},
	}
	for name, r := range cases {
		if _, err := r.build("inst", base); err == nil {
			t.Errorf("%s: esperava erro", name)
		}
	}
	ok := &CreateRequest{Type: "Media", SendAt: &future, Payload: json.RawMessage(`{"number":"1","type":"image","url":"https://x/y.png"}`)}
	if _, err := ok.build("inst", base); err != nil {
		t.Errorf("media válida: %v", err)
	}
}

func TestAdvance(t *testing.T) {
	due := base.Add(-time.Minute)
	upd, send := advance(ScheduledMessage{NextRunAt: &due}, base)
	if !send || upd["status"] != StatusSending {
		t.Fatalf("único no horário: send=%v upd=%v", send, upd)
	}

	late := base.Add(-7 * time.Hour)
	upd, send = advance(ScheduledMessage{NextRunAt: &late}, base)
	if send || upd["status"] != StatusFailed {
		t.Fatalf("único muito atrasado não deve sair: send=%v upd=%v", send, upd)
	}

	// recorrente parado por 3 dias sai uma vez só e pula para a próxima ocorrência futura
	old := base.Add(-72 * time.Hour)
	upd, send = advance(ScheduledMessage{NextRunAt: &old, CronExpr: "0 9 * * *", Timezone: DefaultTimezone}, base)
	next, _ := upd["next_run_at"].(time.Time)
	if !send || !next.After(base) {
		t.Fatalf("recorrente: send=%v next=%v", send, next)
	}
	if _, has := upd["status"]; has {
		t.Fatalf("recorrente não muda status: %v", upd)
	}
}

type fakeSender struct {
	text *send_service.TextStruct
	err  error
}

func (f *fakeSender) SendText(d *send_service.TextStruct, _ *instance_model.Instance) (*send_service.MessageSendStruct, error) {
	f.text = d
	if f.err != nil {
		return nil, f.err
	}
	return &send_service.MessageSendStruct{Info: types.MessageInfo{ID: "MSG1"}}, nil
}
func (f *fakeSender) SendLink(*send_service.LinkStruct, *instance_model.Instance) (*send_service.MessageSendStruct, error) {
	return nil, errors.New("not used")
}
func (f *fakeSender) SendMediaUrl(*send_service.MediaStruct, *instance_model.Instance) (*send_service.MessageSendStruct, error) {
	return nil, errors.New("not used")
}
func (f *fakeSender) SendPoll(*send_service.PollStruct, *instance_model.Instance) (*send_service.MessageSendStruct, error) {
	return nil, errors.New("not used")
}
func (f *fakeSender) SendLocation(*send_service.LocationStruct, *instance_model.Instance) (*send_service.MessageSendStruct, error) {
	return nil, errors.New("not used")
}
func (f *fakeSender) SendContact(*send_service.ContactStruct, *instance_model.Instance) (*send_service.MessageSendStruct, error) {
	return nil, errors.New("not used")
}

type fakeInstances struct{}

func (fakeInstances) GetInstanceByID(id string) (*instance_model.Instance, error) {
	return &instance_model.Instance{Id: id}, nil
}

func TestDispatch(t *testing.T) {
	f := &fakeSender{}
	s := NewService(nil, f, fakeInstances{}, nil)
	id, err := s.dispatch(ScheduledMessage{InstanceId: "inst", Type: "text", Payload: `{"number":"551199","text":"oi"}`})
	if err != nil || id != "MSG1" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if f.text == nil || f.text.Number != "551199" || f.text.Text != "oi" {
		t.Fatalf("payload não repassado: %+v", f.text)
	}
	f.err = errors.New("desconectada")
	if _, err := s.dispatch(ScheduledMessage{InstanceId: "inst", Type: "text", Payload: `{"number":"1","text":"x"}`}); err == nil {
		t.Fatal("esperava erro do sender")
	}
}
