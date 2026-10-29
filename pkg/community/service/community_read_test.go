package community_service

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestIsAdmin(t *testing.T) {
	me := types.NewJID("5511999990001", types.DefaultUserServer)
	meLID := types.NewJID("123456789", types.HiddenUserServer)
	info := &types.GroupInfo{Participants: []types.GroupParticipant{
		{JID: types.NewJID("777", types.HiddenUserServer), IsAdmin: true},
		{JID: meLID, PhoneNumber: me, IsAdmin: false},
	}}
	if isAdmin(info, me, meLID) {
		t.Fatal("instância não é admin")
	}
	info.Participants[1].IsSuperAdmin = true
	if !isAdmin(info, me, meLID) {
		t.Fatal("instância é superadmin pelo LID")
	}
	info.Participants[1] = types.GroupParticipant{JID: types.NewJID("999", types.HiddenUserServer), PhoneNumber: me, IsAdmin: true}
	if !isAdmin(info, me, types.JID{}) {
		t.Fatal("deveria reconhecer pelo número")
	}
}

func TestParseCommunityJID(t *testing.T) {
	c := &communityService{}
	j, err := c.parseCommunityJID("120363000000000001")
	if err != nil || j.Server != types.GroupServer {
		t.Fatalf("sem servidor deve virar @g.us: %v %v", j, err)
	}
	if _, err := c.parseCommunityJID(" "); err == nil {
		t.Fatal("vazio deve dar erro")
	}
}
