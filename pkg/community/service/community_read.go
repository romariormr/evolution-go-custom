package community_service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"

	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
	"github.com/EvolutionAPI/evolution-go/pkg/utils"
)

// Consulta de comunidades: listar as que a instância participa, ver uma
// comunidade com seus grupos (incluindo o grupo de avisos) e os participantes
// de todos os grupos dela. Para mandar aviso, envie para announcementJid com
// os endpoints /send/* normais.

type CommunityJIDStruct struct {
	CommunityJID string `json:"communityJid"`
}

type CommunitySummary struct {
	JID              string    `json:"jid"`
	Name             string    `json:"name"`
	Description      string    `json:"description,omitempty"`
	ParticipantCount int       `json:"participantCount"`
	IsAdmin          bool      `json:"isAdmin"`
	Created          time.Time `json:"created,omitempty"`
}

type SubGroup struct {
	JID            string `json:"jid"`
	Name           string `json:"name"`
	IsAnnouncement bool   `json:"isAnnouncement"`
}

type CommunityDetail struct {
	CommunitySummary
	AnnouncementJID string     `json:"announcementJid,omitempty"`
	SubGroups       []SubGroup `json:"subGroups"`
	OwnerJID        string     `json:"ownerJid,omitempty"`
}

func (c *communityService) parseCommunityJID(raw string) (types.JID, error) {
	if strings.TrimSpace(raw) == "" {
		return types.JID{}, errors.New("communityJid é obrigatório")
	}
	if !strings.Contains(raw, "@") {
		raw += "@" + types.GroupServer
	}
	jid, ok := utils.ParseJID(raw)
	if !ok {
		return types.JID{}, errors.New("communityJid inválido")
	}
	return jid, nil
}

func isAdmin(info *types.GroupInfo, me types.JID, meLID types.JID) bool {
	for _, p := range info.Participants {
		if !(p.IsAdmin || p.IsSuperAdmin) {
			continue
		}
		for _, j := range []types.JID{p.JID, p.PhoneNumber, p.LID} {
			if j.User == "" {
				continue
			}
			if (me.User != "" && j.User == me.User) || (meLID.User != "" && j.User == meLID.User) {
				return true
			}
		}
	}
	return false
}

func summarize(info *types.GroupInfo, me, meLID types.JID) CommunitySummary {
	count := info.ParticipantCount
	if count == 0 {
		count = len(info.Participants)
	}
	return CommunitySummary{
		JID:              info.JID.String(),
		Name:             info.Name,
		Description:      info.Topic,
		ParticipantCount: count,
		IsAdmin:          isAdmin(info, me, meLID),
		Created:          info.GroupCreated,
	}
}

// ListCommunities devolve as comunidades das quais a instância participa.
func (c *communityService) ListCommunities(instance *instance_model.Instance) ([]CommunitySummary, error) {
	client, err := c.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}
	groups, err := client.GetJoinedGroups(context.Background())
	if err != nil {
		return nil, err
	}
	me, meLID := ownJIDs(client)
	out := []CommunitySummary{}
	for _, g := range groups {
		if g.IsParent {
			out = append(out, summarize(g, me, meLID))
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// CommunityInfo devolve a comunidade com os grupos vinculados.
func (c *communityService) CommunityInfo(data *CommunityJIDStruct, instance *instance_model.Instance) (*CommunityDetail, error) {
	client, err := c.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}
	jid, err := c.parseCommunityJID(data.CommunityJID)
	if err != nil {
		return nil, err
	}
	info, err := client.GetGroupInfo(context.Background(), jid)
	if err != nil {
		return nil, err
	}
	if !info.IsParent {
		return nil, errors.New("esse JID é de um grupo comum, não de uma comunidade")
	}
	me, meLID := ownJIDs(client)
	detail := &CommunityDetail{CommunitySummary: summarize(info, me, meLID), SubGroups: []SubGroup{}}
	if !info.OwnerJID.IsEmpty() {
		detail.OwnerJID = info.OwnerJID.String()
	}
	subs, err := client.GetSubGroups(context.Background(), jid)
	if err != nil {
		return nil, err
	}
	for _, s := range subs {
		sg := SubGroup{JID: s.JID.String(), Name: s.Name, IsAnnouncement: s.IsDefaultSubGroup}
		if sg.IsAnnouncement {
			detail.AnnouncementJID = sg.JID
		}
		detail.SubGroups = append(detail.SubGroups, sg)
	}
	return detail, nil
}

// CommunityParticipants devolve os participantes de todos os grupos da comunidade.
func (c *communityService) CommunityParticipants(data *CommunityJIDStruct, instance *instance_model.Instance) ([]string, error) {
	client, err := c.ensureClientConnected(instance.Id)
	if err != nil {
		return nil, err
	}
	jid, err := c.parseCommunityJID(data.CommunityJID)
	if err != nil {
		return nil, err
	}
	parts, err := client.GetLinkedGroupsParticipants(context.Background(), jid)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, p.String())
	}
	sort.Strings(out)
	return out, nil
}

// ownJIDs devolve o número e o LID da própria instância (para saber se é admin).
func ownJIDs(client *whatsmeow.Client) (types.JID, types.JID) {
	var me types.JID
	if client.Store.ID != nil {
		me = client.Store.ID.ToNonAD()
	}
	return me, client.Store.LID.ToNonAD()
}
