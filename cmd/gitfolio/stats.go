package main

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// devTypeFile keeps the last developer type aline.team made for this account, shown by gitfolio stats.
const devTypeFile = "devtype.json"

type devTypeCache struct {
	FetchedAt time.Time `json:"fetchedAt"`
	Year      int       `json:"year"`
	Account   string    `json:"account"` // the account it belongs to: another login fetches again
	Detail    devType   `json:"detail"`
}

// devType is what gitfolio stats shows of aline.team's developer type: the type, its five scores
// (0-100) and the same for AI-assisted work. Only this is kept; the rest of aline.team's answer (the
// account email, the share code, the reasons) is not.
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
	saved := cache.Detail.Title != "" && cache.Year == year && cache.Account == c.creds.Email
	if saved && !refresh {
		showDevType(lang, cache, true)
		return nil
	}
	if !c.ensureLogin() {
		return failure("loginFirst")
	}
	var d devType
	if err := c.call("GET", fmt.Sprintf("/cli/devtype?yearPeriod=%d", year), nil, &d); err != nil {
		if saved { // offline or refused: what was saved still stands
			warn(lang, "statsFetchFailed", err)
			showDevType(lang, cache, true)
			return nil
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() { // aline.team finishes it anyway and keeps it
			return failure("statsSlow")
		}
		return err
	}
	cache = devTypeCache{FetchedAt: time.Now(), Year: year, Account: c.creds.Email, Detail: d}
	if d.Title != "" { // nothing to analyse yet is not kept
		if err := saveJSON(p, cache); err != nil {
			return err
		}
	}
	showDevType(lang, cache, false)
	return nil
}

func showDevType(lang string, cache devTypeCache, saved bool) {
	d := cache.Detail
	if d.Title == "" {
		say(lang, "statsEmpty")
		return
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
	drawDevType(d.Title)
	sayKV(lang, "statsType", d.Title, cache.Year, day(d.ComputedAt), cmp.Or(d.Description, "-"), cmp.Or(d.HashTags, "-"),
		cmp.Or(d.Position, "-"), stat(d.Agility, d.AgilityAI), stat(d.Stability, d.StabilityAI),
		stat(d.Contribution, d.ContribAI), stat(d.Adaptability, d.AdaptAI), stat(d.Consistency, d.ConsistAI),
		cmp.Or(d.TitleAI, "-"))
	if saved {
		notice(fmt.Sprintf(tr(lang, "statsSaved"), cache.FetchedAt.Local().Format("2006-01-02")))
	}
}

// devTypeArt is each developer type's picture as aline.team draws it, 11 squares wide: a letter is
// a square's color in devTypeColors, "." is empty. The icons are aline.team's and, like its name and
// logo, not part of the code's license (README).
var devTypeArt = map[string][]string{
	"BUILDER": {
		".gg........",
		"gggg.gg....",
		"gggggggg...",
		"....gggg...",
		"...........",
		"...gg..gg..",
		"..gggggggg.",
		"..gggggggg.",
	},
	"EXPLORER": {
		".....b.....",
		"....bbb....",
		"...bbbbb...",
		"...bbybb...",
		"...byyyb...",
		"...bbybb...",
		"...bbbbb...",
		"..bbbbbbb..",
		".bb.bbb.bb.",
		".b..y.y..b.",
		"....y.y....",
	},
	"FIXER": {
		"..s.....s..",
		"...s...s...",
		"...sssss...",
		"..sssssss..",
		"s.sysssys.s",
		".sssysysss.",
		"s.sssysss.s",
		".sssysysss.",
		"s.sysssys.s",
		"...sssss...",
	},
	"KEEPER": {
		"..sssssss..",
		".sssssssss.",
		".sssssssss.",
		".sssssswss.",
		".ssssswwss.",
		".swsswwsss.",
		".swwwwssss.",
		"..swwssss..",
		"..sswssss..",
		"...sssss...",
		"....sss....",
	},
	"LEADER": {
		"y....y....y",
		"yy..yyy..yy",
		"yyy.yyy.yyy",
		"yyyyyyyyyyy",
		"yyyyyyyyyyy",
		"yybyybyybyy",
		"yyyyyyyyyyy",
		"yyyyyyyyyyy",
	},
	"SPRINTER": {
		"....yyyyy..",
		"...yyyyy...",
		"...yyyy....",
		"..yyyy.....",
		"..yyyyyyyy.",
		".yyyyyyyy..",
		".....yyy...",
		"....yyy....",
		"...yyy.....",
		"..yy.......",
		".y.........",
	},
}

// devTypeColors are the xterm 256 colors nearest aline.team's (in the comments).
var devTypeColors = map[rune]int{
	'g': 77,  // #39D353
	'b': 68,  // #6699CC
	'y': 187, // #D9E0A3
	's': 108, // #7CB687
	'w': 255, // #E8F0EA
}

// drawDevType draws the type's picture above the result. A character is about twice as tall as wide,
// so each holds two squares, one above the other: "▀" in the upper square's color on the lower one's.
// Only in a terminal with color: without it the picture is gone.
func drawDevType(title string) {
	art := devTypeArt[title]
	if art == nil || !useColor {
		return
	}
	blank()
	for y := 0; y < len(art); y += 2 {
		var b strings.Builder
		for x, top := range art[y] {
			bottom := '.'
			if y+1 < len(art) {
				bottom = rune(art[y+1][x])
			}
			switch {
			case top == '.' && bottom == '.':
				b.WriteString(" ")
			case bottom == '.':
				b.WriteString(paint(fmt.Sprintf("38;5;%d", devTypeColors[top]), "▀"))
			case top == '.':
				b.WriteString(paint(fmt.Sprintf("38;5;%d", devTypeColors[bottom]), "▄"))
			default:
				b.WriteString(paint(fmt.Sprintf("38;5;%d;48;5;%d", devTypeColors[top], devTypeColors[bottom]), "▀"))
			}
		}
		fmt.Fprintln(out, strings.TrimRight(margin+"  "+b.String(), " "))
	}
}

// scoreBar draws a 0-100 score as ten blocks, the empty part dim.
func scoreBar(v int) string {
	n := min(max((v+5)/10, 0), 10)
	return strings.Repeat("█", n) + dim(strings.Repeat("░", 10-n))
}
