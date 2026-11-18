package user_service

import "go.mau.fi/whatsmeow/types"

// BlocklistResponse: formato novo do whatsmeow (Items com LID e número) mais o
// campo antigo JIDs, para não quebrar quem já lia JIDs (fluxos do n8n etc.).
// JIDs traz o número (PN) quando conhecido, senão o LID.
type BlocklistResponse struct {
	DHash          string
	AddressingMode types.AddressingMode
	Items          []types.BlocklistItem
	JIDs           []types.JID
}

func toBlocklistResponse(bl *types.Blocklist) *BlocklistResponse {
	if bl == nil {
		return &BlocklistResponse{Items: []types.BlocklistItem{}, JIDs: []types.JID{}}
	}
	out := &BlocklistResponse{
		DHash:          bl.DHash,
		AddressingMode: bl.AddressingMode,
		Items:          bl.Items,
		JIDs:           make([]types.JID, 0, len(bl.Items)),
	}
	if out.Items == nil {
		out.Items = []types.BlocklistItem{}
	}
	for _, it := range bl.Items {
		if !it.PN.IsEmpty() {
			out.JIDs = append(out.JIDs, it.PN)
		} else {
			out.JIDs = append(out.JIDs, it.LID)
		}
	}
	return out
}
