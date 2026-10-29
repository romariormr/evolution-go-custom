package send_service

import (
	"encoding/json"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestValidateAlbum(t *testing.T) {
	ok := &AlbumStruct{Number: "5511", Items: []AlbumItem{{Url: "https://x/a.jpg", Type: "image"}, {Url: "https://x/b.mp4", Type: "video"}}}
	if err := validateAlbum(ok); err != nil {
		t.Fatalf("álbum válido recusado: %v", err)
	}
	bad := []*AlbumStruct{
		{Items: ok.Items},
		{Number: "5511", Items: ok.Items[:1]},
		{Number: "5511", Items: []AlbumItem{{Url: "https://x/a.jpg", Type: "image"}, {Url: "", Type: "image"}}},
		{Number: "5511", Items: []AlbumItem{{Url: "https://x/a.jpg", Type: "image"}, {Url: "https://x/a.pdf", Type: "document"}}},
		{Number: "5511", Items: make([]AlbumItem, maxAlbumItems+1)},
	}
	for i, a := range bad {
		if err := validateAlbum(a); err == nil {
			t.Errorf("caso %d: esperava erro", i)
		}
	}
	img, vid := albumCounts(ok.Items)
	if img != 1 || vid != 1 {
		t.Fatalf("contagem errada: %d imagens, %d vídeos", img, vid)
	}
}

func TestNativeFlowNodes(t *testing.T) {
	dm := types.NewJID("5511", types.DefaultUserServer)
	group := types.NewJID("123", types.GroupServer)

	nodes := nativeFlowNodes(dm, "")
	if len(nodes) != 2 || nodes[0].Tag != "biz" || nodes[1].Tag != "bot" || nodes[1].Attrs["biz_bot"] != "1" {
		t.Fatalf("1:1 deve ter <biz> e <bot biz_bot=1>: %+v", nodes)
	}
	if nodes[0].Attrs["actual_actors"] != "2" {
		t.Fatalf("<biz> sem atributos completos: %+v", nodes[0].Attrs)
	}
	if len(nativeFlowNodes(group, "mixed")) != 1 {
		t.Fatal("grupo não leva <bot>")
	}
	t.Setenv("INTERACTIVE_NODES", "legacy")
	legacy := nativeFlowNodes(dm, "mixed")
	if len(legacy) != 1 || len(legacy[0].Attrs) != 0 {
		t.Fatalf("modo legacy deve ser só o <biz> mínimo: %+v", legacy)
	}
}

func TestNativeFlowList(t *testing.T) {
	data := &ListStruct{Title: "Planos", Description: "Escolha", FooterText: "TI",
		Sections: []Section{{Title: "Sec", Rows: []Row{{Title: "A", Description: "a"}, {Title: "B", RowId: "b"}}}}}
	msg, err := nativeFlowList(data, "Abrir")
	if err != nil {
		t.Fatal(err)
	}
	btn := msg.GetInteractiveMessage().GetNativeFlowMessage().GetButtons()[0]
	if btn.GetName() != "single_select" {
		t.Fatalf("botão deveria ser single_select: %s", btn.GetName())
	}
	var params struct {
		Title    string `json:"title"`
		Sections []struct {
			Rows []struct{ ID, Title string } `json:"rows"`
		} `json:"sections"`
	}
	if err := json.Unmarshal([]byte(btn.GetButtonParamsJSON()), &params); err != nil {
		t.Fatal(err)
	}
	if params.Title != "Abrir" || params.Sections[0].Rows[0].ID != "row_0_0" || params.Sections[0].Rows[1].ID != "b" {
		t.Fatalf("parâmetros inesperados: %+v", params)
	}
}
