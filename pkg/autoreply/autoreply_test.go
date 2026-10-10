package autoreply

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/types"
)

// sábado 10/10/2026 12:00 UTC = 09:00 em São Paulo
var sat = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func mustRule(t *testing.T, r Rule) *Rule {
	t.Helper()
	if err := r.normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	return &r
}

func TestNormalizeDefaultsAndValidation(t *testing.T) {
	w := mustRule(t, Rule{Trigger: TriggerWelcome, Response: "Olá {{name}}"})
	if w.CooldownMinutes != 1440 || w.Priority != 30 || w.Timezone != DefaultTimezone {
		t.Fatalf("defaults welcome: %+v", w)
	}
	a := mustRule(t, Rule{Trigger: TriggerAway, Response: "fora"})
	if a.Days != "1,2,3,4,5" || a.StartTime != "08:00" || a.EndTime != "18:00" || a.CooldownMinutes != 240 {
		t.Fatalf("defaults away: %+v", a)
	}
	bad := []Rule{
		{Trigger: TriggerKeyword, Response: "x"},                                      // sem pattern
		{Trigger: TriggerKeyword, Pattern: "(", MatchType: MatchRegex, Response: "x"}, // regex inválida
		{Trigger: "outro", Response: "x"},
		{Trigger: TriggerWelcome}, // sem resposta
		{Trigger: TriggerAway, Days: "7", Response: "x"},
		{Trigger: TriggerAway, StartTime: "25:00", Response: "x"},
		{Trigger: TriggerWelcome, Timezone: "Lua/Base", Response: "x"},
	}
	for i, r := range bad {
		if err := r.normalize(); err == nil {
			t.Errorf("caso %d: esperava erro", i)
		}
	}
}

func TestKeywordMatch(t *testing.T) {
	cases := []struct {
		matchType, pattern, text string
		want                     bool
	}{
		{MatchContains, "preço", "Qual o PREÇO?", true},
		{MatchContains, "preço", "", false},
		{MatchEquals, "oi", " Oi ", true},
		{MatchEquals, "oi", "oi tudo bem", false},
		{MatchStartsWith, "pedido", "Pedido 123", true},
		{MatchRegex, `(?i)^pedido\s+\d+$`, "PEDIDO 42", true},
		{MatchRegex, `(?i)^pedido\s+\d+$`, "pedido abc", false},
	}
	for _, c := range cases {
		r := mustRule(t, Rule{Trigger: TriggerKeyword, MatchType: c.matchType, Pattern: c.pattern, Response: "x"})
		if got := r.matches(c.text, sat); got != c.want {
			t.Errorf("%s %q vs %q = %v, want %v", c.matchType, c.pattern, c.text, got, c.want)
		}
	}
}

func TestBusinessHours(t *testing.T) {
	r := mustRule(t, Rule{Trigger: TriggerAway, Response: "x"}) // seg-sex 08-18
	if !r.matches("", sat) {
		t.Error("sábado deve estar fora do horário")
	}
	mon := sat.AddDate(0, 0, 2) // segunda 09:00 local
	if r.matches("", mon) {
		t.Error("segunda 09:00 deve estar dentro do horário")
	}
	if !r.matches("", mon.Add(10*time.Hour)) { // 19:00 local
		t.Error("segunda 19:00 deve estar fora do horário")
	}

	night := mustRule(t, Rule{Trigger: TriggerAway, Days: "0,1,2,3,4,5,6", StartTime: "22:00", EndTime: "06:00", Response: "x"})
	if !night.withinBusinessHours(time.Date(2026, 10, 10, 2, 0, 0, 0, time.UTC)) { // 23:00 local
		t.Error("23:00 deve estar dentro da janela 22-06")
	}
	if night.withinBusinessHours(sat) { // 09:00 local
		t.Error("09:00 deve estar fora da janela 22-06")
	}
}

func TestPickPriorityAndCooldown(t *testing.T) {
	kw := mustRule(t, Rule{Id: "kw", Trigger: TriggerKeyword, Pattern: "preço", Response: "x", CooldownMinutes: 10})
	away := mustRule(t, Rule{Id: "away", Trigger: TriggerAway, Response: "x"})
	welcome := mustRule(t, Rule{Id: "welcome", Trigger: TriggerWelcome, Response: "x"})
	rules := []*Rule{welcome, away, kw}
	sortRules(rules)

	never := func(string) (time.Time, bool) { return time.Time{}, false }
	if got := pick(rules, "qual o preço", sat, never); got != kw {
		t.Fatalf("esperava keyword, veio %v", got)
	}
	if got := pick(rules, "bom dia", sat, never); got != away {
		t.Fatalf("sábado sem keyword: esperava away, veio %v", got)
	}
	recent := func(id string) (time.Time, bool) {
		if id == "kw" || id == "away" {
			return sat.Add(-5 * time.Minute), true
		}
		return time.Time{}, false
	}
	if got := pick(rules, "qual o preço", sat, recent); got != welcome {
		t.Fatalf("keyword e away em cooldown: esperava welcome, veio %v", got)
	}
	all := func(string) (time.Time, bool) { return sat.Add(-time.Minute), true }
	if got := pick(rules, "qual o preço", sat, all); got != nil {
		t.Fatalf("tudo em cooldown: esperava nil, veio %v", got.Id)
	}
}

func TestEligible(t *testing.T) {
	user := types.NewJID("5511999999999", types.DefaultUserServer)
	ok := types.MessageInfo{MessageSource: types.MessageSource{Chat: user}, Type: "text", Timestamp: sat}
	if !eligible(ok, sat) {
		t.Fatal("mensagem normal deveria ser elegível")
	}
	cases := map[string]types.MessageInfo{
		"fromMe": {MessageSource: types.MessageSource{Chat: user, IsFromMe: true}, Type: "text", Timestamp: sat},
		"grupo":  {MessageSource: types.MessageSource{Chat: types.NewJID("123", types.GroupServer), IsGroup: true}, Type: "text", Timestamp: sat},
		"status": {MessageSource: types.MessageSource{Chat: types.StatusBroadcastJID}, Type: "text", Timestamp: sat},
		"reação": {MessageSource: types.MessageSource{Chat: user}, Type: "reaction", Timestamp: sat},
		"antiga": {MessageSource: types.MessageSource{Chat: user}, Type: "text", Timestamp: sat.Add(-10 * time.Minute)},
	}
	for name, info := range cases {
		if eligible(info, sat) {
			t.Errorf("%s não deveria ser elegível", name)
		}
	}
}

func TestRender(t *testing.T) {
	if got := render("Olá {{name}}, tudo bem?", "Ana"); got != "Olá Ana, tudo bem?" {
		t.Errorf("com nome: %q", got)
	}
	if got := render("Olá {{name}}, tudo bem?", ""); got != "Olá, tudo bem?" {
		t.Errorf("sem nome: %q", got)
	}
}
