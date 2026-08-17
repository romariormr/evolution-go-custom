package model

import "time"

// PollVote representa um voto em uma enquete do WhatsApp
type PollVote struct {
	ID              string    `json:"id"`
	CompanyID       string    `json:"companyId"`
	InstanceID      string    `json:"instanceId"`
	PollMessageID   string    `json:"pollMessageId"`
	PollChatJid     string    `json:"pollChatJid"`
	VoteMessageID   string    `json:"voteMessageId"`
	VoterJid        string    `json:"voterJid"`
	VoterPhone      string    `json:"voterPhone,omitempty"`
	VoterName       string    `json:"voterName,omitempty"`
	SelectedOptions []string  `json:"selectedOptions"` // SHA-256 hashes
	VotedAt         time.Time `json:"votedAt"`
	ReceivedAt      time.Time `json:"receivedAt"`
}

// PollOptionResult é uma opção da enquete já com o texto (não só o hash),
// contagem e percentual. Opções conhecidas com zero voto também aparecem.
type PollOptionResult struct {
	Name       string  `json:"name"`       // texto da opção; "" se o hash não é conhecido
	Hash       string  `json:"hash"`       // SHA-256 hex (o que o WhatsApp envia no voto)
	Count      int     `json:"count"`      // quantos votantes escolheram esta opção
	Percentage float64 `json:"percentage"` // count / totalVoters * 100 (1 casa)
	Known      bool    `json:"known"`      // false = voto para uma opção não registrada
}

// PollResults representa os resultados agregados de uma enquete
type PollResults struct {
	PollMessageID string             `json:"pollMessageId"`
	PollChatJid   string             `json:"pollChatJid"`
	Question      string             `json:"question,omitempty"`
	TotalVotes    int                `json:"totalVotes"`  // votantes (uma linha por votante) — mantido por compat
	TotalVoters   int                `json:"totalVoters"` // pessoas distintas que votaram (== TotalVotes)
	Options       []PollOptionResult `json:"options"`     // ordenado por mais votada; inclui opções com 0 voto
	Votes         []PollVote         `json:"votes"`
	OptionCounts  map[string]int     `json:"optionCounts"` // hash -> count (mantido por compat)
	Voters        []VoterInfo        `json:"voters"`
}

// VoterInfo representa informações de um votante
type VoterInfo struct {
	Jid                 string    `json:"jid"`
	Phone               string    `json:"phone,omitempty"`
	Name                string    `json:"name,omitempty"`
	SelectedOptions     []string  `json:"selectedOptions"`     // hashes (compat)
	SelectedOptionNames []string  `json:"selectedOptionNames"` // texto das opções ("" se hash desconhecido)
	VotedAt             time.Time `json:"votedAt"`
}
