package send_service

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// Nós binários que o WhatsApp exige para renderizar botões/listas "native
// flow" (InteractiveMessage). Sem o <biz> o servidor responde erro 463.
//
// Formato completo (padrão, o mesmo que clientes oficiais de empresa e o
// WA-AKG mandam): o <biz> leva actual_actors/host_storage/privacy_mode_ts e o
// filho <quality_control>.
//
// <bot biz_bot="1"> NÃO vai mais em conversa 1:1: desde nov/2026 o WhatsApp
// descarta em silêncio a mensagem interativa que leva esse nó (o envio volta
// OK, mas botões/lista/carrossel nunca aparecem — visto no HML e no WA-AKG,
// issue #112). INTERACTIVE_NODES=bot volta a incluir, se o WhatsApp mudar.
//
// INTERACTIVE_NODES=legacy volta ao formato mínimo (só <biz> com
// <interactive><native_flow/>).
func nativeFlowNodes(recipient types.JID, flowName string) []waBinary.Node {
	if flowName == "" {
		flowName = "mixed"
	}
	interactive := waBinary.Node{
		Tag:   "interactive",
		Attrs: waBinary.Attrs{"type": "native_flow", "v": "1"},
		Content: []waBinary.Node{{
			Tag:   "native_flow",
			Attrs: waBinary.Attrs{"v": "9", "name": flowName},
		}},
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("INTERACTIVE_NODES")), "legacy") {
		return []waBinary.Node{{Tag: "biz", Content: []waBinary.Node{interactive}}}
	}
	nodes := []waBinary.Node{{
		Tag: "biz",
		Attrs: waBinary.Attrs{
			"actual_actors":   "2",
			"host_storage":    "2",
			"privacy_mode_ts": strconv.FormatInt(time.Now().Unix(), 10),
		},
		Content: []waBinary.Node{
			interactive,
			{Tag: "quality_control", Attrs: waBinary.Attrs{"source_type": "third_party"}},
		},
	}}
	withBot := strings.EqualFold(strings.TrimSpace(os.Getenv("INTERACTIVE_NODES")), "bot")
	if withBot && (recipient.Server == types.DefaultUserServer || recipient.Server == types.HiddenUserServer) {
		nodes = append(nodes, waBinary.Node{Tag: "bot", Attrs: waBinary.Attrs{"biz_bot": "1"}})
	}
	return nodes
}

// flowNameOf: com um único botão o <native_flow> leva o nome dele (ex.:
// "single_select" na lista); com vários, "mixed".
func flowNameOf(msg *waE2E.Message) string {
	btns := msg.GetInteractiveMessage().GetNativeFlowMessage().GetButtons()
	if len(btns) == 1 && btns[0].GetName() != "" {
		return btns[0].GetName()
	}
	return "mixed"
}

// nativeFlowList monta a lista como InteractiveMessage "single_select" (a
// gaveta de opções dos clientes atuais), alternativa ao ListMessage antigo.
func nativeFlowList(data *ListStruct, buttonText string) (*waE2E.Message, error) {
	type row struct {
		Header      string `json:"header"`
		Title       string `json:"title"`
		Description string `json:"description"`
		ID          string `json:"id"`
	}
	type section struct {
		Title string `json:"title"`
		Rows  []row  `json:"rows"`
	}
	params := struct {
		Title    string    `json:"title"`
		Sections []section `json:"sections"`
	}{Title: buttonText}
	for si, sec := range data.Sections {
		out := section{Title: sec.Title}
		for ri, r := range sec.Rows {
			id := r.RowId
			if id == "" {
				id = "row_" + strconv.Itoa(si) + "_" + strconv.Itoa(ri)
			}
			out.Rows = append(out.Rows, row{Title: r.Title, Description: r.Description, ID: id})
		}
		params.Sections = append(params.Sections, out)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	im := &waE2E.InteractiveMessage{
		Body:   &waE2E.InteractiveMessage_Body{Text: proto.String(data.Description)},
		Footer: &waE2E.InteractiveMessage_Footer{Text: proto.String(data.FooterText)},
		InteractiveMessage: &waE2E.InteractiveMessage_NativeFlowMessage_{
			NativeFlowMessage: &waE2E.InteractiveMessage_NativeFlowMessage{
				Buttons: []*waE2E.InteractiveMessage_NativeFlowMessage_NativeFlowButton{{
					Name:             proto.String("single_select"),
					ButtonParamsJSON: proto.String(string(raw)),
				}},
				MessageVersion: proto.Int32(1),
			},
		},
	}
	if data.Title != "" {
		im.Header = &waE2E.InteractiveMessage_Header{Title: proto.String(data.Title), HasMediaAttachment: proto.Bool(false)}
	}
	return &waE2E.Message{InteractiveMessage: im}, nil
}
