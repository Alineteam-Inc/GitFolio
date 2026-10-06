package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// devTypeFile keeps the last developer type aline.team made for this account, shown by gitfolio stats.
const devTypeFile = "devtype.json"

type devTypeCache struct {
	FetchedAt time.Time       `json:"fetchedAt"`
	Year      int             `json:"year"`
	Account   string          `json:"account"` // the account it belongs to: another login fetches again
	Detail    json.RawMessage `json:"detail"`  // as aline.team sent it, so fields this version does not show are kept
}

// devType is what gitfolio stats shows of aline.team's developer type: the type, its five scores
// (0-100) and the same for AI-assisted work.
type devType struct {
	Title        string `json:"devTypeTitle"`
	Description  string `json:"devTypeDescription"`
	HashTags     string `json:"devTypeHashTags"`
	ComputedAt   string `json:"devTypeComputedAt"`
	Position     string `json:"dominantPosition"`
	Agility      *int   `json:"agilityStat"`
	Stability    *int   `json:"stabilityStat"`
	Contribution *int   `json:"contributionStat"`
	Adaptability *int   `json:"adaptabilityStat"`
	Consistency  *int   `json:"consistencyStat"`
	TitleAI      string `json:"devTypeTitleAi"`
	AgilityAI    *int   `json:"agilityStatAi"`
	StabilityAI  *int   `json:"stabilityStatAi"`
	ContribAI    *int   `json:"contributionStatAi"`
	AdaptAI      *int   `json:"adaptabilityStatAi"`
	ConsistAI    *int   `json:"consistencyStatAi"`
}

// cmdStats shows the developer type aline.team made from the commits sent: the one saved here, or, when
// there is none for this year and account (or with --refresh), the one aline.team has or makes now,
// saved for next time.
func cmdStats(dir string, args []string) error {
	lang := detectLang(os.Getenv)
	refresh := false
	for _, a := range args {
		if a != "--refresh" {
			return failure("usage", "gitfolio stats [--refresh]")
		}
		refresh = true
	}
	c, err := newClient(dir)
	if err != nil {
		return err
	}
	p := filepath.Join(dir, devTypeFile)
	var cache devTypeCache
	if err := loadJSON(p, &cache); err != nil {
		return err
	}
	year := time.Now().Year()
	saved := len(cache.Detail) > 0 && cache.Year == year && cache.Account == c.creds.Email
	if saved && !refresh {
		return showDevType(lang, cache, true)
	}
	if !c.ensureLogin() {
		return failure("loginFirst")
	}
	var detail json.RawMessage
	if err := c.call("GET", fmt.Sprintf("/cli/devtype?yearPeriod=%d", year), nil, &detail); err != nil {
		if saved { // offline or refused: what was saved still stands
			warn(lang, "statsFetchFailed", err)
			return showDevType(lang, cache, true)
		}
		return err
	}
	cache = devTypeCache{FetchedAt: time.Now(), Year: year, Account: c.creds.Email, Detail: detail}
	var d devType
	if json.Unmarshal(detail, &d) == nil && d.Title != "" { // nothing to analyse yet is not kept
		if err := saveJSON(p, cache); err != nil {
			return err
		}
	}
	return showDevType(lang, cache, false)
}

func showDevType(lang string, cache devTypeCache, saved bool) error {
	var d devType
	if err := json.Unmarshal(cache.Detail, &d); err != nil {
		return err
	}
	if d.Title == "" {
		say(lang, "statsEmpty")
		return nil
	}
	day := func(s string) string { // "2026-10-05T09:00:00" → "2026-10-05"
		if len(s) >= 10 {
			return s[:10]
		}
		return cmp.Or(s, "-")
	}
	stat := func(v, ai *int) string {
		if v == nil {
			return "-"
		}
		s := fmt.Sprintf("%3d %s", *v, scoreBar(*v))
		if ai != nil {
			s += fmt.Sprintf("  AI %d", *ai)
		}
		return s
	}
	sayKV(lang, "statsType", d.Title, cache.Year, day(d.ComputedAt), cmp.Or(d.Description, "-"), cmp.Or(d.HashTags, "-"),
		cmp.Or(d.Position, "-"), stat(d.Agility, d.AgilityAI), stat(d.Stability, d.StabilityAI),
		stat(d.Contribution, d.ContribAI), stat(d.Adaptability, d.AdaptAI), stat(d.Consistency, d.ConsistAI),
		cmp.Or(d.TitleAI, "-"))
	if saved {
		notice(fmt.Sprintf(tr(lang, "statsSaved"), cache.FetchedAt.Local().Format("2006-01-02")))
	}
	return nil
}

// scoreBar draws a 0-100 score as ten blocks, the empty part dim.
func scoreBar(v int) string {
	n := min(max((v+5)/10, 0), 10)
	return strings.Repeat("█", n) + dim(strings.Repeat("░", 10-n))
}
